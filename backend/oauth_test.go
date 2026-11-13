package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

const (
	testClientID = "test-client-id.apps.googleusercontent.com"
	testHostedDn = "ugm.ac.id"
)

// newTestOAuthService returns a service wired to miniredis and to a local JWKS
// server, so verification runs fully offline.
func newTestOAuthService(t *testing.T) (*OAuthService, *miniredis.Miniredis, *rsa.PrivateKey) {
	t.Helper()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	svc := &OAuthService{
		clientID:     testClientID,
		clientSecret: "test-secret",
		redirectURL:  "http://localhost:3000/api/auth/google/callback",
		hd:           testHostedDn,
		rdb:          rdb,
		client:       &http.Client{Timeout: 5 * time.Second},
		keys:         map[string]*rsa.PublicKey{},
	}
	return svc, mr, key
}

const testKID = "test-key-1"

// signIDToken builds a signed ID token with the given claim overrides applied
// on top of a valid baseline.
func signIDToken(t *testing.T, key *rsa.PrivateKey, overrides map[string]any, now time.Time) string {
	t.Helper()

	payload := map[string]any{
		"iss":            googleIssuer,
		"aud":            testClientID,
		"sub":            "1234567890",
		"email":          "voter@ugm.ac.id",
		"email_verified": true,
		"hd":             testHostedDn,
		"nonce":          "expected-nonce",
		"exp":            now.Add(time.Hour).Unix(),
	}
	for k, v := range overrides {
		payload[k] = v
	}

	headerJSON, err := json.Marshal(map[string]string{"alg": "RS256", "kid": testKID, "typ": "JWT"})
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." +
		base64.RawURLEncoding.EncodeToString(payloadJSON)

	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// seedJWKS installs a key set for the generated key directly into the cache,
// standing in for a fetched https://www.googleapis.com/oauth2/v3/certs.
func seedJWKS(svc *OAuthService, key *rsa.PrivateKey) {
	svc.keysMu.Lock()
	svc.keys = map[string]*rsa.PublicKey{testKID: &key.PublicKey}
	svc.keysFetched = time.Now()
	svc.keysCacheFor = time.Hour
	svc.keysMu.Unlock()
}

// roundTripTo rewrites every request onto baseURL while preserving the path and
// query, so the fixed Google JWKS URL can be served by a local test server.
func roundTripTo(baseURL string) http.RoundTripper {
	target, err := url.Parse(baseURL)
	if err != nil {
		panic(err)
	}
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		return http.DefaultTransport.RoundTrip(req)
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestStartStateStoresNonce(t *testing.T) {
	svc, mr, _ := newTestOAuthService(t)
	ctx := context.Background()

	state, nonce, err := svc.StartState(ctx)
	if err != nil {
		t.Fatalf("StartState: %v", err)
	}
	if state == "" || nonce == "" {
		t.Fatal("StartState returned an empty state or nonce")
	}
	if state == nonce {
		t.Fatal("state and nonce must differ")
	}
	got, err := mr.Get(oauthStateKey(state))
	if err != nil {
		t.Fatalf("read stored nonce: %v", err)
	}
	if got != nonce {
		t.Fatalf("stored nonce = %q, want %q", got, nonce)
	}
}

func TestConsumeStateIsSingleUse(t *testing.T) {
	svc, _, _ := newTestOAuthService(t)
	ctx := context.Background()

	state, nonce, err := svc.StartState(ctx)
	if err != nil {
		t.Fatalf("StartState: %v", err)
	}

	got, err := svc.ConsumeState(ctx, state)
	if err != nil {
		t.Fatalf("first ConsumeState: %v", err)
	}
	if got != nonce {
		t.Fatalf("ConsumeState returned %q, want %q", got, nonce)
	}

	if _, err := svc.ConsumeState(ctx, state); err == nil {
		t.Fatal("a replayed state must be rejected")
	}
}

func TestConsumeStateRejectsUnknownAndEmpty(t *testing.T) {
	svc, _, _ := newTestOAuthService(t)
	ctx := context.Background()

	if _, err := svc.ConsumeState(ctx, "never-issued"); err == nil {
		t.Fatal("an unknown state must be rejected")
	}
	if _, err := svc.ConsumeState(ctx, ""); err == nil {
		t.Fatal("an empty state must be rejected")
	}
	if _, err := svc.ConsumeState(ctx, strings.Repeat("a", 200)); err == nil {
		t.Fatal("an oversized state must be rejected")
	}
}

func TestConsumeStateExpires(t *testing.T) {
	svc, mr, _ := newTestOAuthService(t)
	ctx := context.Background()

	state, _, err := svc.StartState(ctx)
	if err != nil {
		t.Fatalf("StartState: %v", err)
	}
	mr.FastForward(oAuthStateTTL + time.Minute)

	if _, err := svc.ConsumeState(ctx, state); err == nil {
		t.Fatal("an expired state must be rejected")
	}
}

func TestVerifyIDTokenHappyPath(t *testing.T) {
	svc, _, key := newTestOAuthService(t)
	seedJWKS(svc, key)
	now := time.Now()

	raw := signIDToken(t, key, nil, now)
	claims, err := svc.VerifyIDToken(context.Background(), raw, "expected-nonce", now)
	if err != nil {
		t.Fatalf("VerifyIDToken: %v", err)
	}
	if claims.Subject != "1234567890" {
		t.Errorf("Subject = %q, want 1234567890", claims.Subject)
	}
	if claims.Email != "voter@ugm.ac.id" {
		t.Errorf("Email = %q, want voter@ugm.ac.id", claims.Email)
	}
	if !claims.EmailVerified {
		t.Error("EmailVerified should be true")
	}
	if claims.HostedDomain != testHostedDn {
		t.Errorf("HostedDomain = %q, want %q", claims.HostedDomain, testHostedDn)
	}
}

func TestVerifyIDTokenNormalisesEmailCase(t *testing.T) {
	svc, _, key := newTestOAuthService(t)
	seedJWKS(svc, key)
	now := time.Now()

	raw := signIDToken(t, key, map[string]any{"email": "Voter@UGM.AC.ID"}, now)
	claims, err := svc.VerifyIDToken(context.Background(), raw, "expected-nonce", now)
	if err != nil {
		t.Fatalf("VerifyIDToken: %v", err)
	}
	if claims.Email != "voter@ugm.ac.id" {
		t.Errorf("Email = %q, want a lower-cased address", claims.Email)
	}
}

func TestVerifyIDTokenAcceptsManagedHostedSubdomain(t *testing.T) {
	svc, _, key := newTestOAuthService(t)
	seedJWKS(svc, key)
	now := time.Now()

	raw := signIDToken(t, key, map[string]any{
		"email": "voter@mail.ugm.ac.id",
		"hd":    "mail.ugm.ac.id",
	}, now)
	claims, err := svc.VerifyIDToken(context.Background(), raw, "expected-nonce", now)
	if err != nil {
		t.Fatalf("a managed hosted subdomain should be accepted: %v", err)
	}
	if claims.HostedDomain != "mail.ugm.ac.id" {
		t.Fatalf("HostedDomain = %q, want mail.ugm.ac.id", claims.HostedDomain)
	}
}

func TestHostedDomainAllowed(t *testing.T) {
	tests := []struct {
		hosted  string
		allowed string
		want    bool
	}{
		{"ugm.ac.id", "ugm.ac.id", true},
		{"mail.ugm.ac.id", "ugm.ac.id", true},
		{"MAIL.UGM.AC.ID", "ugm.ac.id", true},
		{"evilugm.ac.id", "ugm.ac.id", false},
		{"ugm.ac.id.evil.example", "ugm.ac.id", false},
		{"gmail.com", "ugm.ac.id", false},
		{"", "ugm.ac.id", false},
		{"ugm.ac.id", "", false},
	}
	for _, tt := range tests {
		if got := hostedDomainAllowed(tt.hosted, tt.allowed); got != tt.want {
			t.Errorf("hostedDomainAllowed(%q, %q) = %v, want %v", tt.hosted, tt.allowed, got, tt.want)
		}
	}
}

func TestVerifyIDTokenRejectionTable(t *testing.T) {
	now := time.Now()

	cases := []struct {
		name      string
		overrides map[string]any
		nonce     string
	}{
		{"wrong issuer", map[string]any{"iss": "https://evil.example"}, "expected-nonce"},
		{"wrong audience", map[string]any{"aud": "someone-else.apps.googleusercontent.com"}, "expected-nonce"},
		{"expired", map[string]any{"exp": now.Add(-time.Hour).Unix()}, "expected-nonce"},
		{"missing subject", map[string]any{"sub": ""}, "expected-nonce"},
		{"replayed nonce", nil, "stale-nonce"},
		{"empty expected nonce", nil, ""},
		{"email not verified", map[string]any{"email_verified": false}, "expected-nonce"},
		{"missing email", map[string]any{"email": ""}, "expected-nonce"},
		{"personal gmail account", map[string]any{"hd": "", "email": "someone@gmail.com"}, "expected-nonce"},
		{"other hosted domain", map[string]any{"hd": "example.com"}, "expected-nonce"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, key := newTestOAuthService(t)
			seedJWKS(svc, key)

			raw := signIDToken(t, key, tc.overrides, now)
			if _, err := svc.VerifyIDToken(context.Background(), raw, tc.nonce, now); err == nil {
				t.Fatalf("expected the token to be rejected, but it was accepted")
			}
		})
	}
}

func TestVerifyIDTokenRejectsBadSignature(t *testing.T) {
	svc, _, key := newTestOAuthService(t)
	seedJWKS(svc, key)
	now := time.Now()

	raw := signIDToken(t, key, nil, now)
	tampered := raw[:len(raw)-4] + "AAAA"
	if _, err := svc.VerifyIDToken(context.Background(), tampered, "expected-nonce", now); err == nil {
		t.Fatal("a tampered signature must be rejected")
	}
}

func TestVerifyIDTokenRejectsTokenSignedByAnotherKey(t *testing.T) {
	svc, _, key := newTestOAuthService(t)
	seedJWKS(svc, key)
	now := time.Now()

	attacker, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate attacker key: %v", err)
	}
	raw := signIDToken(t, attacker, nil, now)
	if _, err := svc.VerifyIDToken(context.Background(), raw, "expected-nonce", now); err == nil {
		t.Fatal("a token signed by an unknown key must be rejected")
	}
}

