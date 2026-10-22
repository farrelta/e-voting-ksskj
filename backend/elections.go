package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var electionStatuses = map[string]bool{
	"draft":         true,
	"registration":  true,
	"active_voting": true,
	"completed":     true,
}

type electionInput struct {
	Title             string `json:"title"`
	Description       string `json:"description"`
	RegistrationStart string `json:"registration_start"`
	RegistrationEnd   string `json:"registration_end"`
	VotingStart       string `json:"voting_start"`
	VotingEnd         string `json:"voting_end"`
	Status            string `json:"status"`
}

type election struct {
	ID                string    `json:"id"`
	Title             string    `json:"title"`
	Description       string    `json:"description"`
	RegistrationStart time.Time `json:"registration_start"`
	RegistrationEnd   time.Time `json:"registration_end"`
	VotingStart       time.Time `json:"voting_start"`
	VotingEnd         time.Time `json:"voting_end"`
	Status            string    `json:"status"`
	IsRegistered      bool      `json:"is_registered,omitempty"`
	HasVoted          bool      `json:"has_voted,omitempty"`
	ReceiptID         string    `json:"receipt_id,omitempty"`
	ReceiptCode       string    `json:"receipt_code,omitempty"`
}

func parseElectionInput(body electionInput) (electionInput, time.Time, time.Time, time.Time, time.Time, bool) {
	body.Title = strings.TrimSpace(body.Title)
	body.Status = strings.TrimSpace(body.Status)
	registrationStart, err1 := time.Parse(time.RFC3339, body.RegistrationStart)
	registrationEnd, err2 := time.Parse(time.RFC3339, body.RegistrationEnd)
	votingStart, err3 := time.Parse(time.RFC3339, body.VotingStart)
	votingEnd, err4 := time.Parse(time.RFC3339, body.VotingEnd)
	valid := body.Title != "" && electionStatuses[body.Status] &&
		err1 == nil && err2 == nil && err3 == nil && err4 == nil &&
		registrationStart.Before(registrationEnd) &&
		!registrationEnd.After(votingStart) && votingStart.Before(votingEnd)
	return body, registrationStart, registrationEnd, votingStart, votingEnd, valid
}

