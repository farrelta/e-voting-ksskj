package main

import (
	"errors"
	"log"
	"net/http"
	"regexp"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

var otpFormat = regexp.MustCompile(`^\d{6}$`)

// POST /api/login
func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &body); err != nil || body.Email == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "Email and password are required")
		return
	}

	ctx := r.Context()

	var user struct {
		ID           string
		Email        string
		PasswordHash string
		Role         string
		IsVerified   bool
	}
	err := a.db.QueryRow(ctx, `
		SELECT id::text, email, password_hash, role::text, is_verified
		FROM evoting.users
		WHERE email = $1`,
		body.Email,
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.IsVerified)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "Invalid email or password")
		return
	}
	if err != nil {
		log.Printf("login query: %v", err)
		writeError(w, http.StatusInternalServerError, "Login failed")
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.Password)) != nil {
		writeError(w, http.StatusUnauthorized, "Invalid email or password")
		return
	}

	if !user.IsVerified {
		writeError(w, http.StatusForbidden, "Account is not verified")
		return
	}

	challengeID, otp, err := a.otp.CreateChallenge(ctx, user.ID)
	if err != nil {
		log.Printf("create otp challenge: %v", err)
		writeError(w, http.StatusInternalServerError, "Login failed")
		return
	}

	sess := sessionFrom(r)
	sess.PendingChallengeID = challengeID
	if err := a.sessions.Save(ctx, w, sess); err != nil {
		log.Printf("save session: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to create login session")
		return
	}

	if a.cfg.IsDev() {
		log.Printf("[DEV ONLY] OTP for %s: %s", user.Email, otp)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message":      "OTP required",
		"otp_required": true,
	})
}

func (a *App) handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OTP string `json:"otp"`
	}
	if err := decodeJSON(w, r, &body); err != nil || !otpFormat.MatchString(body.OTP) {
		writeError(w, http.StatusBadRequest, "OTP must be 6 digits")
		return
	}

	ctx := r.Context()
	sess := sessionFrom(r)

	if sess.PendingChallengeID == "" {
		writeError(w, http.StatusUnauthorized, "No OTP challenge found")
		return
	}

	result, err := a.otp.VerifyChallenge(ctx, sess.PendingChallengeID, body.OTP)
	if err != nil {
		log.Printf("verify otp: %v", err)
		writeError(w, http.StatusInternalServerError, "OTP verification failed")
		return
	}

	if !result.Success {
		switch result.Reason {
		case reasonTooManyAttempts:
			a.clearPendingLogin(w, r, sess)
			writeError(w, http.StatusTooManyRequests, "Too many OTP attempts")
		case reasonExpiredOrInvalid:
			a.clearPendingLogin(w, r, sess)
			writeError(w, http.StatusUnauthorized, "OTP expired or invalid")
		default:
			writeError(w, http.StatusUnauthorized, "Invalid OTP")
		}
		return
	}

	var user SessionUser
	var isVerified bool
	err = a.db.QueryRow(ctx, `
		SELECT id::text, email, role::text, is_verified
		FROM evoting.users
		WHERE id = $1::uuid`,
		result.UserID,
	).Scan(&user.ID, &user.Email, &user.Role, &isVerified)
	if errors.Is(err, pgx.ErrNoRows) {
		a.clearPendingLogin(w, r, sess)
		writeError(w, http.StatusUnauthorized, "User account not found")
		return
	}
	if err != nil {
		log.Printf("verify otp user lookup: %v", err)
		writeError(w, http.StatusInternalServerError, "OTP verification failed")
		return
	}
	if !isVerified {
		a.clearPendingLogin(w, r, sess)
		writeError(w, http.StatusForbidden, "Account is not verified")
		return
	}

	if err := a.sessions.Regenerate(ctx, sess); err != nil {
		log.Printf("regenerate session: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to create authenticated session")
		return
	}
	sess.User = &user
	if err := a.sessions.Save(ctx, w, sess); err != nil {
		log.Printf("save session: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to create authenticated session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "Authentication successful",
		"user":    map[string]string{"email": user.Email, "role": user.Role},
	})
}

// POST /api/login/resend-otp
func (a *App) handleResendOTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := sessionFrom(r)

	if sess.PendingChallengeID == "" {
		writeError(w, http.StatusUnauthorized, "No OTP challenge found")
		return
	}

	allowed, err := a.otp.CanResend(ctx, sess.PendingChallengeID)
	if err != nil {
		log.Printf("can resend otp: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to resend OTP")
		return
	}
	if !allowed {
		writeError(w, http.StatusTooManyRequests, "Please wait before requesting another OTP")
		return
	}

	otp, err := a.otp.Resend(ctx, sess.PendingChallengeID)
	if err != nil {
		log.Printf("resend otp: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to resend OTP")
		return
	}
	if otp == "" {
		a.clearPendingLogin(w, r, sess)
		writeError(w, http.StatusUnauthorized, "OTP challenge expired")
		return
	}

	if a.cfg.IsDev() {
		log.Printf("[DEV ONLY] Resent OTP: %s", otp)
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "OTP resent"})
}

// GET /api/me
func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	user := sessionFrom(r).User
	if user == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

// POST /api/logout
func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := a.sessions.Destroy(r.Context(), w, sessionFrom(r)); err != nil {
		log.Printf("logout: %v", err)
		writeError(w, http.StatusInternalServerError, "Logout failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Logged out"})
}

// delete req.session.pendingLogin`
func (a *App) clearPendingLogin(w http.ResponseWriter, r *http.Request, sess *Session) {
	sess.PendingChallengeID = ""
	if err := a.sessions.Save(r.Context(), w, sess); err != nil {
		log.Printf("clear pending login: %v", err)
	}
}