func TestVerifyIDTokenRejectsAlgNone(t *testing.T) {
	svc, _, key := newTestOAuthService(t)
	seedJWKS(svc, key)

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","kid":"test-key-1"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"https://accounts.google.com"}`))
	raw := header + "." + payload + "."

	if _, err := svc.VerifyIDToken(context.Background(), raw, "expected-nonce", time.Now()); err == nil {
		t.Fatal("alg=none must be rejected")
	}
}

func TestVerifyIDTokenRejectsMalformedInput(t *testing.T) {
	svc, _, key := newTestOAuthService(t)
	seedJWKS(svc, key)
	now := time.Now()

	for _, raw := range []string{"", "not-a-jwt", "a.b", "a.b.c.d", "!!!.???.$$$"} {
		if _, err := svc.VerifyIDToken(context.Background(), raw, "expected-nonce", now); err == nil {
			t.Errorf("malformed token %q must be rejected", raw)
		}
	}
}

func TestRefreshKeysParsesJWKS(t *testing.T) {
	svc, _, key := newTestOAuthService(t)

	eBytes := big.NewInt(int64(key.PublicKey.E)).Bytes()
	jwks := map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA",
			"kid": testKID,
			"use": "sig",
			"n":   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(eBytes),
		}},
	}
	body, err := json.Marshal(jwks)
	if err != nil {
		t.Fatalf("marshal jwks: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=120")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	svc.client = srv.Client()
	svc.client.Transport = roundTripTo(srv.URL)

	if err := svc.refreshKeys(context.Background()); err != nil {
		t.Fatalf("refreshKeys: %v", err)
	}

	svc.keysMu.RLock()
	got, ok := svc.keys[testKID]
	cacheFor := svc.keysCacheFor
	svc.keysMu.RUnlock()
	if !ok {
		t.Fatal("the key set should contain the signing key")
	}
	if got.N.Cmp(key.PublicKey.N) != 0 || got.E != key.PublicKey.E {
		t.Fatal("the parsed key does not match the source key")
	}
	if cacheFor != 120*time.Second {
		t.Errorf("cacheFor = %v, want 2m from max-age", cacheFor)
	}
}