func (a *App) handleListElections(w http.ResponseWriter, r *http.Request) {
	user := sessionFrom(r).User
	query := `
		SELECT id::text, title, COALESCE(description, ''), registration_start,
			registration_end, voting_start, voting_end, status,
			FALSE, FALSE, '', ''
		FROM evoting.elections`
	args := []any{}
	if user.Role != "admin" {
		query = `
			SELECT e.id::text, e.title, COALESCE(e.description, ''),
				e.registration_start, e.registration_end, e.voting_start, e.voting_end, e.status,
				(ve.id IS NOT NULL), COALESCE(ve.has_voted, FALSE),
				COALESCE(ve.receipt_id::text, ''), COALESCE(rec.receipt_code, '')
			FROM evoting.elections e
			LEFT JOIN evoting.voter_eligibility ve
				ON ve.election_id = e.id AND ve.user_id = $1::uuid
			LEFT JOIN evoting.receipts rec ON rec.id = ve.receipt_id
			WHERE e.status IN ('registration', 'active_voting')`
		args = append(args, user.ID)
	}
	query += " ORDER BY registration_start DESC"

	rows, err := a.db.Query(r.Context(), query, args...)
	if err != nil {
		log.Printf("list elections: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve elections")
		return
	}
	defer rows.Close()

	elections := make([]election, 0)
	for rows.Next() {
		var item election
		if err := rows.Scan(
			&item.ID, &item.Title, &item.Description, &item.RegistrationStart,
			&item.RegistrationEnd, &item.VotingStart, &item.VotingEnd, &item.Status,
			&item.IsRegistered, &item.HasVoted, &item.ReceiptID, &item.ReceiptCode,
		); err != nil {
			log.Printf("scan election: %v", err)
			writeError(w, http.StatusInternalServerError, "Failed to retrieve elections")
			return
		}
		elections = append(elections, item)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate elections: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve elections")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"elections": elections})
}

func (a *App) handleCreateElection(w http.ResponseWriter, r *http.Request) {
	var input electionInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid election details")
		return
	}
	input, registrationStart, registrationEnd, votingStart, votingEnd, ok := parseElectionInput(input)
	if !ok {
		writeError(w, http.StatusBadRequest, "Provide a title, valid dates, and a supported status")
		return
	}

	var id string
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO evoting.elections (
			title, description, registration_start, registration_end,
			voting_start, voting_end, status, created_by
		) VALUES ($1, NULLIF($2, ''), $3, $4, $5, $6, $7, $8::uuid)
		RETURNING id::text`,
		input.Title, strings.TrimSpace(input.Description), registrationStart,
		registrationEnd, votingStart, votingEnd, input.Status, sessionFrom(r).User.ID,
	).Scan(&id)
	if err != nil {
		log.Printf("create election: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to create election")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (a *App) handleUpdateElection(w http.ResponseWriter, r *http.Request) {
	electionID := r.PathValue("electionId")
	if _, err := uuid.Parse(electionID); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid election ID")
		return
	}
	var input electionInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid election details")
		return
	}
	input, registrationStart, registrationEnd, votingStart, votingEnd, ok := parseElectionInput(input)
	if !ok {
		writeError(w, http.StatusBadRequest, "Provide a title, valid dates, and a supported status")
		return
	}
	tag, err := a.db.Exec(r.Context(), `
		UPDATE evoting.elections SET title = $1, description = NULLIF($2, ''),
			registration_start = $3, registration_end = $4, voting_start = $5,
			voting_end = $6, status = $7
		WHERE id = $8::uuid`,
		input.Title, strings.TrimSpace(input.Description), registrationStart,
		registrationEnd, votingStart, votingEnd, input.Status, electionID,
	)
	if err != nil {
		log.Printf("update election: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to update election")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "Election not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Election updated"})
}

// DELETE /api/admin/elections/{electionId}
func (a *App) handleDeleteElection(w http.ResponseWriter, r *http.Request) {
	electionID := r.PathValue("electionId")
	if _, err := uuid.Parse(electionID); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid election ID")
		return
	}

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		log.Printf("begin election deletion: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to delete election")
		return
	}
	defer tx.Rollback(r.Context())

	var lockedID string
	err = tx.QueryRow(
		r.Context(),
		`SELECT id::text FROM evoting.elections WHERE id = $1::uuid FOR UPDATE`,
		electionID,
	).Scan(&lockedID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Election not found")
		return
	}
	if err != nil {
		log.Printf("lock election for deletion: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to delete election")
		return
	}

	var hasVoters, hasBallots bool
	if err := tx.QueryRow(r.Context(), `
		SELECT EXISTS (SELECT 1 FROM evoting.voter_eligibility WHERE election_id = $1::uuid),
		       EXISTS (SELECT 1 FROM evoting.encrypted_ballots WHERE election_id = $1::uuid)`,
		electionID,
	).Scan(&hasVoters, &hasBallots); err != nil {
		log.Printf("check election records before deletion: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to delete election")
		return
	}
	if hasVoters || hasBallots {
		writeError(w, http.StatusConflict, "Elections with voter registrations or ballots cannot be deleted")
		return
	}

	if _, err := tx.Exec(r.Context(), `DELETE FROM evoting.receipts WHERE election_id = $1::uuid`, electionID); err != nil {
		log.Printf("delete election receipts: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to delete election")
		return
	}
	if _, err := tx.Exec(r.Context(), `DELETE FROM evoting.candidates WHERE election_id = $1::uuid`, electionID); err != nil {
		log.Printf("delete election candidates: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to delete election")
		return
	}
	tag, err := tx.Exec(r.Context(), `DELETE FROM evoting.elections WHERE id = $1::uuid`, electionID)
	if err != nil {
		log.Printf("delete election: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to delete election")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "Election not found")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		log.Printf("commit election deletion: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to delete election")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Election deleted"})
}

func (a *App) handleUpdateElectionStatus(w http.ResponseWriter, r *http.Request) {
	electionID := r.PathValue("electionId")
	if _, err := uuid.Parse(electionID); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid election ID")
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(w, r, &body); err != nil || !electionStatuses[body.Status] {
		writeError(w, http.StatusBadRequest, "Unsupported election status")
		return
	}
	tag, err := a.db.Exec(r.Context(),
		`UPDATE evoting.elections SET status = $1 WHERE id = $2::uuid`,
		body.Status, electionID,
	)
	if err != nil {
		log.Printf("update election status: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to update election status")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "Election not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Election status updated"})
}

func (a *App) handleCreateCandidate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ElectionID  string `json:"election_id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid candidate details")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if _, err := uuid.Parse(body.ElectionID); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "A valid election and candidate name are required")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		log.Printf("begin candidate creation: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to create candidate")
		return
	}
	defer tx.Rollback(r.Context())
	var status string
	err = tx.QueryRow(r.Context(), `
		SELECT status FROM evoting.elections WHERE id = $1::uuid FOR UPDATE`, body.ElectionID,
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Election not found")
		return
	}
	if err != nil {
		log.Printf("lock election for candidate creation: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to create candidate")
		return
	}
	if !editableCandidateStatus(status) {
		writeError(w, http.StatusConflict, "Candidates cannot be changed after voting starts")
		return
	}
	var id string
	err = tx.QueryRow(r.Context(), `
		INSERT INTO evoting.candidates (election_id, name, description)
		VALUES ($1::uuid, $2, NULLIF($3, ''))
		RETURNING id::text`,
		body.ElectionID, body.Name, strings.TrimSpace(body.Description),
	).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23505":
				writeError(w, http.StatusConflict, "A candidate with this name already exists for the election")
				return
			case "23503":
				writeError(w, http.StatusNotFound, "Election not found")
				return
			}
		}
		log.Printf("create candidate: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to create candidate")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		log.Printf("commit candidate creation: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to create candidate")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (a *App) handleListCandidates(w http.ResponseWriter, r *http.Request) {
	electionID := r.PathValue("electionId")
	if _, err := uuid.Parse(electionID); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid election ID")
		return
	}
	user := sessionFrom(r).User
	rows, err := a.db.Query(r.Context(), `
		SELECT c.id::text, c.name, COALESCE(c.description, '')
		FROM evoting.candidates c
		JOIN evoting.elections e ON e.id = c.election_id
		JOIN evoting.voter_eligibility ve ON ve.election_id = e.id
		WHERE c.election_id = $1::uuid AND ve.user_id = $2::uuid
			AND e.status = 'active_voting' AND ve.has_voted = FALSE
		ORDER BY c.name`,
		electionID, user.ID,
	)
	if err != nil {
		log.Printf("list candidates: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve candidates")
		return
	}
	defer rows.Close()

	type candidate struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	candidates := make([]candidate, 0)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.ID, &item.Name, &item.Description); err != nil {
			log.Printf("scan candidate: %v", err)
			writeError(w, http.StatusInternalServerError, "Failed to retrieve candidates")
			return
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate candidates: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve candidates")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": candidates})
}

