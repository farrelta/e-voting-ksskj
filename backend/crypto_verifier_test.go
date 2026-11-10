package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/farrelta/e-voting-ksskj/backend-go/cryptoutil"
	"github.com/tuneinsight/lattigo/v6/core/rlwe"
	"github.com/tuneinsight/lattigo/v6/schemes/bgv"
)

func TestGenerateElectionKeySet(t *testing.T) {
	keys, err := GenerateElectionKeySet()
	if err != nil {
		t.Fatalf("GenerateElectionKeySet failed: %v", err)
	}
	if len(keys.KEMPublicKey) == 0 || len(keys.KEMPrivateKey) == 0 {
		t.Fatal("expected non-empty KEM keys")
	}
	if len(keys.DSAPublicKey) == 0 || len(keys.DSAPrivateKey) == 0 {
		t.Fatal("expected non-empty DSA keys")
	}
	if len(keys.HEPublicKey) == 0 || len(keys.HESecretKey) == 0 {
		t.Fatal("expected non-empty HE keys")
	}
	if len(keys.HESecretKeyFingerprint) != 64 {
		t.Fatalf("expected 64-char hex SHA256 fingerprint, got %q", keys.HESecretKeyFingerprint)
	}

	// Verify fingerprint matches SHA-256 of HESecretKey
	expectedFP := sha256.Sum256(keys.HESecretKey)
	if hex.EncodeToString(expectedFP[:]) != keys.HESecretKeyFingerprint {
		t.Fatal("fingerprint does not match SHA-256 of HE secret key")
	}
}

func TestCiphertextValidationAndTruncation(t *testing.T) {
	keys, err := GenerateElectionKeySet()
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}

	validCT, err := cryptoutil.EncryptVote(keys.HEPublicKey, 1, 3)
	if err != nil {
		t.Fatalf("EncryptVote failed: %v", err)
	}

	// 1. Verify exact expected size
	if len(validCT) != cryptoutil.ExpectedCiphertextSize {
		t.Fatalf("expected %d bytes, got %d", cryptoutil.ExpectedCiphertextSize, len(validCT))
	}

	// 2. Valid ciphertext passes
	if err := cryptoutil.ValidateCiphertext(validCT); err != nil {
		t.Fatalf("expected valid ciphertext to pass validation: %v", err)
	}

	// 3. Truncated ciphertexts must be rejected cleanly in O(1) time without crashing/stack overflow
	fractions := []float64{0.0, 0.1, 0.25, 0.5, 0.75, 0.9, 0.99}
	for _, frac := range fractions {
		cut := int(float64(len(validCT)) * frac)
		truncated := validCT[:cut]
		if err := cryptoutil.ValidateCiphertext(truncated); err == nil {
			t.Fatalf("expected truncated ciphertext (%d bytes) to be rejected", cut)
		}
	}

	// 4. Oversized blob rejected
	oversized := append(validCT, 0x01)
	if err := cryptoutil.ValidateCiphertext(oversized); err == nil {
		t.Fatal("expected oversized ciphertext to be rejected")
	}

	// 5. Corrupted bytes of exact size
	corrupted := bytes.Repeat([]byte{0xAA}, cryptoutil.ExpectedCiphertextSize)
	if err := cryptoutil.ValidateCiphertext(corrupted); err == nil {
		t.Fatal("expected corrupt payload of exact size to fail deserialization")
	}
}