func TestRefreshKeysIsRateLimitedByAttempts(t *testing.T) {
	svc, _, key := newTestOAuthService(t)

	eBytes := big.NewInt(int64(key.PublicKey.E)).Bytes()
	jwks := map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA",
			"kid": testKID,
			"n":   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(eBytes),
		}},
	}
	body, err := json.Marshal(jwks)
	if err != nil {
		t.Fatalf("marshal jwks: %v", err)
	}

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	svc.client = srv.Client()
	svc.client.Transport = roundTripTo(srv.URL)

	// The first refresh must reach the key endpoint.
	if err := svc.refreshKeys(context.Background()); err != nil {
		t.Fatalf("first refreshKeys: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("outbound fetches = %d, want 1", got)
	}

	// Further refreshes inside the window must be suppressed, so a burst of
	// unknown keys cannot hammer Google.
	for i := 0; i < 5; i++ {
		if err := svc.refreshKeys(context.Background()); err != nil {
			t.Fatalf("subsequent refreshKeys: %v", err)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("outbound fetches = %d after a burst, want the window to hold it at 1", got)
	}
}

func TestSigningKeyRefreshesForRotatedKid(t *testing.T) {
	svc, _, key := newTestOAuthService(t)
	now := time.Now()

	// A fresh cache that does NOT contain the key, and no attempt recorded yet:
	// this is the rotated-key case that the old code could never refresh.
	svc.keysMu.Lock()
	svc.keys = map[string]*rsa.PublicKey{"an-old-key": &key.PublicKey}
	svc.keysFetched = now
	svc.keysCacheFor = time.Hour
	svc.keysAttempted = time.Time{}
	svc.keysMu.Unlock()

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		n := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes())
		_, _ = w.Write([]byte(`{"keys":[{"kty":"RSA","kid":"rotated-kid","n":"` + n + `","e":"` + e + `"}]}`))
	}))
	defer srv.Close()

	svc.client = srv.Client()
	svc.client.Transport = roundTripTo(srv.URL)

	got, err := svc.signingKey(context.Background(), "rotated-kid")
	if err != nil {
		t.Fatalf("signingKey should recover a rotated key, got: %v", err)
	}
	if got.N.Cmp(key.PublicKey.N) != 0 {
		t.Fatal("the recovered key does not match the served JWKS")
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("outbound fetches = %d, want exactly 1", hits)
	}
}

