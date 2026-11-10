package main

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestEncryptDecryptPrivateKeyRoundtrip(t *testing.T) {
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatalf("generate master key: %v", err)
	}

	rawPrivKey := make([]byte, 2400) // typical ML-KEM-768 private key size
	if _, err := rand.Read(rawPrivKey); err != nil {
		t.Fatalf("generate raw private key: %v", err)
	}

	encrypted, err := encryptPrivateKey(rawPrivKey, masterKey)
	if err != nil {
		t.Fatalf("encryptPrivateKey failed: %v", err)
	}

	// Verify prefix
	if len(encrypted) <= len(keyEncPrefix) || string(encrypted[:len(keyEncPrefix)]) != keyEncPrefix {
		t.Fatalf("encrypted key does not have expected prefix %q", keyEncPrefix)
	}

	// Decrypt
	decrypted, err := decryptPrivateKey(encrypted, masterKey)
	if err != nil {
		t.Fatalf("decryptPrivateKey failed: %v", err)
	}

	if !bytes.Equal(rawPrivKey, decrypted) {
		t.Fatal("decrypted key does not match original plaintext")
	}
}

func TestLegacyPlaintextCompatibility(t *testing.T) {
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatalf("generate master key: %v", err)
	}

	legacyPlaintext := []byte("this-is-a-legacy-unencrypted-private-key-bytes")

	decrypted, err := decryptPrivateKey(legacyPlaintext, masterKey)
	if err != nil {
		t.Fatalf("expected legacy key to decrypt without error, got: %v", err)
	}

	if !bytes.Equal(legacyPlaintext, decrypted) {
		t.Fatal("legacy key was altered during decryption")
	}
}

func TestTamperedEncryptedKeyFails(t *testing.T) {
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatalf("generate master key: %v", err)
	}

	rawPrivKey := []byte("sensitive-server-private-key-payload")
	encrypted, err := encryptPrivateKey(rawPrivKey, masterKey)
	if err != nil {
		t.Fatalf("encryptPrivateKey failed: %v", err)
	}

	// Tamper a byte in the ciphertext payload
	tampered := append([]byte(nil), encrypted...)
	tampered[len(tampered)-1] ^= 0x01

	if _, err := decryptPrivateKey(tampered, masterKey); err == nil {
		t.Fatal("expected tampered ciphertext to fail authentication")
	}
}

func TestWrongMasterKeyFails(t *testing.T) {
	masterKey1 := make([]byte, 32)
	masterKey2 := make([]byte, 32)
	masterKey1[0] = 0xAA
	masterKey2[0] = 0xBB

	rawPrivKey := []byte("sensitive-server-private-key-payload")
	encrypted, err := encryptPrivateKey(rawPrivKey, masterKey1)
	if err != nil {
		t.Fatalf("encryptPrivateKey failed: %v", err)
	}

	if _, err := decryptPrivateKey(encrypted, masterKey2); err == nil {
		t.Fatal("expected decryption with incorrect master key to fail")
	}
}

func TestInvalidInputs(t *testing.T) {
	validKey := make([]byte, 32)
	shortKey := make([]byte, 16)

	// Empty plaintext
	if _, err := encryptPrivateKey(nil, validKey); err == nil {
		t.Fatal("expected error on nil plaintext")
	}

	// Short master key
	if _, err := encryptPrivateKey([]byte("test"), shortKey); err == nil {
		t.Fatal("expected error on 16-byte master key")
	}

	// Empty data to decrypt
	if _, err := decryptPrivateKey(nil, validKey); err == nil {
		t.Fatal("expected error on empty data to decrypt")
	}
}
