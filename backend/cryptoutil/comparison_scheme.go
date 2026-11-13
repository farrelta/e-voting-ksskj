package cryptoutil

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/tuneinsight/lattigo/v6/core/rlwe"
	"github.com/tuneinsight/lattigo/v6/schemes/bgv"
)

type VoteCryptoScheme interface {
	Name() string
	GenerateKeys() (pubKey, secKey []byte, err error)
	EncryptVote(pubKey []byte, candidateID, numCandidates int) ([]byte, error)
	ValidateBallot(ciphertext []byte) error
	TallyVotes(secKey []byte, ciphertexts [][]byte, numCandidates int) ([]uint64, error)
}

func DefaultComparisonSchemes() []VoteCryptoScheme {
	return []VoteCryptoScheme{
		BFVFullScheme{},
		BFVPureAddScheme{},
		AESGCMScheme{},
	}
}

type BFVFullScheme struct{}

func (BFVFullScheme) Name() string { return "HE-BFV-Full" }

func (BFVFullScheme) GenerateKeys() ([]byte, []byte, error) {
	return GenerateHEKeyPair()
}

func (BFVFullScheme) EncryptVote(pubKey []byte, candidateID, numCandidates int) ([]byte, error) {
	return EncryptVote(pubKey, candidateID, numCandidates)
}

func (BFVFullScheme) ValidateBallot(ciphertext []byte) error {
	return ValidateCiphertext(ciphertext)
}

func (BFVFullScheme) TallyVotes(secKey []byte, ciphertexts [][]byte, numCandidates int) ([]uint64, error) {
	return AggregateAndDecrypt(secKey, ciphertexts, numCandidates)
}

type BFVPureAddScheme struct{}

func (BFVPureAddScheme) Name() string { return "HE-BFV-PureAdd" }

func (BFVPureAddScheme) GenerateKeys() ([]byte, []byte, error) {
	return GenerateHEKeyPair()
}

func (BFVPureAddScheme) EncryptVote(pubKey []byte, candidateID, numCandidates int) ([]byte, error) {
	return EncryptVote(pubKey, candidateID, numCandidates)
}

func (BFVPureAddScheme) ValidateBallot(ciphertext []byte) error {
	return ValidateCiphertext(ciphertext)
}

func (BFVPureAddScheme) TallyVotes(secKey []byte, ciphertexts [][]byte, numCandidates int) ([]uint64, error) {
	if len(ciphertexts) == 0 {
		return make([]uint64, numCandidates), nil
	}
	params, err := newHEParams()
	if err != nil {
		return nil, err
	}
	sk := rlwe.NewSecretKey(params.Parameters)
	if err := sk.UnmarshalBinary(secKey); err != nil {
		return nil, fmt.Errorf("invalid he secret key: %w", err)
	}

	evaluator := bgv.NewEvaluator(params, nil, true)
	var sum *rlwe.Ciphertext

	for i, ctBytes := range ciphertexts {
		if err := ValidateCiphertext(ctBytes); err != nil {
			return nil, fmt.Errorf("invalid ciphertext at index %d: %w", i, err)
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
			return nil, fmt.Errorf("homomorphic add at index %d: %w", i, err)
		}
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

const (
	aesKeySize        = 32
	aesNonceSize      = 12
	aesPlaintextSize  = 4
	aesGCMTagSize     = 16
	aesBallotByteSize = aesNonceSize + aesPlaintextSize + aesGCMTagSize
)

type AESGCMScheme struct{}

func (AESGCMScheme) Name() string { return "NoHE-AES256GCM" }

func (AESGCMScheme) GenerateKeys() ([]byte, []byte, error) {
	key := make([]byte, aesKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, nil, err
	}
	sec := make([]byte, aesKeySize)
	copy(sec, key)
	return key, sec, nil
}

func (AESGCMScheme) EncryptVote(pubKey []byte, candidateID, numCandidates int) ([]byte, error) {
	if candidateID < 0 || candidateID >= numCandidates {
		return nil, fmt.Errorf("candidate_id %d out of range [0,%d)", candidateID, numCandidates)
	}
	if len(pubKey) != aesKeySize {
		return nil, errors.New("invalid aes key length")
	}
	block, err := aes.NewCipher(pubKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, aesNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	plaintext := make([]byte, aesPlaintextSize)
	binary.LittleEndian.PutUint32(plaintext, uint32(candidateID))

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (AESGCMScheme) ValidateBallot(ciphertext []byte) error {
	if len(ciphertext) != aesBallotByteSize {
		return fmt.Errorf("invalid ballot size: expected %d bytes, got %d", aesBallotByteSize, len(ciphertext))
	}
	return nil
}

func (AESGCMScheme) TallyVotes(secKey []byte, ciphertexts [][]byte, numCandidates int) ([]uint64, error) {
	counts := make([]uint64, numCandidates)
	if len(ciphertexts) == 0 {
		return counts, nil
	}
	if len(secKey) != aesKeySize {
		return nil, errors.New("invalid aes key length")
	}
	block, err := aes.NewCipher(secKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	for i, ct := range ciphertexts {
		if len(ct) != aesBallotByteSize {
			return nil, fmt.Errorf("invalid ballot at index %d", i)
		}
		pt, err := gcm.Open(nil, ct[:aesNonceSize], ct[aesNonceSize:], nil)
		if err != nil {
			return nil, fmt.Errorf("decrypt ballot at index %d: %w", i, err)
		}
		candidateID := int(binary.LittleEndian.Uint32(pt))
		if candidateID < 0 || candidateID >= numCandidates {
			return nil, fmt.Errorf("candidate_id %d out of range at index %d", candidateID, i)
		}
		counts[candidateID]++
	}
	return counts, nil
}