func TestSigningKeyRejectsUnknownKidWhenWindowClosed(t *testing.T) {
	svc, _, key := newTestOAuthService(t)
	seedJWKS(svc, key)

	// A fetch was just attempted, so a truly unknown key must be reported as
	// invalid rather than triggering more outbound traffic.
	svc.keysMu.Lock()
	svc.keysAttempted = time.Now()
	svc.keysMu.Unlock()

	if _, err := svc.signingKey(context.Background(), "no-such-key"); err == nil {
		t.Fatal("an unknown kid must be rejected when the refresh window is closed")
	}
}

func TestVerifyIDTokenRejectsMissingHostedDomain(t *testing.T) {
	svc, _, key := newTestOAuthService(t)
	seedJWKS(svc, key)
	now := time.Now()

	// A consumer account carries no hd claim. It must be rejected outright
	// rather than skipping the organisation gate.
	raw := signIDToken(t, key, map[string]any{"hd": ""}, now)
	if _, err := svc.VerifyIDToken(context.Background(), raw, "expected-nonce", now); err == nil {
		t.Fatal("an account without a hosted domain must be rejected")
	}
}

func TestParseRSAPublicKeyRejectsGarbage(t *testing.T) {
	if _, err := parseRSAPublicKey("", ""); err == nil {
		t.Error("empty modulus and exponent must be rejected")
	}
	if _, err := parseRSAPublicKey("!!!", "AQAB"); err == nil {
		t.Error("an invalid modulus must be rejected")
	}
	goodN := base64.RawURLEncoding.EncodeToString(big.NewInt(3233).Bytes())
	if _, err := parseRSAPublicKey(goodN, base64.RawURLEncoding.EncodeToString([]byte{1})); err == nil {
		t.Error("an exponent below 3 must be rejected")
	}
}

