package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"math/big"
	"net/http"

	"github.com/cloudflare/circl/blindsign/blindrsa"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	credentialRSABits     = 3072
	credentialRSABytes    = credentialRSABits / 8
	credentialPrefixBytes = 32 // RFC 9474 PrepareRandomize
	credentialNonceBytes  = 32
	credentialDomain      = "evoting/anonymous-v1/credential/"
)

func newAnonymousIssuerKey(masterKey []byte) (public, encrypted []byte, err error) {
	key, err := rsa.GenerateKey(rand.Reader, credentialRSABits)
	if err != nil {
		return nil, nil, err
	}
	public, err = x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, nil, err
	}
	private := x509.MarshalPKCS1PrivateKey(key)
	defer clear(private)
	encrypted, err = encryptPrivateKey(private, masterKey)
	return public, encrypted, err
}

func signCredential(masterKey, public, encrypted, blinded []byte) ([]byte, error) {
	if len(blinded) != credentialRSABytes || !bytes.HasPrefix(encrypted, []byte(keyEncPrefix)) {
		return nil, errors.New("invalid credential signing inputs")
	}
	private, err := decryptPrivateKey(encrypted, masterKey)
	if err != nil {
		return nil, err
	}
	defer clear(private)
	key, err := x509.ParsePKCS1PrivateKey(private)
	if err != nil {
		return nil, err
	}
	derived, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil || !bytes.Equal(derived, public) || key.N.BitLen() != credentialRSABits || key.E != 65537 {
		return nil, errors.New("credential issuer key does not match")
	}
	representative := new(big.Int).SetBytes(blinded)
	if representative.Sign() <= 0 || representative.Cmp(key.N) >= 0 {
		return nil, errors.New("invalid blinded credential")
	}
	return blindrsa.NewSigner(key).BlindSign(blinded)
}

func (a *App) verifyCredential(ctx context.Context, id uuid.UUID, token []byte) ([32]byte, error) {
	var digest [32]byte
	domain := []byte(credentialDomain + id.String() + "/")
	messageEnd := credentialPrefixBytes + len(domain) + credentialNonceBytes
	if len(token) != messageEnd+credentialRSABytes ||
		!bytes.Equal(token[credentialPrefixBytes:credentialPrefixBytes+len(domain)], domain) {
		return digest, errors.New("invalid credential format or election")
	}
	var encodedKey []byte
	if err := a.db.QueryRow(ctx, `
		SELECT public_key FROM evoting.anonymous_issuer_keys WHERE election_id = $1`, id).Scan(&encodedKey); err != nil {
		return digest, err
	}
	return verifySignedCredential(id, token, encodedKey)
}

func verifySignedCredential(id uuid.UUID, token, encodedKey []byte) ([32]byte, error) {
	var digest [32]byte
	domain := []byte(credentialDomain + id.String() + "/")
	messageEnd := credentialPrefixBytes + len(domain) + credentialNonceBytes
	if len(token) != messageEnd+credentialRSABytes ||
		!bytes.Equal(token[credentialPrefixBytes:credentialPrefixBytes+len(domain)], domain) {
		return digest, errors.New("invalid credential format or election")
	}
	parsed, err := x509.ParsePKIXPublicKey(encodedKey)
	if err != nil {
		return digest, err
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok || key.N.BitLen() != credentialRSABits || key.E != 65537 {
		return digest, errors.New("invalid credential verification key")
	}
	verifier, err := blindrsa.NewVerifier(blindrsa.SHA384PSSRandomized, key)
	if err != nil {
		return digest, err
	}
	if err := verifier.Verify(token[:messageEnd], token[messageEnd:]); err != nil {
		return digest, err
	}
	// The digest identifies the signed nonce even if signature bytes change.
	return sha256.Sum256(token[credentialPrefixBytes:messageEnd]), nil
}

func (a *App) anonymousBallotPilot() bool {
	return a.cfg.IsDev() && a.cfg.AnonymousBallotPilot
}

func (a *App) handleCredentialKey(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !a.anonymousBallotPilot() {
		writeError(w, http.StatusServiceUnavailable, "Anonymous credentials are not configured")
		return
	}
	id, err := uuid.Parse(r.PathValue("electionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid election ID")
		return
	}
	var public []byte
	err = a.db.QueryRow(r.Context(), `
		SELECT k.public_key FROM evoting.anonymous_issuer_keys k
		JOIN evoting.voter_eligibility ve ON ve.election_id = k.election_id
		WHERE k.election_id = $1 AND ve.user_id = $2::uuid`,
		id, sessionFrom(r).User.ID).Scan(&public)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "No eligible anonymous election")
		return
	}
	if err != nil {
		anonymousStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]byte{"public_key": public})
}
