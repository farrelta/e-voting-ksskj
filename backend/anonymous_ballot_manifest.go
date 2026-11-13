package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/farrelta/e-voting-ksskj/backend-go/anonymousballot"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ballotCandidate struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ballotManifest struct {
	PublicKey  []byte            `json:"public_key"`
	Candidates []ballotCandidate `json:"candidates"`
	Hash       []byte            `json:"manifest_hash"`
	issuerKey  []byte
}

type manifestDB interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadBallotManifest(ctx context.Context, db manifestDB, id uuid.UUID) (ballotManifest, error) {
	var m ballotManifest
	err := db.QueryRow(ctx, `
		SELECT k.he_public_key, i.public_key FROM evoting.election_keys k
		JOIN evoting.elections e ON e.id = k.election_id
		JOIN evoting.anonymous_issuer_keys i ON i.election_id = e.id
		WHERE e.id = $1
		AND e.status IN ('registration', 'active_voting', 'completed')`, id).Scan(&m.PublicKey, &m.issuerKey)
	if err != nil {
		return m, err
	}
	rows, err := db.Query(ctx, `
		SELECT id::text, name FROM evoting.candidates
		WHERE election_id = $1 ORDER BY position LIMIT $2`, id, anonymousballot.MaxCandidates+1)
	if err != nil {
		return m, err
	}
	m.Candidates, err = pgx.CollectRows(rows, pgx.RowToStructByPos[ballotCandidate])
	if err != nil {
		return m, err
	}
	if len(m.Candidates) == 0 || len(m.Candidates) > anonymousballot.MaxCandidates {
		return m, errors.New("invalid number of candidates")
	}
	data, err := json.Marshal(m.Candidates)
	if err != nil {
		return m, err
	}
	hash := sha256.Sum256(data)
	m.Hash = hash[:]
	return m, nil
}

func (m ballotManifest) scope(id uuid.UUID, tokenDigest [32]byte) (scope anonymousballot.Scope) {
	copy(scope[:16], id[:])
	copy(scope[16:48], m.Hash)
	copy(scope[48:], tokenDigest[:])
	return scope
}

func (a *App) handleAnonymousManifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !a.anonymousBallotPilot() {
		writeError(w, http.StatusServiceUnavailable, "Anonymous ballot pilot is disabled")
		return
	}
	id, err := uuid.Parse(r.PathValue("electionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid election ID")
		return
	}
	m, err := loadBallotManifest(r.Context(), a.db, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Anonymous ballot is not available")
		return
	}
	if err != nil {
		anonymousStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}
