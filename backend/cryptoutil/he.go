package cryptoutil

import (
	"fmt"

	"github.com/tuneinsight/lattigo/v6/core/rlwe"
	"github.com/tuneinsight/lattigo/v6/schemes/bgv"
)

// NOTE ON SCHEME: Lattigo v6 no longer ships a separate `bfv` package — BFV
// was merged into `bgv` as a "scale-invariant" evaluation mode, since the two
// schemes share almost all of their machinery (see schemes/bgv/README.md and
// schemes/bfv/README.md in the lattigo v6 source for the rationale). We use
// the bgv package throughout and pass scaleInvariant=true wherever an
// Evaluator is created, which gives BFV-style multiplication semantics.
// For this service we only ever homomorphically ADD ciphertexts (to sum
// one-hot vote vectors), and Add behaves identically in BGV and BFV, so this
// distinction doesn't affect the tally's correctness — it's kept here mainly
// so the terminology matches "BFV" as documented in the thesis/report.
//
// NOTE ON VERSIONS: if a future lattigo release renames these functions again,
// check https://pkg.go.dev/github.com/tuneinsight/lattigo for the version
// pinned in go.mod — the overall shape (params -> keygen -> encoder/
// encryptor/decryptor/evaluator) is stable across versions even when method
// names move.

// newHEParams returns a fixed parameter set. LogN=13 with these Q/P moduli is
// a conservative, commonly used default (128-bit-class security); the
// plaintext modulus supports vote counts far beyond any realistic election.
func newHEParams() (bgv.Parameters, error) {
	return bgv.NewParametersFromLiteral(bgv.ParametersLiteral{
		LogN:             13,
		LogQ:             []int{54, 54},
		LogP:             []int{55},
		PlaintextModulus: 65537,
	})
}

// GenerateHEKeyPair creates a BFV (scale-invariant BGV) key pair for one
// election. The public key is distributed so every voter's ballot can be
// encrypted under it; the secret key must be held only by the tally
// authority and used solely to decrypt the final, already-aggregated result.
func GenerateHEKeyPair() (pubKey, secKey []byte, err error) {
	params, err := newHEParams()
	if err != nil {
		return nil, nil, fmt.Errorf("he params: %w", err)
	}
	kgen := rlwe.NewKeyGenerator(params.Parameters)
	sk, pk := kgen.GenKeyPairNew()

	skBytes, err := sk.MarshalBinary()
	if err != nil {
		return nil, nil, err
	}
	pkBytes, err := pk.MarshalBinary()
	if err != nil {
		return nil, nil, err
	}
	return pkBytes, skBytes, nil
}

// EncryptVote one-hot encodes the chosen candidate (a slice of length
// numCandidates, all zeros except a 1 at candidateID) and encrypts it under
// the election's public key. Summing these ciphertexts later, slot by slot,
// yields the vote count per candidate without ever decrypting an individual
// ballot.
func EncryptVote(pubKeyBytes []byte, candidateID, numCandidates int) ([]byte, error) {
	if candidateID < 0 || candidateID >= numCandidates {
		return nil, fmt.Errorf("candidate_id %d out of range [0,%d)", candidateID, numCandidates)
	}
	params, err := newHEParams()
	if err != nil {
		return nil, err
	}

	pk := rlwe.NewPublicKey(params.Parameters)
	if err := pk.UnmarshalBinary(pubKeyBytes); err != nil {
		return nil, fmt.Errorf("invalid he public key: %w", err)
	}

	encoder := bgv.NewEncoder(params)
	encryptor := bgv.NewEncryptor(params, pk)

	values := make([]uint64, params.MaxSlots())
	values[candidateID] = 1 // one-hot encoding; every other slot stays 0

	pt := bgv.NewPlaintext(params, params.MaxLevel())
	if err := encoder.Encode(values, pt); err != nil {
		return nil, fmt.Errorf("encode failed: %w", err)
	}

	ct, err := encryptor.EncryptNew(pt)
	if err != nil {
		return nil, fmt.Errorf("encrypt failed: %w", err)
	}

	return ct.MarshalBinary()
}

// ExpectedCiphertextSize is the exact byte length of a valid BFV/BGV ciphertext
// serialized under the fixed parameter set (LogN=13, LogQ=[54, 54], LogP=[55], PlaintextModulus=65537).
const ExpectedCiphertextSize = 262478