func TestMLKEMEnvelopeLayer(t *testing.T) {
	keys, err := GenerateElectionKeySet()
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}

	validCT, err := cryptoutil.EncryptVote(keys.HEPublicKey, 0, 3)
	if err != nil {
		t.Fatalf("EncryptVote failed: %v", err)
	}

	// 1. Wrap the ballot inside an ML-KEM-768 + AES-256-GCM envelope
	enveloped, err := cryptoutil.SealEnvelope(keys.KEMPublicKey, validCT)
	if err != nil {
		t.Fatalf("SealEnvelope failed: %v", err)
	}

	if len(enveloped) != cryptoutil.ExpectedEnvelopedCiphertextSize {
		t.Fatalf("expected enveloped size %d, got %d", cryptoutil.ExpectedEnvelopedCiphertextSize, len(enveloped))
	}

	// 2. Unwrap ballot using the server's KEM private key
	unwrapped, err := cryptoutil.UnwrapBallot(keys.KEMPrivateKey, enveloped)
	if err != nil {
		t.Fatalf("UnwrapBallot failed on enveloped data: %v", err)
	}

	if !bytes.Equal(unwrapped, validCT) {
		t.Fatal("unwrapped ballot does not match original HE ciphertext")
	}

	// 3. UnwrapBallot also accepts already raw HE ciphertext seamlessly
	rawUnwrapped, err := cryptoutil.UnwrapBallot(keys.KEMPrivateKey, validCT)
	if err != nil {
		t.Fatalf("UnwrapBallot failed on raw ciphertext: %v", err)
	}
	if !bytes.Equal(rawUnwrapped, validCT) {
		t.Fatal("raw unwrapped ballot does not match original HE ciphertext")
	}

	// 4. Tampered envelope is rejected by GCM authentication
	tamperedEnv := append([]byte(nil), enveloped...)
	tamperedEnv[len(tamperedEnv)-1] ^= 0x01
	if _, err := cryptoutil.UnwrapBallot(keys.KEMPrivateKey, tamperedEnv); err == nil {
		t.Fatal("expected tampered envelope to be rejected by GCM")
	}
}

func TestDSASignatureVerification(t *testing.T) {
	keys, err := GenerateElectionKeySet()
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}

	validCT, err := cryptoutil.EncryptVote(keys.HEPublicKey, 0, 3)
	if err != nil {
		t.Fatalf("EncryptVote failed: %v", err)
	}

	// Server signs the ciphertext
	sig, err := cryptoutil.SignCiphertext(keys.DSAPrivateKey, validCT)
	if err != nil {
		t.Fatalf("SignCiphertext failed: %v", err)
	}

	// Verify valid signature
	ok, err := cryptoutil.VerifySignature(keys.DSAPublicKey, validCT, sig)
	if err != nil || !ok {
		t.Fatalf("expected signature to be valid, got ok=%v, err=%v", ok, err)
	}

	// Tampered ciphertext fails
	tamperedCT := append([]byte(nil), validCT...)
	tamperedCT[len(tamperedCT)/2] ^= 0xFF
	ok, err = cryptoutil.VerifySignature(keys.DSAPublicKey, tamperedCT, sig)
	if err == nil && ok {
		t.Fatal("expected tampered ciphertext to fail verification")
	}

	// Tampered signature fails
	tamperedSig := append([]byte(nil), sig...)
	tamperedSig[len(tamperedSig)/2] ^= 0xFF
	ok, err = cryptoutil.VerifySignature(keys.DSAPublicKey, validCT, tamperedSig)
	if err == nil && ok {
		t.Fatal("expected tampered signature to fail verification")
	}
}

// helper to encrypt custom raw slot values for validity testing
func encryptCustomSlots(hePubKey []byte, slots []uint64) ([]byte, error) {
	params, err := bgv.NewParametersFromLiteral(bgv.ParametersLiteral{
		LogN:             13,
		LogQ:             []int{54, 54},
		LogP:             []int{55},
		PlaintextModulus: 65537,
	})
	if err != nil {
		return nil, err
	}
	pk := rlwe.NewPublicKey(params.Parameters)
	if err := pk.UnmarshalBinary(hePubKey); err != nil {
		return nil, err
	}
	encoder := bgv.NewEncoder(params)
	encryptor := bgv.NewEncryptor(params, pk)

	values := make([]uint64, params.MaxSlots())
	copy(values, slots)

	pt := bgv.NewPlaintext(params, params.MaxLevel())
	if err := encoder.Encode(values, pt); err != nil {
		return nil, err
	}
	ct, err := encryptor.EncryptNew(pt)
	if err != nil {
		return nil, err
	}
	return ct.MarshalBinary()
}

