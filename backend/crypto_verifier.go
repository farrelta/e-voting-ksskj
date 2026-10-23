package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/farrelta/e-voting-ksskj/backend-go/cryptoutil"
)

type ElectionKeySet struct {
	KEMPublicKey           []byte
	KEMPrivateKey          []byte
	DSAPublicKey           []byte
	DSAPrivateKey          []byte
	HEPublicKey            []byte
	HESecretKey            []byte // only held in memory at keygen; NOT stored in DB!
	HESecretKeyFingerprint string // SHA-256 fingerprint stored in DB
}

func GenerateElectionKeySet() (*ElectionKeySet, error) {
	kemPub, kemPriv, err := cryptoutil.GenerateKEMKeyPair()
	if err != nil {
		return nil, fmt.Errorf("kem keygen: %w", err)
	}
	dsaPub, dsaPriv, err := cryptoutil.GenerateDSAKeyPair()
	if err != nil {
		return nil, fmt.Errorf("dsa keygen: %w", err)
	}
	hePub, heSec, err := cryptoutil.GenerateHEKeyPair()
	if err != nil {
		return nil, fmt.Errorf("he keygen: %w", err)
	}

	fingerprint := sha256.Sum256(heSec)
	return &ElectionKeySet{
		KEMPublicKey:           kemPub,
		KEMPrivateKey:          kemPriv,
		DSAPublicKey:           dsaPub,
		DSAPrivateKey:          dsaPriv,
		HEPublicKey:            hePub,
		HESecretKey:            heSec,
		HESecretKeyFingerprint: hex.EncodeToString(fingerprint[:]),
	}, nil
}

type DefaultBallotVerifier struct {
	db *pgxpool.Pool
}

func NewBallotVerifier(db *pgxpool.Pool) *DefaultBallotVerifier {
	return &DefaultBallotVerifier{db: db}
}

func (v *DefaultBallotVerifier) Verify(ctx context.Context, electionID string, encryptedVote, proof []byte) error {
	if len(encryptedVote) == 0 {
		return errors.New("empty encrypted vote")
	}

	// 1. Fetch election keys (KEM private key for unwrapping envelope, DSA public key for signature check)
	var kemPrivKey, dsaPubKey []byte
	err := v.db.QueryRow(ctx, `
		SELECT kem_private_key, dsa_public_key
		FROM evoting.election_keys
		WHERE election_id = $1::uuid`,
		electionID,
	).Scan(&kemPrivKey, &dsaPubKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("election signing key not found")
	}
	if err != nil {
		return fmt.Errorf("retrieve election keys: %w", err)
	}

	// 2. Unwrap ML-KEM envelope if enveloped, or accept raw HE ciphertext
	rawCT, err := cryptoutil.UnwrapBallot(kemPrivKey, encryptedVote)
	if err != nil {
		return fmt.Errorf("unwrap ballot: %w", err)
	}

	// 3. Validate exact ciphertext length and structure (prevents stack overflow)
	if err := cryptoutil.ValidateCiphertext(rawCT); err != nil {
		return fmt.Errorf("invalid ballot ciphertext: %w", err)
	}

	// 4. Verify DSA signature if client provided proof
	if len(proof) > 0 {
		valid, err := cryptoutil.VerifySignature(dsaPubKey, rawCT, proof)
		if err != nil {
			return fmt.Errorf("verify signature: %w", err)
		}
		if !valid {
			return errors.New("ballot signature verification failed")
		}
	}

	return nil
}
