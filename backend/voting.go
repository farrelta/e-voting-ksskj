package main

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type BallotVerifier interface {
	Verify(ctx context.Context, electionID string, encryptedVote, proof []byte) error
}

func newReceiptCode() (string, error) {
	random := make([]byte, 20)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "EV-" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random), nil
}

func (a *App) handleSubmitBallot(w http.ResponseWriter, r *http.Request) {
	electionID := r.PathValue("electionId")
	if _, err := uuid.Parse(electionID); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid election ID")
		return
	}
	var body struct {
		ReceiptID     string `json:"receipt_id"`
		EncryptedVote string `json:"encrypted_vote"`
		Proof         string `json:"proof"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid encrypted ballot")
		return
	}
	if _, err := uuid.Parse(body.ReceiptID); err != nil {
		writeError(w, http.StatusBadRequest, "A valid voter receipt is required")
		return
	}
	encryptedVote, err := base64.StdEncoding.DecodeString(body.EncryptedVote)
	if err != nil || len(encryptedVote) == 0 || len(encryptedVote) > 256*1024 {
		writeError(w, http.StatusBadRequest, "Encrypted vote must be valid base64 data no larger than 256 KB")
		return
	}
	proof, err := base64.StdEncoding.DecodeString(body.Proof)
	if err != nil || len(proof) == 0 || len(proof) > 256*1024 {
		writeError(w, http.StatusBadRequest, "Proof must be valid base64 data no larger than 256 KB")
		return
	}
	if a.ballotVerifier == nil {
		writeError(w, http.StatusServiceUnavailable, "Ballot submission is disabled until the election proof verifier is configured")
		return
	}
	if err := a.ballotVerifier.Verify(r.Context(), electionID, encryptedVote, proof); err != nil {
		log.Printf("verify submitted ballot proof: %v", err)
		writeError(w, http.StatusUnprocessableEntity, "Ballot proof verification failed")
		return
	}

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		log.Printf("begin ballot submission: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to submit ballot")
		return
	}
	defer tx.Rollback(r.Context())

	var lockedElectionID string
	err = tx.QueryRow(r.Context(), `
		SELECT id::text FROM evoting.elections
		WHERE id = $1::uuid AND status = 'active_voting'
			AND voting_start <= NOW() AND voting_end > NOW()
		FOR UPDATE`,
		electionID,
	).Scan(&lockedElectionID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "Election is not accepting ballots")
		return
	}
	if err != nil {
		log.Printf("lock election for ballot: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to submit ballot")
		return
	}

	var eligibilityID string
	err = tx.QueryRow(r.Context(), `
		UPDATE evoting.voter_eligibility ve
		SET has_voted = TRUE
		FROM evoting.elections e
		WHERE ve.election_id = e.id
			AND ve.election_id = $1::uuid
			AND ve.user_id = $2::uuid
			AND ve.receipt_id = $3::uuid
			AND ve.has_voted = FALSE
			AND e.status = 'active_voting'
			AND e.voting_start <= NOW() AND e.voting_end > NOW()
		RETURNING ve.id::text`,
		electionID, sessionFrom(r).User.ID, body.ReceiptID,
	).Scan(&eligibilityID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "No active, unused voter registration was found for this ballot")
		return
	}
	if err != nil {
		log.Printf("reserve voter eligibility: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to submit ballot")
		return
	}

	var ballotHash string
	err = tx.QueryRow(r.Context(), `
		INSERT INTO evoting.encrypted_ballots (
			election_id, receipt_id, encrypted_vote, proof, ballot_hash
		)
		VALUES (
			$1::uuid, $2::uuid, $3::bytea, $4::bytea,
			encode(public.digest(
				convert_to($1::uuid::text, 'UTF8') ||
				int4send(octet_length($3::bytea)) || $3::bytea ||
				int4send(octet_length($4::bytea)) || $4::bytea,
				'sha256'
			), 'hex')
		)
		RETURNING ballot_hash`,
		electionID, body.ReceiptID, encryptedVote, proof,
	).Scan(&ballotHash)
	if err != nil {
		log.Printf("store encrypted ballot: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to store encrypted ballot")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		log.Printf("commit ballot submission: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to submit ballot")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"ballot_hash": ballotHash})
}

func (a *App) handleVerifyReceipt(w http.ResponseWriter, r *http.Request) {
	receiptCode := strings.TrimSpace(r.PathValue("receiptCode"))
	if len(receiptCode) < 10 || len(receiptCode) > 80 {
		writeError(w, http.StatusBadRequest, "Invalid receipt code")
		return
	}
	var result struct {
		ReceiptCode  string  `json:"receipt_code"`
		ElectionTitle string  `json:"election_title"`
		Recorded     bool    `json:"recorded"`
		BallotHash   *string `json:"ballot_hash,omitempty"`
	}
	err := a.db.QueryRow(r.Context(), `
		SELECT rec.receipt_code, e.title, (b.id IS NOT NULL), b.ballot_hash
		FROM evoting.receipts rec
		JOIN evoting.elections e ON e.id = rec.election_id
		LEFT JOIN evoting.encrypted_ballots b ON b.receipt_id = rec.id
		WHERE rec.receipt_code = $1`,
		receiptCode,
	).Scan(&result.ReceiptCode, &result.ElectionTitle, &result.Recorded, &result.BallotHash)
	if electionNotFound(err) {
		writeError(w, http.StatusNotFound, "Receipt not found")
		return
	}
	if err != nil {
		log.Printf("verify receipt: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to verify receipt")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
