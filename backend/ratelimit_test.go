package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return rdb, mr
}

var testRule = rateRule{name: "test", limit: 3, window: time.Minute}

func TestRateLimiterAllowsUpToLimitThenResets(t *testing.T) {
	rdb, mr := newTestRedis(t)
	l := newRateLimiter(rdb)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		ok, _, err := l.Allow(ctx, testRule, "ip-1")
		if err != nil || !ok {
			t.Fatalf("event %d should be allowed: ok=%v err=%v", i, ok, err)
		}
	}
	ok, wait, err := l.Allow(ctx, testRule, "ip-1")
	if err != nil || ok {
		t.Fatalf("event 4 should be refused: ok=%v err=%v", ok, err)
	}
	if wait <= 0 || wait > time.Minute {
		t.Fatalf("retry-after should be within the window, got %v", wait)
	}

	mr.FastForward(time.Minute + time.Second)
	if ok, _, err := l.Allow(ctx, testRule, "ip-1"); err != nil || !ok {
		t.Fatalf("the window should have reset: ok=%v err=%v", ok, err)
	}
}

func TestRateLimiterKeysAreIndependent(t *testing.T) {
	rdb, _ := newTestRedis(t)
	l := newRateLimiter(rdb)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		l.Allow(ctx, testRule, "ip-1")
	}
	if ok, _, _ := l.Allow(ctx, testRule, "ip-2"); !ok {
		t.Error("a different client should not be affected")
	}
	other := rateRule{name: "other", limit: 3, window: time.Minute}
	if ok, _, _ := l.Allow(ctx, other, "ip-1"); !ok {
		t.Error("a different rule should not be affected")
	}
}

func TestRateLimiterFailureCounter(t *testing.T) {
	rdb, _ := newTestRedis(t)
	l := newRateLimiter(rdb)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if blocked, _, err := l.Blocked(ctx, testRule, "acct"); err != nil || blocked {
			t.Fatalf("before failure %d: blocked=%v err=%v", i, blocked, err)
		}
		if err := l.Fail(ctx, testRule, "acct"); err != nil {
			t.Fatal(err)
		}
	}
	// Checking must not count as a failure.
	for i := 0; i < 5; i++ {
		l.Blocked(ctx, testRule, "acct")
	}
	blocked, wait, err := l.Blocked(ctx, testRule, "acct")
	if err != nil || !blocked || wait <= 0 {
		t.Fatalf("after %d failures: blocked=%v wait=%v err=%v", testRule.limit, blocked, wait, err)
	}
	if err := l.Reset(ctx, testRule, "acct"); err != nil {
		t.Fatal(err)
	}
	if blocked, _, _ := l.Blocked(ctx, testRule, "acct"); blocked {
		t.Error("reset should clear the failures")
	}
}

func TestRateLimiterCounterAlwaysGetsAnExpiry(t *testing.T) {
	rdb, mr := newTestRedis(t)
	l := newRateLimiter(rdb)
	ctx := context.Background()
	// A counter left without a TTL (for example after a crash) must not block forever.
	mr.Set(rateKey(testRule, "stuck"), "10")
	if ok, _, _ := l.Allow(ctx, testRule, "stuck"); ok {
		t.Fatal("a stuck counter above the limit should refuse")
	}
	if ttl := mr.TTL(rateKey(testRule, "stuck")); ttl <= 0 {
		t.Fatalf("the counter should have been given an expiry, ttl=%v", ttl)
	}
}