func TestBallotValidityChecks(t *testing.T) {
	keys, err := GenerateElectionKeySet()
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}

	numCandidates := 3

	// 1. Valid ballot (candidate 1)
	validCT, err := cryptoutil.EncryptVote(keys.HEPublicKey, 1, numCandidates)
	if err != nil {
		t.Fatalf("EncryptVote failed: %v", err)
	}
	valid, err := cryptoutil.VerifyBallotValidity(keys.HESecretKey, validCT, numCandidates)
	if err != nil || !valid {
		t.Fatalf("expected valid ballot to pass: valid=%v, err=%v", valid, err)
	}

	// 2. Stuffing ballot: [1000, 0, 0]
	stuffingCT, err := encryptCustomSlots(keys.HEPublicKey, []uint64{1000, 0, 0})
	if err != nil {
		t.Fatalf("encrypt stuffing failed: %v", err)
	}
	valid, err = cryptoutil.VerifyBallotValidity(keys.HESecretKey, stuffingCT, numCandidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Fatal("expected stuffing ballot [1000, 0, 0] to be rejected")
	}

	// 3. Double voting: [1, 1, 0]
	doubleVoteCT, err := encryptCustomSlots(keys.HEPublicKey, []uint64{1, 1, 0})
	if err != nil {
		t.Fatalf("encrypt double vote failed: %v", err)
	}
	valid, err = cryptoutil.VerifyBallotValidity(keys.HESecretKey, doubleVoteCT, numCandidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Fatal("expected double vote [1, 1, 0] to be rejected")
	}

	// 4. Empty ballot: [0, 0, 0]
	emptyCT, err := encryptCustomSlots(keys.HEPublicKey, []uint64{0, 0, 0})
	if err != nil {
		t.Fatalf("encrypt empty failed: %v", err)
	}
	valid, err = cryptoutil.VerifyBallotValidity(keys.HESecretKey, emptyCT, numCandidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Fatal("expected empty vote [0, 0, 0] to be rejected")
	}

	// 5. Negative values (wrapped to 65536 in Z_p): [-1, 2, 0] -> [65536, 2, 0]
	negativeCT, err := encryptCustomSlots(keys.HEPublicKey, []uint64{65536, 2, 0})
	if err != nil {
		t.Fatalf("encrypt negative failed: %v", err)
	}
	valid, err = cryptoutil.VerifyBallotValidity(keys.HESecretKey, negativeCT, numCandidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Fatal("expected negative vote [65536, 2, 0] to be rejected")
	}
}

func TestHomomorphicTallyPipelineWithFiltering(t *testing.T) {
	keys, err := GenerateElectionKeySet()
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}

	numCandidates := 3
	// Valid votes: Candidate 0 (2 votes), Candidate 1 (1 vote), Candidate 2 (2 votes)
	validVotes := []int{0, 2, 1, 0, 2}
	expected := []uint64{2, 1, 2}

	var ciphertexts [][]byte
	for _, choice := range validVotes {
		ct, err := cryptoutil.EncryptVote(keys.HEPublicKey, choice, numCandidates)
		if err != nil {
			t.Fatalf("encrypt vote failed: %v", err)
		}
		ciphertexts = append(ciphertexts, ct)
	}

	// Infiltrate with stuffing ballot [1000, 0, 0]
	stuffingCT, err := encryptCustomSlots(keys.HEPublicKey, []uint64{1000, 0, 0})
	if err != nil {
		t.Fatalf("encrypt stuffing failed: %v", err)
	}
	ciphertexts = append(ciphertexts, stuffingCT)

	// Infiltrate with double vote [1, 1, 0]
	doubleVoteCT, err := encryptCustomSlots(keys.HEPublicKey, []uint64{1, 1, 0})
	if err != nil {
		t.Fatalf("encrypt double vote failed: %v", err)
	}
	ciphertexts = append(ciphertexts, doubleVoteCT)

	// Infiltrate with negative vote [65536, 2, 0]
	negativeCT, err := encryptCustomSlots(keys.HEPublicKey, []uint64{65536, 2, 0})
	if err != nil {
		t.Fatalf("encrypt negative failed: %v", err)
	}
	ciphertexts = append(ciphertexts, negativeCT)

	// Tally should automatically filter out the 3 invalid ballots and tally the 5 valid votes correctly
	counts, err := cryptoutil.AggregateAndDecrypt(keys.HESecretKey, ciphertexts, numCandidates)
	if err != nil {
		t.Fatalf("AggregateAndDecrypt failed: %v", err)
	}

	for i, exp := range expected {
		if counts[i] != exp {
			t.Errorf("candidate %d: expected %d votes, got %d", i, exp, counts[i])
		}
	}
}