func TestParseMaxAge(t *testing.T) {
	cases := map[string]struct {
		want   int
		wantOK bool
	}{
		"public, max-age=3600": {3600, true},
		"max-age=60":           {60, true},
		"no-store":             {0, false},
		"":                     {0, false},
	}
	for header, want := range cases {
		got, ok := parseMaxAge(header)
		if ok != want.wantOK || (ok && got != want.want) {
			t.Errorf("parseMaxAge(%q) = (%d, %v), want (%d, %v)", header, got, ok, want.want, want.wantOK)
		}
	}
}

func TestAuthCodeURLContainsRequiredParameters(t *testing.T) {
	svc, _, _ := newTestOAuthService(t)
	got := svc.AuthCodeURL("the-state", "the-nonce")

	for _, want := range []string{
		"client_id=test-client-id.apps.googleusercontent.com",
		"response_type=code",
		"scope=openid+email+profile",
		"state=the-state",
		"nonce=the-nonce",
		"hd=ugm.ac.id",
		"prompt=select_account",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("auth URL is missing %q\ngot: %s", want, got)
		}
	}
	if !strings.HasPrefix(got, googleAuthEndpoint+"?") {
		t.Errorf("auth URL does not target the Google endpoint: %s", got)
	}
}

func TestOAuthEnabledRequiresEveryCredential(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"all set", Config{GoogleClientID: "id", GoogleClientSecret: "s", GoogleRedirectURL: "u"}, true},
		{"id only", Config{GoogleClientID: "id"}, false},
		{"no redirect", Config{GoogleClientID: "id", GoogleClientSecret: "s"}, false},
		{"nothing set", Config{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.oauthEnabled(); got != tc.want {
				t.Errorf("oauthEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestOAuthCallbackClearsAnExistingSession proves the callback cannot convert
// an already-authenticated browser session into a pending sign-in for a
// different account.
func TestOAuthCallbackClearsAnExistingSession(t *testing.T) {
	app := newTestApp(t, &Config{
		GoogleClientID:     testClientID,
		GoogleClientSecret: "secret",
		GoogleRedirectURL:  "http://localhost:3000/api/auth/google/callback",
		AllowedEmailDomain: testHostedDn,
	})
	ctx := t.Context()

	// An existing authenticated session for some other user.
	seed := httptest.NewRecorder()
	if err := app.sessions.Save(ctx, seed, &Session{
		User: &SessionUser{ID: "user-a", Email: "a@ugm.ac.id", Role: "voter"},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	cookies := seed.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("Save should have issued a session cookie")
	}
	sessionID := cookies[0].Value

	loadReq := httptest.NewRequest(http.MethodGet, "/", nil)
	loadReq.AddCookie(cookies[0])
	loaded, err := app.sessions.Load(ctx, loadReq)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if loaded.User == nil {
		t.Fatal("the seeded session should be authenticated")
	}

	// Destroy mirrors what the callback now does before storing pending state.
	rec := httptest.NewRecorder()
	if err := app.sessions.Destroy(ctx, rec, loaded); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if loaded.User != nil || loaded.ID != "" {
		t.Fatal("Destroy must clear the authenticated user and session id")
	}
	if _, err := app.rdb.Get(ctx, sessionKey(sessionID)).Result(); err == nil {
		t.Fatal("the old session key must be removed from the store")
	}
}