// GET /api/admin/elections/{electionId}/candidates
func (a *App) handleListElectionCandidates(w http.ResponseWriter, r *http.Request) {
	electionID := r.PathValue("electionId")
	if _, err := uuid.Parse(electionID); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid election ID")
		return
	}
	rows, err := a.db.Query(r.Context(), `
		SELECT id::text, name, COALESCE(description, '')
		FROM evoting.candidates WHERE election_id = $1::uuid ORDER BY name`, electionID)
	if err != nil {
		log.Printf("list election candidates: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve candidates")
		return
	}
	defer rows.Close()
	type candidate struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	candidates := make([]candidate, 0)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.ID, &item.Name, &item.Description); err != nil {
			log.Printf("scan election candidate: %v", err)
			writeError(w, http.StatusInternalServerError, "Failed to retrieve candidates")
			return
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate election candidates: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve candidates")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": candidates})
}

func candidateElectionStatus(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, candidateID string) (string, error) {
	var status string
	err := db.QueryRow(ctx, `
		SELECT e.status FROM evoting.candidates c
		JOIN evoting.elections e ON e.id = c.election_id
		WHERE c.id = $1::uuid FOR UPDATE OF e`, candidateID).Scan(&status)
	return status, err
}

func editableCandidateStatus(status string) bool {
	return status == "draft" || status == "registration"
}

// PUT /api/admin/candidates/{candidateId}
func (a *App) handleUpdateCandidate(w http.ResponseWriter, r *http.Request) {
	candidateID := r.PathValue("candidateId")
	if _, err := uuid.Parse(candidateID); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid candidate ID")
		return
	}
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid candidate details")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Description = strings.TrimSpace(body.Description)
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "Candidate name is required")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to update candidate")
		return
	}
	defer tx.Rollback(r.Context())
	status, err := candidateElectionStatus(r.Context(), tx, candidateID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Candidate not found")
		return
	}
	if err != nil {
		log.Printf("lock candidate election for update: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to update candidate")
		return
	}
	if !editableCandidateStatus(status) {
		writeError(w, http.StatusConflict, "Candidates cannot be changed after voting starts")
		return
	}
	_, err = tx.Exec(r.Context(), `
		UPDATE evoting.candidates SET name = $1, description = NULLIF($2, '') WHERE id = $3::uuid`,
		body.Name, body.Description, candidateID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "A candidate with this name already exists for the election")
			return
		}
		log.Printf("update candidate: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to update candidate")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		log.Printf("commit candidate update: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to update candidate")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Candidate updated"})
}

// DELETE /api/admin/candidates/{candidateId}
func (a *App) handleDeleteCandidate(w http.ResponseWriter, r *http.Request) {
	candidateID := r.PathValue("candidateId")
	if _, err := uuid.Parse(candidateID); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid candidate ID")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to delete candidate")
		return
	}
	defer tx.Rollback(r.Context())
	status, err := candidateElectionStatus(r.Context(), tx, candidateID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Candidate not found")
		return
	}
	if err != nil {
		log.Printf("lock candidate election for deletion: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to delete candidate")
		return
	}
	if !editableCandidateStatus(status) {
		writeError(w, http.StatusConflict, "Candidates cannot be changed after voting starts")
		return
	}
	if _, err := tx.Exec(r.Context(), `DELETE FROM evoting.candidates WHERE id = $1::uuid`, candidateID); err != nil {
		log.Printf("delete candidate: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to delete candidate")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		log.Printf("commit candidate deletion: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to delete candidate")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Candidate deleted"})
}

func electionNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
