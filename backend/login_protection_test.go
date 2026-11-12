package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

func loginRequest(a *App, email, password string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(map[string]string{"email": email, "password": password})
	r := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	a.handleLogin(rec, r)
	return rec
}

func insertTestUser(t *testing.T, a *App, role, hash string) string {
	t.Helper()
	email := "it-login-" + uuid.NewString()[:8] + "@ugm.ac.id"
	if _, err := a.db.Exec(context.Background(), `
		INSERT INTO evoting.users (email, password_hash, role, is_verified)
		VALUES ($1, $2, $3::evoting.roles, TRUE)`, email, hash, role); err != nil {
		t.Fatal(err)
	}
	return email
}

func TestCheckPasswordHandlesUnusableHashes(t *testing.T) {
	passwordCost.Store(4)
	t.Cleanup(func() { passwordCost.Store(defaultPasswordCost) })
	good, err := bcrypt.GenerateFromPassword([]byte("correct horse"), 4)
	if err != nil {
		t.Fatal(err)
	}
	if !checkPassword(string(good), "correct horse") {
		t.Error("the right password should match")
	}
	if checkPassword(string(good), "wrong") {
		t.Error("a wrong password matched")
	}
	for _, unusable := range []string{lockedPasswordHash, "", "not-a-hash"} {
		if checkPassword(unusable, "anything") || checkPassword(unusable, unusable) {
			t.Errorf("hash %q must never match", unusable)
		}
	}
	if passwordCost.Load() != 4 {
		t.Errorf("the dummy cost should follow the real hashes, got %d", passwordCost.Load())
	}
}

func TestLoginLocksAccountAfterRepeatedFailures(t *testing.T) {
	a := testApp(t)
	a.limiter = newLimitedApp(t, 0).limiter
	testAdmin(t, a) // registers cleanup of it-* users
	passwordCost.Store(4)
	t.Cleanup(func() { passwordCost.Store(defaultPasswordCost) })

	hash, _ := bcrypt.GenerateFromPassword([]byte("right-password"), 4)
	email := insertTestUser(t, a, "voter", string(hash))

	for i := 0; i < int(ruleLoginFailures.limit); i++ {
		if rec := loginRequest(a, email, "wrong"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong password #%d: %d, want 401", i+1, rec.Code)
		}
	}
	// Locked: even the right password is refused until the window passes.
	rec := loginRequest(a, email, "right-password")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("locked account: %d / Retry-After %q, want 429 with a Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}

	// An email with no account behaves exactly the same, so the lock reveals nothing.
	ghost := "it-nobody-" + uuid.NewString()[:8] + "@ugm.ac.id"
	for i := 0; i < int(ruleLoginFailures.limit); i++ {
		if rec := loginRequest(a, ghost, "wrong"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("unknown email attempt #%d: %d, want 401", i+1, rec.Code)
		}
	}
	if rec := loginRequest(a, ghost, "wrong"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("unknown email after %d attempts: %d, want 429", ruleLoginFailures.limit, rec.Code)
	}
	// The same address in another case shares the counter.
	if rec := loginRequest(a, "  "+upper(email), "right-password"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("case variation escaped the lock: %d", rec.Code)
	}
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

// An unknown email, a QR-only account (placeholder hash) and a real account with
// the wrong password must all take about as long, or the response time shows
// which addresses are registered.
func TestLoginTimingDoesNotRevealAccounts(t *testing.T) {
	a := testApp(t) // no limiter: only the timing is under test
	testAdmin(t, a)
	t.Cleanup(func() { passwordCost.Store(defaultPasswordCost) })

	const cost = 10
	hash, err := bcrypt.GenerateFromPassword([]byte("right-password"), cost)
	if err != nil {
		t.Fatal(err)
	}
	real := insertTestUser(t, a, "voter", string(hash))
	qrOnly := insertTestUser(t, a, "voter", lockedPasswordHash)
	ghost := "it-nobody-" + uuid.NewString()[:8] + "@ugm.ac.id"

	loginRequest(a, real, "wrong") // lets the dummy hash adopt the real cost
	median := func(email string) time.Duration {
		var runs []time.Duration
		for i := 0; i < 5; i++ {
			start := time.Now()
			if rec := loginRequest(a, email, "wrong"); rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", rec.Code)
			}
			runs = append(runs, time.Since(start))
		}
		sort.Slice(runs, func(i, j int) bool { return runs[i] < runs[j] })
		return runs[len(runs)/2]
	}
	known := median(real)
	for name, email := range map[string]string{"unknown email": ghost, "QR-only account": qrOnly} {
		got := median(email)
		if got < known/2 {
			t.Errorf("%s answered in %v but a real account takes %v: the difference reveals which emails exist", name, got, known)
		}
	}
}
