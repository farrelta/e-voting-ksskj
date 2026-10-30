//go:build ignore

// This file is NOT part of the compiled package (see the "ignore" build tag
// above) — it's a reference showing how your teammate's backend would call
// this package directly from their own route handlers, once cryptoutil/ is
// merged into (or imported by) their Go module.
//
// Delete or keep this file for reference; it will not affect `go build`.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"

	"evoting-crypto/cryptoutil" // adjust import path once merged — see README
)

// ----- Example 1: Admin creates an election -----

func exampleGenerateElectionKeys() {
	kemPub, kemPriv, err := cryptoutil.GenerateKEMKeyPair()
	if err != nil {
		log.Fatal(err)
	}
	dsaPub, dsaPriv, err := cryptoutil.GenerateDSAKeyPair()
	if err != nil {
		log.Fatal(err)
	}
	hePub, heSec, err := cryptoutil.GenerateHEKeyPair()
	if err != nil {
		log.Fatal(err)
	}

	// Store these against the election record in your database.
	// kem_private_key, dsa_private_key, he_secret_key are secrets —
	// store them encrypted at rest / in a secrets manager, not in plain columns.
	election := map[string]string{
		"kem_public_key":  base64.StdEncoding.EncodeToString(kemPub),
		"kem_private_key": base64.StdEncoding.EncodeToString(kemPriv),
		"dsa_public_key":  base64.StdEncoding.EncodeToString(dsaPub),
		"dsa_private_key": base64.StdEncoding.EncodeToString(dsaPriv),
		"he_public_key":   base64.StdEncoding.EncodeToString(hePub),
		"he_secret_key":   base64.StdEncoding.EncodeToString(heSec),
	}
	out, _ := json.MarshalIndent(election, "", "  ")
	fmt.Println(string(out))
	// db.SaveElection(election) <- your teammate's code
}

// ----- Example 2: Voter submits a ballot (inside your HTTP handler) -----

func exampleHandleVoteSubmission(hePubBase64, dsaPrivBase64 string, candidateID, numCandidates int) (ciphertextB64, signatureB64 string, err error) {
	hePub, err := base64.StdEncoding.DecodeString(hePubBase64)
	if err != nil {
		return "", "", fmt.Errorf("invalid he public key: %w", err)
	}
	dsaPriv, err := base64.StdEncoding.DecodeString(dsaPrivBase64)
	if err != nil {
		return "", "", fmt.Errorf("invalid dsa private key: %w", err)
	}

	// 1. Encrypt the vote (one-hot encoded under the hood)
	ciphertext, err := cryptoutil.EncryptVote(hePub, candidateID, numCandidates)
	if err != nil {
		return "", "", fmt.Errorf("encrypt failed: %w", err)
	}

	// 2. Sign the ciphertext (proves authenticity/integrity, not the choice itself)
	signature, err := cryptoutil.SignCiphertext(dsaPriv, ciphertext)
	if err != nil {
		return "", "", fmt.Errorf("sign failed: %w", err)
	}

	return base64.StdEncoding.EncodeToString(ciphertext),
		base64.StdEncoding.EncodeToString(signature),
		nil

	// Your teammate's handler then does:
	//   db.SaveVote(electionID, ciphertextB64, signatureB64)
}

// ----- Example 3: Verify a ballot on ingest (optional, before storing it) -----

func exampleVerifyBallot(dsaPubBase64, ciphertextB64, signatureB64 string) (bool, error) {
	dsaPub, err := base64.StdEncoding.DecodeString(dsaPubBase64)
	if err != nil {
		return false, err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return false, err
	}
	signature, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return false, err
	}
	return cryptoutil.VerifySignature(dsaPub, ciphertext, signature)
}

// ----- Example 4: Admin triggers tally after voting closes -----

func exampleTally(heSecBase64 string, allCiphertextsB64 []string, numCandidates int, candidateNames []string) (map[string]int, error) {
	heSec, err := base64.StdEncoding.DecodeString(heSecBase64)
	if err != nil {
		return nil, err
	}

	ciphertexts := make([][]byte, 0, len(allCiphertextsB64))
	for _, c := range allCiphertextsB64 {
		raw, err := base64.StdEncoding.DecodeString(c)
		if err != nil {
			return nil, fmt.Errorf("invalid ciphertext: %w", err)
		}
		ciphertexts = append(ciphertexts, raw)
	}

	counts, err := cryptoutil.AggregateAndDecrypt(heSec, ciphertexts, numCandidates)
	if err != nil {
		return nil, fmt.Errorf("tally failed: %w", err)
	}

	results := make(map[string]int, len(counts))
	for i, c := range counts {
		name := candidateNames[i]
		results[name] = int(c)
	}
	return results, nil
	// Your teammate's handler then does:
	//   db.SaveResults(electionID, results)
	//   return results as JSON to the public results page
}