func TestNilRateLimiterAllowsEverything(t *testing.T) {
	var l *RateLimiter
	ctx := context.Background()
	if ok, _, err := l.Allow(ctx, testRule, "x"); !ok || err != nil {
		t.Errorf("nil Allow: %v %v", ok, err)
	}
	if blocked, _, err := l.Blocked(ctx, testRule, "x"); blocked || err != nil {
		t.Errorf("nil Blocked: %v %v", blocked, err)
	}
	if err := l.Fail(ctx, testRule, "x"); err != nil {
		t.Error(err)
	}
	if err := l.Reset(ctx, testRule, "x"); err != nil {
		t.Error(err)
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        []string
		hops       int
		want       string
	}{
		{"direct, header ignored when no proxy is trusted", "203.0.113.9:5000", []string{"1.2.3.4"}, 0, "203.0.113.9"},
		{"one proxy: last entry is the client", "172.18.0.5:4000", []string{"198.51.100.7"}, 1, "198.51.100.7"},
		{"one proxy: entries written by the client are ignored", "172.18.0.5:4000", []string{"6.6.6.6, 7.7.7.7, 198.51.100.7"}, 1, "198.51.100.7"},
		{"two proxies: second from the right", "172.18.0.5:4000", []string{"6.6.6.6, 198.51.100.7, 172.18.0.9"}, 2, "198.51.100.7"},
		{"repeated header lines are joined in order", "172.18.0.5:4000", []string{"6.6.6.6", "198.51.100.7"}, 1, "198.51.100.7"},
		{"header missing: fall back to the peer", "172.18.0.5:4000", nil, 1, "172.18.0.5"},
		{"fewer entries than trusted proxies: fall back to the peer", "172.18.0.5:4000", []string{"198.51.100.7"}, 2, "172.18.0.5"},
		{"garbage entry: fall back to the peer", "172.18.0.5:4000", []string{"not-an-ip"}, 1, "172.18.0.5"},
		{"IPv4-mapped IPv6 matches plain IPv4", "172.18.0.5:4000", []string{"::ffff:198.51.100.7"}, 1, "198.51.100.7"},
		{"IPv6 clients share their /64", "172.18.0.5:4000", []string{"2001:db8:1:2:aaaa:bbbb:cccc:dddd"}, 1, "2001:db8:1:2::/64"},
		{"IPv6 peer", "[2001:db8:1:2:1:2:3:4]:4000", nil, 0, "2001:db8:1:2::/64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remoteAddr
			for _, v := range tt.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := clientIP(r, tt.hops); got != tt.want {
				t.Errorf("clientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func newLimitedApp(t *testing.T, hops int) *App {
	t.Helper()
	rdb, _ := newTestRedis(t)
	return &App{
		cfg:      &Config{ClientIPKnown: true, TrustedProxyHops: hops},
		rdb:      rdb,
		limiter:  newRateLimiter(rdb),
		sessions: newSessionStore(rdb, false),
		otp:      newOTPService(rdb, "test-secret-test-secret"),
	}
}

func TestIPRateLimitMiddleware(t *testing.T) {
	a := newLimitedApp(t, 1)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := a.rateLimitRequests(ok)

	do := func(method, path, xff string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.RemoteAddr = "172.18.0.5:4000"
		r.Header.Set("X-Forwarded-For", xff)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}

	for i := 0; i < int(ruleLoginIP.limit); i++ {
		if rec := do(http.MethodPost, "/api/login", "198.51.100.7"); rec.Code != http.StatusOK {
			t.Fatalf("login attempt %d refused too early: %d", i+1, rec.Code)
		}
	}
	rec := do(http.MethodPost, "/api/login", "198.51.100.7")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("login attempt past the limit: %d, want 429", rec.Code)
	}
	if secs, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || secs < 1 {
		t.Errorf("Retry-After = %q, want a positive number of seconds", rec.Header().Get("Retry-After"))
	}
	var body map[string]string
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"] == "" {
		t.Error("the 429 response should explain itself in the usual JSON error format")
	}

	// A forged leading entry does not give the client a fresh allowance.
	if rec := do(http.MethodPost, "/api/login", "9.9.9.9, 198.51.100.7"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("spoofed X-Forwarded-For escaped the limit: %d", rec.Code)
	}
	// Another client is unaffected, and so are routes without a limit.
	if rec := do(http.MethodPost, "/api/login", "203.0.113.50"); rec.Code != http.StatusOK {
		t.Errorf("a different client was limited: %d", rec.Code)
	}
	if rec := do(http.MethodGet, "/api/me", "198.51.100.7"); rec.Code != http.StatusOK {
		t.Errorf("an unlimited route was limited: %d", rec.Code)
	}
	// The other sign-in routes have their own buckets.
	if rec := do(http.MethodPost, "/api/login/verify-otp", "198.51.100.7"); rec.Code != http.StatusOK {
		t.Errorf("verify-otp shares the login bucket: %d", rec.Code)
	}
}

func TestIPRateLimitsStayOffUntilClientIPIsKnown(t *testing.T) {
	a := newLimitedApp(t, 0)
	a.cfg.ClientIPKnown = false // TRUSTED_PROXY_HOPS not set
	h := a.rateLimitRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for i := 0; i < int(ruleLoginIP.limit)*3; i++ {
		r := httptest.NewRequest(http.MethodPost, "/api/login", nil)
		r.RemoteAddr = "172.18.0.5:4000" // every user looks like the frontend proxy
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d was limited (%d): one shared limit would lock out every voter", i+1, rec.Code)
		}
	}
}

func TestPublicVerifyRouteIsRateLimited(t *testing.T) {
	a := newLimitedApp(t, 0)
	h := a.rateLimitRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	var last int
	for i := 0; i < int(ruleVerifyBallotIP.limit)+1; i++ {
		r := httptest.NewRequest(http.MethodGet, "/api/verify/"+strings.Repeat("a", 64), nil)
		r.RemoteAddr = "203.0.113.9:1111"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		last = rec.Code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("request %d to /api/verify: %d, want 429", ruleVerifyBallotIP.limit+1, last)
	}
}

func TestLimiterFailsClosedWhenRedisIsDown(t *testing.T) {
	a := newLimitedApp(t, 0)
	a.rdb.Close()
	h := a.rateLimitRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	r := httptest.NewRequest(http.MethodPost, "/api/login", nil)
	r.RemoteAddr = "203.0.113.9:1111"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("with Redis down: %d, want 503", rec.Code)
	}
}

