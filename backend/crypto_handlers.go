package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/farrelta/e-voting-ksskj/backend-go/anonymousballot"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// POST /api/admin/elections/{electionId}/tally
func (a *App) handleTallyElection(w http.ResponseWriter, r *http.Request) {
	electionID := r.PathValue("electionId")
	electionUUID, err := uuid.Parse(electionID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid election ID")
		return
	}

	// Admin must provide he_secret_key in request body (it is NEVER stored in database)
	var body struct {
		HESecretKey string `json:"he_secret_key"`
	}
	if err := decodeJSON(w, r, &body); err != nil || body.HESecretKey == "" {
		writeError(w, http.StatusBadRequest, "he_secret_key is required to perform tally")
		return
	}

	heSecKey, err := base64.StdEncoding.DecodeString(body.HESecretKey)
	if err != nil || len(heSecKey) == 0 {
		writeError(w, http.StatusBadRequest, "Invalid base64 encoding for he_secret_key")
		return
	}
	defer func() {
		// Securely zero out the secret key in memory once tallying completes
		for i := range heSecKey {
			heSecKey[i] = 0
		}
	}()

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		log.Printf("begin tally transaction: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to process tally")
		return
	}
	defer tx.Rollback(r.Context())

	// 1. Lock election and verify status and voting_end time
	var status string
	var hasIssuer bool
	var votingEnd time.Time
	err = tx.QueryRow(r.Context(), `
		SELECT status, voting_end, EXISTS (
			SELECT 1 FROM evoting.anonymous_issuer_keys k WHERE k.election_id = elections.id
		) FROM evoting.elections
		WHERE id = $1::uuid FOR UPDATE`,
		electionID,
	).Scan(&status, &votingEnd, &hasIssuer)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Election not found")
		return
	}
	if err != nil {
		log.Printf("query election status for tally: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to process tally")
		return
	}
	if !hasIssuer {
		writeError(w, http.StatusGone, "This election is archived")
		return
	}
	if status != "active_voting" && status != "completed" {
		writeError(w, http.StatusConflict, "Election is not ready for tallying (must be in active_voting or completed status)")
		return
	}

	if time.Now().Before(votingEnd) {
		writeError(w, http.StatusConflict, "Voting is still active; tally cannot be performed before voting_end")
		return
	}
	if !a.anonymousBallotPilot() {
		writeError(w, http.StatusServiceUnavailable, "Anonymous tally is not configured")
		return
	}

	// Verify the tally key against the stored fingerprint.
	var expectedFingerprint string
	err = tx.QueryRow(r.Context(), `
		SELECT he_secret_key_fingerprint
		FROM evoting.election_keys
		WHERE election_id = $1::uuid`,
		electionID,
	).Scan(&expectedFingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Election cryptographic keys not found")
		return
	}
	if err != nil {
		log.Printf("query election keys for tally: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve election keys")
		return
	}

	providedFingerprint := sha256.Sum256(heSecKey)
	if hex.EncodeToString(providedFingerprint[:]) != expectedFingerprint {
		writeError(w, http.StatusUnauthorized, "Provided he_secret_key does not match election fingerprint")
		return
	}

	manifest, err := loadBallotManifest(r.Context(), tx, electionUUID)
	if err != nil {
		anonymousStorageError(w, err)
		return
	}
	candidates := manifest.Candidates

	ballotRows, err := tx.Query(r.Context(), `
		SELECT encrypted_vote, proof, token_digest, credential
		FROM evoting.anonymous_ballots WHERE election_id = $1::uuid`, electionID)
	if err != nil {
		log.Printf("query encrypted ballots for tally: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve ballots")
		return
	}
	defer ballotRows.Close()

	var validCiphertexts [][]byte

	for ballotRows.Next() {
		var ct, proof, tokenDigest, credential []byte
		if err := ballotRows.Scan(&ct, &proof, &tokenDigest, &credential); err != nil {
			log.Printf("scan encrypted ballot: %v", err)
			writeError(w, http.StatusInternalServerError, "Failed to retrieve ballots")
			return
		}
		digest, err := verifySignedCredential(electionUUID, credential, manifest.issuerKey)
		if err != nil || subtle.ConstantTimeCompare(digest[:], tokenDigest) != 1 {
			writeError(w, http.StatusConflict, "An anonymous ballot has an invalid credential")
			return
		}
		if err := anonymousballot.Verify(manifest.scope(electionUUID, digest), manifest.PublicKey, len(candidates), ct, proof); err != nil {
			writeError(w, http.StatusConflict, "An anonymous ballot failed verification")
			return
		}
		validCiphertexts = append(validCiphertexts, ct)
	}
	if err := ballotRows.Err(); err != nil {
		log.Printf("iterate tally ballots: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve ballots")
		return
	}
	ballotRows.Close()

	// 5. Homomorphic aggregation and decryption using the admin-supplied secret key
	counts, err := anonymousballot.Tally(heSecKey, len(candidates), validCiphertexts)
	if err != nil || len(counts) != len(candidates) {
		log.Printf("aggregate and decrypt: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to aggregate encrypted ballots")
		return
	}

	type CandidateTally struct {
		CandidateID string `json:"candidate_id"`
		Name        string `json:"name"`
		Votes       uint64 `json:"votes"`
	}
	tallyList := make([]CandidateTally, len(candidates))
	for i, c := range candidates {
		tallyList[i] = CandidateTally{
			CandidateID: c.ID,
			Name:        c.Name,
			Votes:       counts[i],
		}
	}

	resultsJSON, err := json.Marshal(tallyList)
	if err != nil {
		log.Printf("marshal tally results: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to serialize results")
		return
	}

	// 6. Save results and update status to completed
	_, err = tx.Exec(r.Context(), `
		INSERT INTO evoting.election_results (election_id, total_ballots, results, tallied_at)
		VALUES ($1::uuid, $2, $3, CURRENT_TIMESTAMP)
		ON CONFLICT (election_id) DO UPDATE
		SET total_ballots = EXCLUDED.total_ballots,
			results = EXCLUDED.results,
			tallied_at = EXCLUDED.tallied_at`,
		electionID, len(validCiphertexts), resultsJSON,
	)
	if err != nil {
		log.Printf("save election results: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to save tally results")
		return
	}

	_, err = tx.Exec(r.Context(), `
		UPDATE evoting.elections SET status = 'completed' WHERE id = $1::uuid`,
		electionID,
	)
	if err != nil {
		log.Printf("set election completed: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to update election status")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		log.Printf("commit tally: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to finalize tally")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"election_id":   electionID,
		"total_ballots": len(validCiphertexts),
		"valid_ballots": len(validCiphertexts),
		"results":       tallyList,
		"status":        "completed",
	})
}

// GET /api/elections/{electionId}/results
// Public endpoint: results are visible to anyone once tallied.
func (a *App) handleGetElectionResults(w http.ResponseWriter, r *http.Request) {
	electionID := r.PathValue("electionId")
	if _, err := uuid.Parse(electionID); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid election ID")
		return
	}

	var totalBallots int
	var resultsJSON []byte
	var talliedAt time.Time
	err := a.db.QueryRow(r.Context(), `
		SELECT total_ballots, results, tallied_at
		FROM evoting.election_results
		WHERE election_id = $1::uuid`,
		electionID,
	).Scan(&totalBallots, &resultsJSON, &talliedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Election results not yet available")
		return
	}
	if err != nil {
		log.Printf("query election results: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve results")
		return
	}

	var results []any
	if err := json.Unmarshal(resultsJSON, &results); err != nil {
		results = []any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"election_id":   electionID,
		"total_ballots": totalBallots,
		"results":       results,
		"tallied_at":    talliedAt,
	})
}
