package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

const keyEncPrefix = "ENC\x01"

// encryptPrivateKey encrypts a private key using AES-256-GCM with the provided 32-byte master key.
// Output format: "ENC\x01" (4 bytes) || 12-byte nonce || AES-GCM ciphertext + 16-byte tag.
func encryptPrivateKey(plaintext, masterKey []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, errors.New("empty plaintext key")
	}
	if len(masterKey) != 32 {
		return nil, errors.New("master key must be 32 bytes for AES-256")
	}

	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("aes gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	result := make([]byte, 0, len(keyEncPrefix)+len(nonce)+len(ciphertext))
	result = append(result, keyEncPrefix...)
	result = append(result, nonce...)
	result = append(result, ciphertext...)
	return result, nil
}

// decryptPrivateKey decrypts a private key encrypted with encryptPrivateKey.
// If the data does not start with the "ENC\x01" prefix, it is treated as a legacy
// unencrypted key for backward compatibility with existing databases.
func decryptPrivateKey(data, masterKey []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty key data")
	}

	// Backward compatibility: If data doesn't have the "ENC\x01" prefix or is too short,
	// treat it as legacy plaintext.
	if len(data) < len(keyEncPrefix)+12+16 || string(data[:len(keyEncPrefix)]) != keyEncPrefix {
		legacy := make([]byte, len(data))
		copy(legacy, data)
		return legacy, nil
	}

	if len(masterKey) != 32 {
		return nil, errors.New("master key must be 32 bytes for AES-256")
	}

	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("aes gcm: %w", err)
	}

	payload := data[len(keyEncPrefix):]
	nonceSize := gcm.NonceSize()
	if len(payload) < nonceSize+gcm.Overhead() {
		return nil, errors.New("encrypted key payload too short")
	}

	nonce := payload[:nonceSize]
	ciphertext := payload[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt private key (invalid master key or corrupted data): %w", err)
	}

	return plaintext, nil
}
