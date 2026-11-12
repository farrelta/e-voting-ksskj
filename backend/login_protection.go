package main

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/bcrypt"
)

// Checking a password against a missing or unusable account must cost as much
// as checking it against a real one. Otherwise the response time tells an
// attacker which email addresses have accounts.
//
// Two cases need the extra work: an email that is not registered, and an
// account whose stored password is not a bcrypt hash (voters added in bulk
// have the placeholder lockedPasswordHash and sign in with a QR code).

// defaultPasswordCost matches the cost used when accounts are created in the
// README. It is replaced by the cost of the first real hash we see.
const defaultPasswordCost = 12

var (
	passwordCost atomic.Int32
	dummyHashes  sync.Map // bcrypt cost -> hash that no password matches
)

func init() { passwordCost.Store(defaultPasswordCost) }

func dummyHashFor(cost int) []byte {
	if cached, ok := dummyHashes.Load(cost); ok {
		return cached.([]byte)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("only-used-to-equalise-login-timing"), cost)
	if err != nil {
		return nil
	}
	dummyHashes.Store(cost, hash)
	return hash
}

// warmPasswordDummy builds the dummy hash at startup so the first sign-in for an
// unknown email is not slower than later ones.
func warmPasswordDummy() { dummyHashFor(int(passwordCost.Load())) }

func burnPasswordCheck(password string) {
	if hash := dummyHashFor(int(passwordCost.Load())); hash != nil {
		_ = bcrypt.CompareHashAndPassword(hash, []byte(password))
	}
}

// checkPassword reports whether password matches storedHash, taking about the
// same time whatever the stored value is.
func checkPassword(storedHash, password string) bool {
	cost, err := bcrypt.Cost([]byte(storedHash))
	if err != nil {
		burnPasswordCheck(password)
		return false
	}
	passwordCost.Store(int32(cost))
	return bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password)) == nil
}

// accountRateKey identifies an account for rate limiting without storing the
// email address in Redis. It is the same for registered and unregistered
// emails, so limits behave identically for both.
func accountRateKey(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:])
}