// Wrong sign-in codes are counted per account. Starting a new sign-in creates anew challenge with its own five attempts, but must not reset this count.
func TestOTPFailuresAreCountedAcrossChallenges(t *testing.T) {
	a := newLimitedApp(t, 0)
	ctx := context.Background()
	const userID = "user-1"

	attempt := func(challengeID, code string) (int, string) {
		raw, _ := json.Marshal(map[string]string{"otp": code})
		r := httptest.NewRequest(http.MethodPost, "/api/login/verify-otp", bytes.NewReader(raw))
		r = r.WithContext(context.WithValue(r.Context(), ctxKey{},
			&Session{PendingChallengeID: challengeID, PendingUserID: userID}))
		rec := httptest.NewRecorder()
		a.handleVerifyOTP(rec, r)
		return rec.Code, rec.Header().Get("Retry-After")
	}
	wrongFor := func(otp string) string {
		if otp == "000000" {
			return "000001"
		}
		return "000000"
	}

	failures := 0
	for failures < int(ruleOTPFailures.limit) {
		id, otp, err := a.otp.CreateChallenge(ctx, userID) // a fresh sign-in
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < otpMaxAttempts-1 && failures < int(ruleOTPFailures.limit); i++ {
			code, _ := attempt(id, wrongFor(otp))
			if code != http.StatusUnauthorized {
				t.Fatalf("wrong code #%d: status %d, want 401", failures+1, code)
			}
			failures++
		}
	}

	// Even the correct code from a brand-new challenge is now refused.
	id, otp, err := a.otp.CreateChallenge(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	code, retryAfter := attempt(id, otp)
	if code != http.StatusTooManyRequests {
		t.Fatalf("after %d wrong codes the account should be blocked, got status %d", failures, code)
	}
	if retryAfter == "" {
		t.Error("expected a Retry-After header")
	}
}

func TestOTPSendLimitStopsEmailFlooding(t *testing.T) {
	a := newLimitedApp(t, 0)
	ctx := context.Background()
	for i := 0; i < int(ruleOTPSends.limit); i++ {
		rec := httptest.NewRecorder()
		if !a.allowOTPSend(ctx, rec, "user-1") {
			t.Fatalf("code %d should be allowed", i+1)
		}
	}
	rec := httptest.NewRecorder()
	if a.allowOTPSend(ctx, rec, "user-1") {
		t.Fatal("one code too many was allowed")
	}
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("got %d / Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if !a.allowOTPSend(ctx, httptest.NewRecorder(), "user-2") {
		t.Error("another account should not be affected")
	}
}

func TestTrustedProxyHopsConfig(t *testing.T) {
	t.Setenv("DB_NAME", "db")
	t.Setenv("DB_USER", "user")
	t.Setenv("OTP_SECRET", "0123456789abcdef0123")
	t.Setenv("APP_ENV", "development")

	t.Setenv("TRUSTED_PROXY_HOPS", "")
	if cfg, err := loadConfig(); err != nil || cfg.ClientIPKnown {
		t.Errorf("unset: ClientIPKnown should be false (err=%v)", err)
	}
	for value, want := range map[string]int{"0": 0, "1": 1, " 2 ": 2} {
		t.Setenv("TRUSTED_PROXY_HOPS", value)
		cfg, err := loadConfig()
		if err != nil || !cfg.ClientIPKnown || cfg.TrustedProxyHops != want {
			t.Errorf("TRUSTED_PROXY_HOPS=%q: cfg=%+v err=%v, want known with %d hops", value, cfg, err, want)
		}
	}
	for _, bad := range []string{"abc", "-1", "11", "1.5"} {
		t.Setenv("TRUSTED_PROXY_HOPS", bad)
		if _, err := loadConfig(); err == nil {
			t.Errorf("TRUSTED_PROXY_HOPS=%q should be rejected", bad)
		}
	}
}

func TestAccountRateKeyIgnoresCaseAndSpaces(t *testing.T) {
	if accountRateKey("User@UGM.ac.id ") != accountRateKey("user@ugm.ac.id") {
		t.Error("the same address in a different case must share a counter")
	}
	if accountRateKey("a@ugm.ac.id") == accountRateKey("b@ugm.ac.id") {
		t.Error("different addresses must not share a counter")
	}
	if strings.Contains(accountRateKey("user@ugm.ac.id"), "ugm") {
		t.Error("the key must not contain the email address")
	}
}
