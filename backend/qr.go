package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

const qrTokenTTL = 15 * time.Minute

func qrTokenKey(hash string) string    { return "qr:login:" + hash }
func qrActiveKey(userID string) string { return "qr:active:" + userID }

func hashQRToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type QRService struct {
	rdb *redis.Client
}

func newQRService(rdb *redis.Client) *QRService {
	return &QRService{rdb: rdb}
}

func (s *QRService) Issue(ctx context.Context, userID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := hashQRToken(token)

	prev, err := s.rdb.Get(ctx, qrActiveKey(userID)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return "", err
	}
	if prev != "" {
		if err := s.rdb.Del(ctx, qrTokenKey(prev)).Err(); err != nil {
			return "", err
		}
	}

	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, qrTokenKey(hash), userID, qrTokenTTL)
	pipe.Set(ctx, qrActiveKey(userID), hash, qrTokenTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", err
	}
	return token, nil
}

func (s *QRService) Redeem(ctx context.Context, token string) (userID string, ok bool, err error) {
	if token == "" || len(token) > 128 {
		return "", false, nil
	}
	userID, err = s.rdb.GetDel(ctx, qrTokenKey(hashQRToken(token))).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if err := s.rdb.Del(ctx, qrActiveKey(userID)).Err(); err != nil {
		return "", false, err
	}
	return userID, true, nil
}

func (a *App) handleCreateVoterQR(w http.ResponseWriter, r *http.Request) {
	electionID := r.PathValue("electionId")
	userID := r.PathValue("userId")
	if _, err := uuid.Parse(electionID); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid election ID")
		return
	}
	if _, err := uuid.Parse(userID); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	var status string
	err := a.db.QueryRow(r.Context(), `
		SELECT e.status
		FROM evoting.voter_eligibility ve
		JOIN evoting.elections e ON e.id = ve.election_id
		JOIN evoting.users u ON u.id = ve.user_id
		WHERE ve.election_id = $1::uuid AND ve.user_id = $2::uuid
			AND u.role = 'voter' AND u.is_verified = TRUE
			AND EXISTS (SELECT 1 FROM evoting.anonymous_issuer_keys k WHERE k.election_id = e.id)`,
		electionID, userID,
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Voter is not eligible for this election")
		return
	}
	if err != nil {
		log.Printf("check voter for qr: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to create QR code")
		return
	}
	if status == "completed" {
		writeError(w, http.StatusConflict, "Election is completed")
		return
	}

	token, err := a.qr.Issue(r.Context(), userID)
	if err != nil {
		log.Printf("issue qr token: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to create QR code")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":              token,
		"expires_in_seconds": int(qrTokenTTL.Seconds()),
	})
}

func (a *App) handleQRLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &body); err != nil || body.Token == "" {
		writeError(w, http.StatusBadRequest, "A QR token is required")
		return
	}

	ctx := r.Context()
	userID, ok, err := a.qr.Redeem(ctx, body.Token)
	if err != nil {
		log.Printf("redeem qr token: %v", err)
		writeError(w, http.StatusInternalServerError, "QR sign-in failed")
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "This QR code is invalid, expired, or has already been used")
		return
	}

	var email, role string
	var isVerified bool
	err = a.db.QueryRow(ctx, `
		SELECT email, role::text, is_verified
		FROM evoting.users WHERE id = $1::uuid`,
		userID,
	).Scan(&email, &role, &isVerified)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (role != "voter" || !isVerified)) {
		writeError(w, http.StatusForbidden, "This account cannot sign in with a QR code")
		return
	}
	if err != nil {
		log.Printf("qr login user lookup: %v", err)
		writeError(w, http.StatusInternalServerError, "QR sign-in failed")
		return
	}

	challengeID, otp, err := a.otp.CreateChallenge(ctx, userID)
	if err != nil {
		log.Printf("create otp challenge: %v", err)
		writeError(w, http.StatusInternalServerError, "QR sign-in failed")
		return
	}
	if err := a.deliverOTP(ctx, email, otp); err != nil {
		log.Printf("send QR sign-in OTP email: %v", err)
		if cleanupErr := a.otp.DeleteChallenge(ctx, challengeID); cleanupErr != nil {
			log.Printf("delete unsent OTP challenge: %v", cleanupErr)
		}
		writeError(w, http.StatusServiceUnavailable, "Unable to send sign-in code. Please try again later.")
		return
	}

	sess := sessionFrom(r)
	sess.PendingChallengeID = challengeID
	sess.PendingUserID = userID
	if err := a.sessions.Save(ctx, w, sess); err != nil {
		log.Printf("save session: %v", err)
		if cleanupErr := a.otp.DeleteChallenge(ctx, challengeID); cleanupErr != nil {
			log.Printf("delete unsaved OTP challenge: %v", cleanupErr)
		}
		writeError(w, http.StatusInternalServerError, "Failed to create login session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message":      "OTP required",
		"otp_required": true,
	})
}
