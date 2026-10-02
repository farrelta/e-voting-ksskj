package cryptoutil

import (
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