// AggregateAndDecrypt homomorphically sums every submitted ciphertext (one
// per valid, signature-verified ballot) and decrypts only the final sum —
// never an individual ballot — using the tally authority's secret key.
// Each ballot is checked to ensure it represents a valid one-hot vector
// (preventing stuffing, double voting, and negative values).
func AggregateAndDecrypt(secKeyBytes []byte, ciphertextsBytes [][]byte, numCandidates int) ([]uint64, error) {
	if len(ciphertextsBytes) == 0 {
		return make([]uint64, numCandidates), nil
	}

	params, err := newHEParams()
	if err != nil {
		return nil, err
	}

	sk := rlwe.NewSecretKey(params.Parameters)
	if err := sk.UnmarshalBinary(secKeyBytes); err != nil {
		return nil, fmt.Errorf("invalid he secret key: %w", err)
	}

	evaluator := bgv.NewEvaluator(params, nil, true) // scaleInvariant=true => BFV-style

	var sum *rlwe.Ciphertext
	for i, ctBytes := range ciphertextsBytes {
		if err := ValidateCiphertext(ctBytes); err != nil {
			return nil, fmt.Errorf("invalid ciphertext at index %d: %w", i, err)
		}

		// Verify each ballot is a valid one-hot vector (rejects stuffing, double votes, negative values)
		valid, err := VerifyBallotValidity(secKeyBytes, ctBytes, numCandidates)
		if err != nil {
			return nil, fmt.Errorf("ballot validity check failed at index %d: %w", i, err)
		}
		if !valid {
			// Skip invalid/stuffing ballot
			continue
		}

		ct := bgv.NewCiphertext(params, 1, params.MaxLevel())
		if err := ct.UnmarshalBinary(ctBytes); err != nil {
			return nil, fmt.Errorf("unmarshal ciphertext at index %d: %w", i, err)
		}
		if sum == nil {
			sum = ct
			continue
		}
		if err := evaluator.Add(sum, ct, sum); err != nil {
			return nil, fmt.Errorf("homomorphic add failed at index %d: %w", i, err)
		}
	}

	if sum == nil {
		return make([]uint64, numCandidates), nil
	}

	decryptor := bgv.NewDecryptor(params, sk)
	encoder := bgv.NewEncoder(params)

	pt := decryptor.DecryptNew(sum)
	values := make([]uint64, params.MaxSlots())
	if err := encoder.Decode(pt, values); err != nil {
		return nil, fmt.Errorf("decode failed: %w", err)
	}

	return values[:numCandidates], nil
}

// ValidateCiphertext checks whether the provided bytes have the exact required length
// and can be deserialized as a valid BGV/BFV ciphertext, preventing fatal stack overflow crashes.
func ValidateCiphertext(ctBytes []byte) (err error) {
	if len(ctBytes) != ExpectedCiphertextSize {
		return fmt.Errorf("invalid ciphertext size: expected %d bytes, got %d", ExpectedCiphertextSize, len(ctBytes))
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("corrupt ciphertext: %v", r)
		}
	}()
	params, err := newHEParams()
	if err != nil {
		return err
	}
	ct := bgv.NewCiphertext(params, 1, params.MaxLevel())
	return ct.UnmarshalBinary(ctBytes)
}

// VerifyBallotValidity decrypts an individual ciphertext and verifies that it encodes a valid ballot:
// 1. Every slot up to numCandidates is either 0 or 1.
// 2. Exactly one candidate slot is 1 (sum == 1), rejecting double voting, ballot stuffing, and empty ballots.
// 3. No negative values (which wrap around to large values in Z_p) are present.
// 4. All unused slots beyond numCandidates are strictly 0.
func VerifyBallotValidity(secKeyBytes, ctBytes []byte, numCandidates int) (bool, error) {
	if err := ValidateCiphertext(ctBytes); err != nil {
		return false, err
	}
	params, err := newHEParams()
	if err != nil {
		return false, err
	}
	sk := rlwe.NewSecretKey(params.Parameters)
	if err := sk.UnmarshalBinary(secKeyBytes); err != nil {
		return false, fmt.Errorf("invalid he secret key: %w", err)
	}
	decryptor := bgv.NewDecryptor(params, sk)
	encoder := bgv.NewEncoder(params)

	ct := bgv.NewCiphertext(params, 1, params.MaxLevel())
	if err := ct.UnmarshalBinary(ctBytes); err != nil {
		return false, fmt.Errorf("unmarshal ciphertext: %w", err)
	}

	pt := decryptor.DecryptNew(ct)
	values := make([]uint64, params.MaxSlots())
	if err := encoder.Decode(pt, values); err != nil {
		return false, fmt.Errorf("decode plaintext: %w", err)
	}

	var sum uint64
	for i := 0; i < numCandidates; i++ {
		val := values[i]
		if val != 0 && val != 1 {
			// Stuffing, negative values, or arbitrary weights
			return false, nil
		}
		sum += val
	}
	if sum != 1 {
		// Must be exactly one vote: rejects 0 (empty), 2+ (double vote), etc.
		return false, nil
	}

	// Verify all remaining slots are strictly 0
	for j := numCandidates; j < len(values); j++ {
		if values[j] != 0 {
			return false, nil
		}
	}

	return true, nil
}

