package cryptoutil

import (
	"fmt"

	"github.com/cloudflare/circl/sign/schemes"
)

// dsaScheme resolves "ML-DSA-65", the NIST FIPS 204 standardized name in circl.
var dsaScheme = schemes.ByName("ML-DSA-65")

// GenerateDSAKeyPair creates a signing key pair. In this design, the server
// (or a dedicated election authority) holds the private key and signs each
// encrypted ballot to prove it was processed by a legitimate, untampered
// instance of the voting system; the public key is published so anyone can
// verify ballots on the bulletin board.
func GenerateDSAKeyPair() (pubKey, privKey []byte, err error) {
	if dsaScheme == nil {
		return nil, nil, fmt.Errorf("ML-DSA-65 scheme not available in this circl build")
	}
	pk, sk, err := dsaScheme.GenerateKey()
	if err != nil {
		return nil, nil, fmt.Errorf("dsa keygen failed: %w", err)
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

// SignCiphertext signs the BFV ciphertext bytes, not the plaintext vote,
// so the signature proves integrity/authenticity without revealing the choice.
func SignCiphertext(privKeyBytes, ciphertext []byte) (signature []byte, err error) {
	sk, err := dsaScheme.UnmarshalBinaryPrivateKey(privKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid dsa private key: %w", err)
	}
	sig := dsaScheme.Sign(sk, ciphertext, nil)
	return sig, nil
}

// VerifySignature checks a ballot's signature against its ciphertext and the
// known public key. Call this on ingest, before a ballot is accepted into storage.
func VerifySignature(pubKeyBytes, ciphertext, signature []byte) (bool, error) {
	pk, err := dsaScheme.UnmarshalBinaryPublicKey(pubKeyBytes)
	if err != nil {
		return false, fmt.Errorf("invalid dsa public key: %w", err)
	}
	return dsaScheme.Verify(pk, ciphertext, signature, nil), nil
}
