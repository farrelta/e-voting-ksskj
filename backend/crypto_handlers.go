package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/farrelta/e-voting-ksskj/backend-go/cryptoutil"
)

// ensureElectionKeys ensures that an election has cryptographic keys, generating them if missing.
// Note: he_secret_key is NEVER stored in the database; only its SHA-256 fingerprint is persisted.
// Private keys (kem_private_key, dsa_private_key) are encrypted at rest with the master key.
func (a *App) ensureElectionKeys(r *http.Request, electionID string) (*ElectionKeySet, error) {
	ctx := r.Context()
	var keys ElectionKeySet
	var storedKEMPriv, storedDSAPriv []byte
	err := a.db.QueryRow(ctx, `
		SELECT kem_public_key, kem_private_key,
		       dsa_public_key, dsa_private_key,
		       he_public_key, he_secret_key_fingerprint
		FROM evoting.election_keys
		WHERE election_id = $1::uuid`,
		electionID,
	).Scan(
		&keys.KEMPublicKey, &storedKEMPriv,
		&keys.DSAPublicKey, &storedDSAPriv,
		&keys.HEPublicKey, &keys.HESecretKeyFingerprint,
	)
	if err == nil {
		kemPriv, err := decryptPrivateKey(storedKEMPriv, a.cfg.MasterKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt kem private key: %w", err)
		}
		dsaPriv, err := decryptPrivateKey(storedDSAPriv, a.cfg.MasterKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt dsa private key: %w", err)
		}
		keys.KEMPrivateKey = kemPriv
		keys.DSAPrivateKey = dsaPriv
		return &keys, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	// Verify election exists before generating keys
	var exists bool
	if err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM evoting.elections WHERE id = $1::uuid)`, electionID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, pgx.ErrNoRows
	}

	newKeys, err := GenerateElectionKeySet()
	if err != nil {
		return nil, err
	}

	encKEMPriv, err := encryptPrivateKey(newKeys.KEMPrivateKey, a.cfg.MasterKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt kem private key: %w", err)
	}
	encDSAPriv, err := encryptPrivateKey(newKeys.DSAPrivateKey, a.cfg.MasterKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt dsa private key: %w", err)
	}

	_, err = a.db.Exec(ctx, `
		INSERT INTO evoting.election_keys (
			election_id, kem_public_key, kem_private_key,
			dsa_public_key, dsa_private_key, he_public_key, he_secret_key_fingerprint
		) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (election_id) DO NOTHING`,
		electionID, newKeys.KEMPublicKey, encKEMPriv,
		newKeys.DSAPublicKey, encDSAPriv,
		newKeys.HEPublicKey, newKeys.HESecretKeyFingerprint,
	)
	if err != nil {
		return nil, err
	}

	return newKeys, nil
}

// GET /api/elections/{electionId}/keys
func (a *App) handleGetElectionKeys(w http.ResponseWriter, r *http.Request) {
	electionID := r.PathValue("electionId")
	if _, err := uuid.Parse(electionID); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid election ID")
		return
	}

	keys, err := a.ensureElectionKeys(r, electionID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Election not found")
		return
	}
	if err != nil {
		log.Printf("retrieve election keys: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve election keys")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"election_id":    electionID,
		"kem_public_key": base64.StdEncoding.EncodeToString(keys.KEMPublicKey),
		"dsa_public_key": base64.StdEncoding.EncodeToString(keys.DSAPublicKey),
		"he_public_key":  base64.StdEncoding.EncodeToString(keys.HEPublicKey),
	})
}

// POST /api/elections/{electionId}/sign-ballot
func (a *App) handleSignBallot(w http.ResponseWriter, r *http.Request) {
	electionID := r.PathValue("electionId")
	if _, err := uuid.Parse(electionID); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid election ID")
		return
	}

	var body struct {
		ReceiptID     string `json:"receipt_id"`
		EncryptedVote string `json:"encrypted_vote"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ballot signing request")
		return
	}
	if _, err := uuid.Parse(body.ReceiptID); err != nil {
		writeError(w, http.StatusBadRequest, "A valid voter receipt is required")
		return
	}

	encryptedVote, err := base64.StdEncoding.DecodeString(body.EncryptedVote)
	if err != nil || len(encryptedVote) == 0 || len(encryptedVote) > 1024*1024 {
		writeError(w, http.StatusBadRequest, "Encrypted vote must be valid base64 data no larger than 1 MB")
		return
	}

	keys, err := a.ensureElectionKeys(r, electionID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Election not found")
		return
	}
	if err != nil {
		log.Printf("retrieve election keys for signing: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve signing key")
		return
	}

	// Unwrap ML-KEM envelope if enveloped, or accept raw HE ciphertext
	rawCT, err := cryptoutil.UnwrapBallot(keys.KEMPrivateKey, encryptedVote)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to unwrap ballot envelope")
		return
	}

	// Validate BFV ciphertext format & exact size
	if err := cryptoutil.ValidateCiphertext(rawCT); err != nil {
		writeError(w, http.StatusBadRequest, "Encrypted vote is not a valid ciphertext")
		return
	}

	ctx := r.Context()
	var eligible bool
	err = a.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM evoting.voter_eligibility ve
			JOIN evoting.elections e ON e.id = ve.election_id
			WHERE ve.election_id = $1::uuid
				AND ve.user_id = $2::uuid
				AND ve.receipt_id = $3::uuid
				AND ve.has_voted = FALSE
				AND e.status = 'active_voting'
		)`,
		electionID, sessionFrom(r).User.ID, body.ReceiptID,
	).Scan(&eligible)
	if err != nil {
		log.Printf("check eligibility for signing: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to authorize ballot signature")
		return
	}
	if !eligible {
		writeError(w, http.StatusForbidden, "No active, unused voter registration was found for this ballot")
		return
	}

	signature, err := cryptoutil.SignCiphertext(keys.DSAPrivateKey, rawCT)
	if err != nil {
		log.Printf("sign ciphertext: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to sign ballot")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"proof": base64.StdEncoding.EncodeToString(signature),
	})
}

// POST /api/admin/elections/{electionId}/tally
func (a *App) handleTallyElection(w http.ResponseWriter, r *http.Request) {
	electionID := r.PathValue("electionId")
	if _, err := uuid.Parse(electionID); err != nil {
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
	var votingEnd time.Time
	err = tx.QueryRow(r.Context(), `
		SELECT status, voting_end FROM evoting.elections
		WHERE id = $1::uuid FOR UPDATE`,
		electionID,
	).Scan(&status, &votingEnd)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Election not found")
		return
	}
	if err != nil {
		log.Printf("query election status for tally: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to process tally")
		return
	}
	if status != "active_voting" && status != "completed" {
		writeError(w, http.StatusConflict, "Election is not ready for tallying (must be in active_voting or completed status)")
		return
	}

	// Must reject tally if voting has not yet ended, unless already completed
	if time.Now().Before(votingEnd) && status != "completed" {
		writeError(w, http.StatusConflict, "Voting is still active; tally cannot be performed before voting_end")
		return
	}

	// 2. Fetch DSA public key and HE secret key fingerprint, and verify the provided HE key
	var dsaPubKey []byte
	var expectedFingerprint string
	err = tx.QueryRow(r.Context(), `
		SELECT dsa_public_key, he_secret_key_fingerprint
		FROM evoting.election_keys
		WHERE election_id = $1::uuid`,
		electionID,
	).Scan(&dsaPubKey, &expectedFingerprint)
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

	// 3. Fetch candidates ordered deterministically
	rows, err := tx.Query(r.Context(), `
		SELECT id::text, name
		FROM evoting.candidates
		WHERE election_id = $1::uuid
		ORDER BY name, id`,
		electionID,
	)
	if err != nil {
		log.Printf("query candidates for tally: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve election candidates")
		return
	}
	defer rows.Close()

	type candItem struct {
		ID   string
		Name string
	}
	var candidates []candItem
	for rows.Next() {
		var c candItem
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			log.Printf("scan candidate for tally: %v", err)
			writeError(w, http.StatusInternalServerError, "Failed to retrieve election candidates")
			return
		}
		candidates = append(candidates, c)
	}
	rows.Close()

	if len(candidates) == 0 {
		writeError(w, http.StatusBadRequest, "Cannot tally an election with no candidates")
		return
	}

	// 4. Fetch all stored ballots and signatures
	ballotRows, err := tx.Query(r.Context(), `
		SELECT encrypted_vote, proof
		FROM evoting.encrypted_ballots
		WHERE election_id = $1::uuid`,
		electionID,
	)
	if err != nil {
		log.Printf("query encrypted ballots for tally: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve ballots")
		return
	}
	defer ballotRows.Close()

	var validCiphertexts [][]byte
	totalBallots := 0

	for ballotRows.Next() {
		totalBallots++
		var ct, proof []byte
		if err := ballotRows.Scan(&ct, &proof); err != nil {
			log.Printf("scan encrypted ballot: %v", err)
			writeError(w, http.StatusInternalServerError, "Failed to retrieve ballots")
			return
		}

		// Security check 1: Verify server ML-DSA-65 signature on stored ciphertext.
		// Rejects any rows directly inserted into database bypassing server submission.
		validSig, err := cryptoutil.VerifySignature(dsaPubKey, ct, proof)
		if err != nil || !validSig {
			log.Printf("tally: skipping ballot with invalid or missing server signature")
			continue
		}

		// Security check 2: Verify ballot encodes a valid one-hot vector.
		// Rejects ballot stuffing (e.g. 1000 votes), double voting, or negative values.
		validBallot, err := cryptoutil.VerifyBallotValidity(heSecKey, ct, len(candidates))
		if err != nil || !validBallot {
			log.Printf("tally: skipping ballot with invalid vote contents (stuffing/double vote/negative)")
			continue
		}

		validCiphertexts = append(validCiphertexts, ct)
	}
	ballotRows.Close()

	// 5. Homomorphic aggregation and decryption using the admin-supplied secret key
	counts, err := cryptoutil.AggregateAndDecrypt(heSecKey, validCiphertexts, len(candidates))
	if err != nil {
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
		"total_ballots": totalBallots,
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
