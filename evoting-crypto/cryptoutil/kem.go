package cryptoutil

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"

	"github.com/cloudflare/circl/kem/schemes"
)

// kemScheme is resolved once at package init. "ML-KEM-768" is the NIST FIPS 203
// standardized name used by circl's scheme registry.
var kemScheme = schemes.ByName("ML-KEM-768")

// GenerateKEMKeyPair creates a new ML-KEM-768 key pair for establishing
// an encrypted session channel with a voter's client after login.
func GenerateKEMKeyPair() (pubKey, privKey []byte, err error) {
	if kemScheme == nil {
		return nil, nil, fmt.Errorf("ML-KEM-768 scheme not available in this circl build")
	}
	pk, sk, err := kemScheme.GenerateKeyPair()
	if err != nil {
		return nil, nil, fmt.Errorf("kem keygen failed: %w", err)
	}
	pkBytes, err := pk.MarshalBinary()
	if err != nil {
		return nil, nil, err
	}
	skBytes, err := sk.MarshalBinary()
	if err != nil {
		return nil, nil, err
	}
	return pkBytes, skBytes, nil
}

// Encapsulate is called when a client wants to establish a shared secret with
// the server's public key (client-side operation, included here for completeness
// of the reference implementation / testing).
func Encapsulate(pubKeyBytes []byte) (ciphertext, sharedSecret []byte, err error) {
	pk, err := kemScheme.UnmarshalBinaryPublicKey(pubKeyBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid kem public key: %w", err)
	}
	seed := make([]byte, kemScheme.EncapsulationSeedSize())
	if _, err := rand.Read(seed); err != nil {
		return nil, nil, err
	}
	ct, ss, err := kemScheme.EncapsulateDeterministically(pk, seed)
	if err != nil {
		return nil, nil, fmt.Errorf("encapsulation failed: %w", err)
	}
	return ct, ss, nil
}

// Decapsulate recovers the shared secret on the server side using its private key.
func Decapsulate(privKeyBytes, ciphertext []byte) (sharedSecret []byte, err error) {
	sk, err := kemScheme.UnmarshalBinaryPrivateKey(privKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid kem private key: %w", err)
	}
	ss, err := kemScheme.Decapsulate(sk, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decapsulation failed: %w", err)
	}
	return ss, nil
}

// ExpectedEnvelopedCiphertextSize is the exact length of a ballot sealed inside an ML-KEM-768 + AES-256-GCM envelope.
// Format: [ML-KEM-768 ciphertext (1088 bytes)] || [GCM nonce (12 bytes)] || [AES-GCM ciphertext + 16 byte tag]
const ExpectedEnvelopedCiphertextSize = 1088 + 12 + ExpectedCiphertextSize + 16

// SealEnvelope encapsulates a post-quantum shared secret using kemPubKey,
// and encrypts the payload using AES-256-GCM.
func SealEnvelope(kemPubKey, payload []byte) ([]byte, error) {
	kemCT, sharedSecret, err := Encapsulate(kemPubKey)
	if err != nil {
		return nil, fmt.Errorf("kem encapsulate: %w", err)
	}

	block, err := aes.NewCipher(sharedSecret)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("aes gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("nonce read: %w", err)
	}

	encrypted := gcm.Seal(nil, nonce, payload, nil)

	envelope := make([]byte, 0, len(kemCT)+len(nonce)+len(encrypted))
	envelope = append(envelope, kemCT...)
	envelope = append(envelope, nonce...)
	envelope = append(envelope, encrypted...)
	return envelope, nil
}

// OpenEnvelope decapsulates the shared secret using kemPrivKey, and decrypts the AES-256-GCM envelope.
func OpenEnvelope(kemPrivKey, envelope []byte) ([]byte, error) {
	kemCTSize := kemScheme.CiphertextSize() // 1088 bytes
	nonceSize := 12
	minSize := kemCTSize + nonceSize + 16
	if len(envelope) < minSize {
		return nil, fmt.Errorf("envelope too short: %d < %d", len(envelope), minSize)
	}

	kemCT := envelope[:kemCTSize]
	nonce := envelope[kemCTSize : kemCTSize+nonceSize]
	ciphertext := envelope[kemCTSize+nonceSize:]

	sharedSecret, err := Decapsulate(kemPrivKey, kemCT)
	if err != nil {
		return nil, fmt.Errorf("kem decapsulate: %w", err)
	}

	block, err := aes.NewCipher(sharedSecret)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("aes gcm: %w", err)
	}

	payload, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("aes gcm decrypt: %w", err)
	}
	return payload, nil
}

// UnwrapBallot opens the ML-KEM envelope if enveloped, or accepts raw HE ciphertext if already unenveloped.
func UnwrapBallot(kemPrivKey, data []byte) ([]byte, error) {
	if len(data) == ExpectedEnvelopedCiphertextSize {
		return OpenEnvelope(kemPrivKey, data)
	}
	if len(data) == ExpectedCiphertextSize {
		return data, nil
	}
	return nil, fmt.Errorf("invalid ballot length: got %d (expected %d for enveloped or %d for raw)",
		len(data), ExpectedEnvelopedCiphertextSize, ExpectedCiphertextSize)
}

