package main

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// POST /api/admin/elections/{electionId}/voters
// Body: {"email": "voter@example.com"}
//
// Grants an existing, verified voter account eligibility for an election and
// issues their voter receipt. The admin only ever learns the voter's email and
// has_voted status, never a choice.
func (a *App) handleGrantEligibility(w http.ResponseWriter, r *http.Request) {
	electionID := r.PathValue("electionId")
	if _, err := uuid.Parse(electionID); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid election ID")
		return
	}

	var body struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	if email == "" || len(email) > 255 || !strings.Contains(email, "@") {
		writeError(w, http.StatusBadRequest, "A valid email is required")
		return
	}

	ctx := r.Context()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		log.Printf("begin grant eligibility: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to grant eligibility")
		return
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx,
		`SELECT status FROM evoting.elections WHERE id = $1::uuid FOR UPDATE`,
		electionID,
	).Scan(&status)
	if electionNotFound(err) {
		writeError(w, http.StatusNotFound, "Election not found")
		return
	}
	if err != nil {
		log.Printf("lock election for eligibility: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to grant eligibility")
		return
	}
	if status == "completed" {
		writeError(w, http.StatusConflict, "Election is completed; eligibility can no longer change")
		return
	}

	var userID string
	err = tx.QueryRow(ctx, `
		SELECT id::text FROM evoting.users
		WHERE lower(email) = $1 AND role = 'voter' AND is_verified = TRUE`,
		email,
	).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "No verified voter account found with that email")
		return
	}
	if err != nil {
		log.Printf("find voter by email: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to grant eligibility")
		return
	}

	receiptCode, err := newReceiptCode()
	if err != nil {
		log.Printf("generate receipt code: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to grant eligibility")
		return
	}
	var receiptID string
	err = tx.QueryRow(ctx, `
		INSERT INTO evoting.receipts (election_id, receipt_code)
		VALUES ($1::uuid, $2)
		RETURNING id::text`,
		electionID, receiptCode,
	).Scan(&receiptID)
	if err != nil {
		log.Printf("create receipt: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to grant eligibility")
		return
	}

	var eligibilityID string
	err = tx.QueryRow(ctx, `
		INSERT INTO evoting.voter_eligibility (election_id, user_id, receipt_id)
		VALUES ($1::uuid, $2::uuid, $3::uuid)
		ON CONFLICT (election_id, user_id) DO NOTHING
		RETURNING id::text`,
		electionID, userID, receiptID,
	).Scan(&eligibilityID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "This voter is already eligible for this election")
		return
	}
	if err != nil {
		log.Printf("insert eligibility: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to grant eligibility")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		log.Printf("commit grant eligibility: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to grant eligibility")
		return
	}
	writeJSON(w, http.StatusCreated, voterStatus{UserID: userID, Email: email, HasVoted: false})
}

// DELETE /api/admin/elections/{electionId}/voters/{userId}
//
// Revokes eligibility, only if the voter has not voted yet. Once a ballot is
// recorded the eligibility row must stay so the election remains auditable.
func (a *App) handleRevokeEligibility(w http.ResponseWriter, r *http.Request) {
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

	ctx := r.Context()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		log.Printf("begin revoke eligibility: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to revoke eligibility")
		return
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx,
		`SELECT status FROM evoting.elections WHERE id = $1::uuid FOR UPDATE`,
		electionID,
	).Scan(&status)
	if electionNotFound(err) {
		writeError(w, http.StatusNotFound, "Election not found")
		return
	}
	if err != nil {
		log.Printf("lock election for revoke: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to revoke eligibility")
		return
	}
	if status == "completed" {
		writeError(w, http.StatusConflict, "Election is completed; eligibility can no longer change")
		return
	}

	var receiptID *string
	err = tx.QueryRow(ctx, `
		DELETE FROM evoting.voter_eligibility
		WHERE election_id = $1::uuid AND user_id = $2::uuid AND has_voted = FALSE
		RETURNING receipt_id::text`,
		electionID, userID,
	).Scan(&receiptID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "Voter is not eligible for this election or has already voted")
		return
	}
	if err != nil {
		log.Printf("delete eligibility: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to revoke eligibility")
		return
	}
	if receiptID != nil {
		if _, err := tx.Exec(ctx,
			`DELETE FROM evoting.receipts WHERE id = $1::uuid`, *receiptID,
		); err != nil {
			log.Printf("delete receipt: %v", err)
			writeError(w, http.StatusInternalServerError, "Failed to revoke eligibility")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		log.Printf("commit revoke eligibility: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to revoke eligibility")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Eligibility revoked"})
}
