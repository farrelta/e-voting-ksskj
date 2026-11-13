package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestApp builds an App with just enough wiring to exercise routing and the
// OAuth handlers without a database.
func newTestApp(t *testing.T, cfg *Config) *App {
	t.Helper()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	cfg.OAuthEnabled = cfg.oauthEnabled()
	return &App{
		cfg:      cfg,
		rdb:      rdb,
		sessions: newSessionStore(rdb, false),
		otp:      newOTPService(rdb, "test-secret-at-least-16-chars-long"),
		oauth:    newOAuthService(cfg, rdb),
	}
}

// oauthRoutes mirrors the conditional registration in main.routes().
func oauthRoutes(a *App) *http.ServeMux {
	mux := http.NewServeMux()
	if a.cfg.OAuthEnabled {
		mux.HandleFunc("GET /api/auth/google", a.handleOAuthStart)
		mux.HandleFunc("GET /api/auth/google/callback", a.handleOAuthCallback)
	}
	return mux
}

func TestOAuthRoutesAbsentWhenNotConfigured(t *testing.T) {
	app := newTestApp(t, &Config{AllowedEmailDomain: "ugm.ac.id"})
	mux := oauthRoutes(app)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/google", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when OAuth is not configured", rec.Code)
	}
}

func TestOAuthStartRedirectsToGoogle(t *testing.T) {
	app := newTestApp(t, &Config{
		GoogleClientID:     testClientID,
		GoogleClientSecret: "secret",
		GoogleRedirectURL:  "http://localhost:3000/api/auth/google/callback",
		AllowedEmailDomain: testHostedDn,
	})
	mux := oauthRoutes(app)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/google", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, googleAuthEndpoint+"?") {
		t.Fatalf("Location = %q, want the Google consent endpoint", location)
	}
	for _, want := range []string{"response_type=code", "state=", "nonce=", "hd=ugm.ac.id"} {
		if !strings.Contains(location, want) {
			t.Errorf("Location is missing %q: %s", want, location)
		}
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestOAuthCallbackRejectsMissingCode(t *testing.T) {
	app := newTestApp(t, &Config{
		GoogleClientID:     testClientID,
		GoogleClientSecret: "secret",
		GoogleRedirectURL:  "http://localhost:3000/api/auth/google/callback",
		AllowedEmailDomain: testHostedDn,
	})
	mux := oauthRoutes(app)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/google/callback", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 back to the login page", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "error="+oauthErrExchange) {
		t.Fatalf("Location = %q, want an oauth_exchange error", loc)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestOAuthCallbackRejectsForgedState(t *testing.T) {
	app := newTestApp(t, &Config{
		GoogleClientID:     testClientID,
		GoogleClientSecret: "secret",
		GoogleRedirectURL:  "http://localhost:3000/api/auth/google/callback",
		AllowedEmailDomain: testHostedDn,
	})
	mux := oauthRoutes(app)

	// A code is present but the state was never issued, so the flow must stop
	// before any token exchange happens.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/google/callback?code=attacker-code&state=forged-state", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "error="+oauthErrState) {
		t.Fatalf("Location = %q, want an oauth_state error", loc)
	}
}

func TestOAuthCallbackRejectsProviderError(t *testing.T) {
	app := newTestApp(t, &Config{
		GoogleClientID:     testClientID,
		GoogleClientSecret: "secret",
		GoogleRedirectURL:  "http://localhost:3000/api/auth/google/callback",
		AllowedEmailDomain: testHostedDn,
	})
	mux := oauthRoutes(app)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/google/callback?error=access_denied&state=whatever", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "error="+oauthErrExchange) {
		t.Fatalf("Location = %q, want an oauth_exchange error on denied consent", loc)
	}
}

func TestOAuthCallbackRejectsReplayedState(t *testing.T) {
	cfg := &Config{
		GoogleClientID:     testClientID,
		GoogleClientSecret: "secret",
		GoogleRedirectURL:  "http://localhost:3000/api/auth/google/callback",
		AllowedEmailDomain: testHostedDn,
	}
	app := newTestApp(t, cfg)
	mux := oauthRoutes(app)

	// A recorded first attempt must fail at the state check only, so the test
	// stays offline: the state is consumed before any token exchange happens.
	state, _, err := app.oauth.StartState(t.Context())
	if err != nil {
		t.Fatalf("StartState: %v", err)
	}

	if _, err := app.oauth.ConsumeState(t.Context(), state); err != nil {
		t.Fatalf("first ConsumeState: %v", err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/auth/google/callback?code=replay&state="+state, nil))

	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "error="+oauthErrState) {
		t.Fatalf("replayed state Location = %q, want an oauth_state error", loc)
	}
}

func TestOAuthStartIssuesAFreshStateEachTime(t *testing.T) {
	app := newTestApp(t, &Config{
		GoogleClientID:     testClientID,
		GoogleClientSecret: "secret",
		GoogleRedirectURL:  "http://localhost:3000/api/auth/google/callback",
		AllowedEmailDomain: testHostedDn,
	})
	mux := oauthRoutes(app)

	states := make(map[string]bool)
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/google", nil))
		loc := rec.Header().Get("Location")
		idx := strings.Index(loc, "state=")
		if idx < 0 {
			t.Fatal("no state in the redirect")
		}
		value := loc[idx+len("state="):]
		if end := strings.Index(value, "&"); end >= 0 {
			value = value[:end]
		}
		if states[value] {
			t.Fatalf("state %q was reused across requests", value)
		}
		states[value] = true
	}
}
