package main

import (
	"errors"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// POST /api/admin/elections/{electionId}/voters
// Body: {"email": "voter@ugm.ac.id"}
//
// Adds one participant (see addParticipants). The bulk endpoint is the same
// thing for many addresses at once.
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
	emails := extractEmails(body.Email)
	if len(emails) != 1 {
		writeError(w, http.StatusBadRequest, "A single valid email is required")
		return
	}
	email := emails[0]
	if !emailInDomain(email, a.cfg.AllowedEmailDomain) {
		writeError(w, http.StatusBadRequest,
			"Only @"+a.cfg.AllowedEmailDomain+" addresses can be added")
		return
	}

	sum, err := a.addParticipants(r.Context(), electionID, []string{email})
	if err != nil {
		writeParticipantError(w, err)
		return
	}
	switch {
	case len(sum.Added) == 1:
		writeJSON(w, http.StatusCreated, voterStatus{UserID: sum.Added[0].UserID, Email: email, HasVoted: false})
	case len(sum.AlreadyEligible) == 1:
		writeError(w, http.StatusConflict, "This voter is already eligible for this election")
	default:
		writeError(w, http.StatusConflict, "Cannot add this email: "+sum.Skipped[0].Reason)
	}
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
