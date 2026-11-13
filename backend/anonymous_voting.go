package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log"
	"mime"
	"net/http"

	"github.com/farrelta/e-voting-ksskj/backend-go/anonymousballot"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (a *App) handleIssueCredential(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !a.anonymousBallotPilot() {
		writeError(w, http.StatusServiceUnavailable, "Anonymous voting is not configured")
		return
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Use application/json for credential requests")
		return
	}
	id, err := uuid.Parse(r.PathValue("electionId"))
	var body struct {
		BlindRequest []byte `json:"blind_request"`
		AccountID    string `json:"account_id"`
	}
	if err != nil || decodeJSON(w, r, &body) != nil || len(body.BlindRequest) != credentialRSABytes {
		writeError(w, http.StatusBadRequest, "Invalid credential request")
		return
	}
	userID := sessionFrom(r).User.ID
	if body.AccountID != userID {
		writeError(w, http.StatusConflict, "The active account changed. Sign in with the original account and retry.")
		return
	}
	ctx := r.Context()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		anonymousStorageError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	var canIssue bool
	err = tx.QueryRow(ctx, `
		SELECT e.status IN ('registration', 'active_voting') AND clock_timestamp() < e.voting_end
		FROM evoting.elections e
		JOIN evoting.voter_eligibility ve ON ve.election_id = e.id
		JOIN evoting.anonymous_issuer_keys k ON k.election_id = e.id
		WHERE e.id = $1 AND ve.user_id = $2::uuid
		FOR UPDATE OF e, ve`, id, userID).Scan(&canIssue)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusForbidden, "No eligible anonymous election")
		return
	}
	if err != nil {
		anonymousStorageError(w, err)
		return
	}
	digest := sha256.Sum256(body.BlindRequest)
	var savedDigest, response []byte
	err = tx.QueryRow(ctx, `
		SELECT request_digest, blind_response FROM evoting.credential_issuances
		WHERE election_id = $1 AND user_id = $2::uuid`, id, userID).Scan(&savedDigest, &response)
	switch {
	case err == nil:
		if subtle.ConstantTimeCompare(savedDigest, digest[:]) != 1 {
			writeError(w, http.StatusConflict, "A credential was already issued. Retry with the original request.")
			return
		}
	case errors.Is(err, pgx.ErrNoRows):
		if !canIssue {
			writeError(w, http.StatusConflict, "Credential issuance is closed")
			return
		}
		var public, encrypted []byte
		err = tx.QueryRow(ctx, `
			SELECT public_key, private_key_enc FROM evoting.anonymous_issuer_keys WHERE election_id = $1`,
			id).Scan(&public, &encrypted)
		if err != nil {
			anonymousStorageError(w, err)
			return
		}
		response, err = signCredential(a.cfg.MasterKey, public, encrypted, body.BlindRequest)
		if err != nil || len(response) == 0 {
			writeError(w, http.StatusUnprocessableEntity, "Credential issuance failed")
			return
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO evoting.credential_issuances (election_id, user_id, request_digest, blind_response)
			VALUES ($1, $2::uuid, $3, $4)`, id, userID, digest[:], response)
		if err != nil {
			anonymousStorageError(w, err)
			return
		}
	default:
		anonymousStorageError(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		anonymousStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]byte{"blind_response": response})
}

func (a *App) handleAnonymousBallot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
		writeError(w, http.StatusBadRequest, "Send the ballot without account credentials")
		return
	}
	if !a.anonymousBallotPilot() {
		writeError(w, http.StatusServiceUnavailable, "Anonymous voting is not configured")
		return
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Use application/json for ballot requests")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	id, err := uuid.Parse(r.PathValue("electionId"))
	var body struct {
		Token []byte `json:"token"`
		Vote  []byte `json:"encrypted_vote"`
		Proof []byte `json:"proof"`
	}
	if err != nil || decodeJSON(w, r, &body) != nil || len(body.Token) == 0 || len(body.Token) > 4096 ||
		len(body.Vote) == 0 || len(body.Proof) == 0 {
		writeError(w, http.StatusBadRequest, "Invalid anonymous ballot")
		return
	}
	ctx := r.Context()
	tokenDigest, err := a.verifyCredential(ctx, id, body.Token)
	if err != nil {
		writeError(w, http.StatusForbidden, "Invalid voting credential")
		return
	}
	// Length prefix separates the ciphertext from its proof.
	payload := make([]byte, 0, 4+len(body.Vote)+len(body.Proof))
	n := len(body.Vote)
	payload = append(payload, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	payload = append(payload, body.Vote...)
	payload = append(payload, body.Proof...)
	digest := sha256.Sum256(payload)
	tx, err := a.db.Begin(ctx)
	if err != nil {
		anonymousStorageError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	var open bool
	err = tx.QueryRow(ctx, `
		SELECT status = 'active_voting' AND voting_start <= clock_timestamp() AND voting_end > clock_timestamp()
		FROM evoting.elections e WHERE id = $1 AND EXISTS (
			SELECT 1 FROM evoting.anonymous_issuer_keys k WHERE k.election_id = e.id
		) FOR UPDATE`, id).Scan(&open)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Anonymous election not found")
		return
	}
	if err != nil {
		anonymousStorageError(w, err)
		return
	}
	var savedDigest []byte
	var tracker string
	err = tx.QueryRow(ctx, `
		SELECT request_digest, ballot_hash FROM evoting.anonymous_ballots
		WHERE election_id = $1 AND token_digest = $2`, id, tokenDigest[:]).Scan(&savedDigest, &tracker)
	if err == nil {
		if subtle.ConstantTimeCompare(savedDigest, digest[:]) != 1 {
			writeError(w, http.StatusConflict, "This credential has already been used")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ballot_hash": tracker})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		anonymousStorageError(w, err)
		return
	}
	if !open {
		writeError(w, http.StatusConflict, "Election is not accepting ballots")
		return
	}
	manifest, err := loadBallotManifest(ctx, tx, id)
	if err != nil {
		anonymousStorageError(w, err)
		return
	}
	if err := anonymousballot.Verify(manifest.scope(id, tokenDigest), manifest.PublicKey, len(manifest.Candidates), body.Vote, body.Proof); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "Invalid ballot proof")
		return
	}
	commitment := sha256.Sum256(append([]byte("evoting/anonymous-v1/ballot/"+id.String()+"/"), payload...))
	tracker = hex.EncodeToString(commitment[:])
	tag, err := tx.Exec(ctx, `
		INSERT INTO evoting.anonymous_ballots
			(election_id, token_digest, request_digest, encrypted_vote, proof, ballot_hash, credential)
		SELECT id, $2, $3, $4, $5, $6, $8 FROM evoting.elections
		WHERE id = $1 AND voting_end > clock_timestamp()
			AND (SELECT count(*) FROM evoting.anonymous_ballots WHERE election_id = $1) < $7
		ON CONFLICT (ballot_hash) DO NOTHING`, id, tokenDigest[:], digest[:], body.Vote, body.Proof, tracker, anonymousballot.MaxBallots, body.Token)
	if err != nil {
		anonymousStorageError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "Voting closed, ballot already recorded, or pilot capacity reached")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		anonymousStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"ballot_hash": tracker})
}

func anonymousStorageError(w http.ResponseWriter, err error) {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		log.Printf("anonymous voting storage error: SQLSTATE %s", pgErr.SQLState())
	} else {
		log.Print("anonymous voting storage error")
	}
	writeError(w, http.StatusInternalServerError, "Anonymous voting request could not be stored")
}

func (a *App) handleBallotTracker(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	digest, err := hex.DecodeString(r.PathValue("ballotHash"))
	if err != nil || len(digest) != 32 {
		writeError(w, http.StatusBadRequest, "Invalid ballot hash")
		return
	}
	tracker := hex.EncodeToString(digest)
	var title string
	err = a.db.QueryRow(r.Context(), `
		SELECT e.title FROM evoting.anonymous_ballots b
		JOIN evoting.elections e ON e.id = b.election_id WHERE b.ballot_hash = $1`, tracker).Scan(&title)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Ballot hash not found")
		return
	}
	if err != nil {
		anonymousStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"election_title": title, "ballot_hash": tracker, "recorded": true,
	})
}
