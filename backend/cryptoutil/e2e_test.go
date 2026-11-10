package cryptoutil

import "testing"

// TestFullElectionPipeline simulates 5 dummy voters choosing among 3
// candidates, running the full encrypt -> sign -> verify -> tally pipeline,
// and checks that the decrypted result matches the expected counts.
func TestFullElectionPipeline(t *testing.T) {
	// 1. Election setup: generate all key material once.
	_, _, err := GenerateKEMKeyPair()
	if err != nil {
		t.Fatalf("kem keygen: %v", err)
	}
	dsaPub, dsaPriv, err := GenerateDSAKeyPair()
	if err != nil {
		t.Fatalf("dsa keygen: %v", err)
	}
	hePub, heSec, err := GenerateHEKeyPair()
	if err != nil {
		t.Fatalf("he keygen: %v", err)
	}

	numCandidates := 3
	// dummy votes: candidate 0 gets 2 votes, candidate 1 gets 1, candidate 2 gets 2
	dummyVotes := []int{0, 0, 1, 2, 2}
	expected := map[int]int{0: 2, 1: 1, 2: 2}

	var ciphertexts [][]byte

	for i, candidateID := range dummyVotes {
		ct, err := EncryptVote(hePub, candidateID, numCandidates)
		if err != nil {
			t.Fatalf("voter %d: encrypt failed: %v", i, err)
		}
		sig, err := SignCiphertext(dsaPriv, ct)
		if err != nil {
			t.Fatalf("voter %d: sign failed: %v", i, err)
		}
		valid, err := VerifySignature(dsaPub, ct, sig)
		if err != nil {
			t.Fatalf("voter %d: verify error: %v", i, err)
		}
		if !valid {
			t.Fatalf("voter %d: signature did not verify", i)
		}
		ciphertexts = append(ciphertexts, ct)
	}

	// Tamper check: a flipped byte must fail verification.
	tampered := append([]byte(nil), ciphertexts[0]...)
	tampered[0] ^= 0xFF
	tamperSig, _ := SignCiphertext(dsaPriv, ciphertexts[0])
	if valid, _ := VerifySignature(dsaPub, tampered, tamperSig); valid {
		t.Fatalf("tampered ciphertext incorrectly verified as valid")
	}

	counts, err := AggregateAndDecrypt(heSec, ciphertexts, numCandidates)
	if err != nil {
		t.Fatalf("tally failed: %v", err)
	}

	for candidateID, want := range expected {
		if int(counts[candidateID]) != want {
			t.Errorf("candidate %d: got %d votes, want %d", candidateID, counts[candidateID], want)
		}
	}
	t.Logf("tally result: %v (expected %v) — individual votes were never decrypted", counts, expected)
}
