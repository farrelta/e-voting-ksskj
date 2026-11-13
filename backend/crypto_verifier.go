package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

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
	return generateElectionKeySet(cryptoutil.GenerateHEKeyPair)
}

func generateElectionKeySet(generateTallyKey func() ([]byte, []byte, error)) (*ElectionKeySet, error) {
	kemPub, kemPriv, err := cryptoutil.GenerateKEMKeyPair()
	if err != nil {
		return nil, fmt.Errorf("kem keygen: %w", err)
	}
	dsaPub, dsaPriv, err := cryptoutil.GenerateDSAKeyPair()
	if err != nil {
		return nil, fmt.Errorf("dsa keygen: %w", err)
	}
	hePub, heSec, err := generateTallyKey()
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
