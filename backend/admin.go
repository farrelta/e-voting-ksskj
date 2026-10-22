package main

import (
	"log"
	"net/http"

	"github.com/google/uuid"
)

type voterStatus struct {
	UserID   string `json:"user_id"`
	Email    string `json:"email"`
	HasVoted bool   `json:"has_voted"`
}

// GET /api/admin/elections/{electionId}/voters
func (a *App) handleListVoters(w http.ResponseWriter, r *http.Request) {
	electionID := r.PathValue("electionId")

	if _, err := uuid.Parse(electionID); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid election ID")
		return
	}

	ctx := r.Context()

	var exists bool
	err := a.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM evoting.elections WHERE id = $1::uuid)`,
		electionID,
	).Scan(&exists)
	if err != nil {
		log.Printf("check election: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve voter status")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "Election not found")
		return
	}

	rows, err := a.db.Query(ctx, `
		SELECT
			u.id::text AS user_id,
			u.email,
			ve.has_voted
		FROM evoting.voter_eligibility ve
		JOIN evoting.users u
			ON ve.user_id = u.id
		WHERE ve.election_id = $1::uuid
		ORDER BY u.email`,
		electionID,
	)
	if err != nil {
		log.Printf("query voters: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve voter status")
		return
	}
	defer rows.Close()

	voters := make([]voterStatus, 0) // [] instead of null when empty
	for rows.Next() {
		var v voterStatus
		if err := rows.Scan(&v.UserID, &v.Email, &v.HasVoted); err != nil {
			log.Printf("scan voter: %v", err)
			writeError(w, http.StatusInternalServerError, "Failed to retrieve voter status")
			return
		}
		voters = append(voters, v)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate voters: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve voter status")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"election_id": electionID,
		"voters":      voters,
	})
}
