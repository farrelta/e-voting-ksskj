package main

import (
	"context"
	"errors"
	"log"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// rateRule allows at most limit events per window for one key.
type rateRule struct {
	name   string
	limit  int64
	window time.Duration
}

// Limits are per client IP unless the name says otherwise. They are generous for a real person and cheap to hit for a script.
var (
	ruleLoginIP        = rateRule{"login-ip", 20, 10 * time.Minute}
	ruleOTPVerifyIP    = rateRule{"otp-verify-ip", 30, 10 * time.Minute}
	ruleOTPResendIP    = rateRule{"otp-resend-ip", 30, 10 * time.Minute}
	ruleQRLoginIP      = rateRule{"qr-login-ip", 30, 10 * time.Minute}
	ruleVerifyBallotIP = rateRule{"verify-ballot-ip", 60, time.Minute}

	ruleLoginFailures = rateRule{"login-fail", 10, 15 * time.Minute} // wrong passwords
	ruleOTPSends      = rateRule{"otp-send", 5, time.Hour}           // sign-in codes emailed
	ruleOTPFailures   = rateRule{"otp-fail", 10, 15 * time.Minute}   // wrong codes, across all challenges
)

var rateLimitScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
local ttl = redis.call('PTTL', KEYS[1])
if count == 1 or ttl < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
  ttl = tonumber(ARGV[1])
end
return {count, ttl}
`)

// RateLimiter keeps fixed-window counters in Redis. A nil *RateLimiter allows everything, which keeps tests that build an App by hand simple.
type RateLimiter struct {
	rdb *redis.Client
}

func newRateLimiter(rdb *redis.Client) *RateLimiter {
	return &RateLimiter{rdb: rdb}
}

func rateKey(rule rateRule, id string) string { return "rl:" + rule.name + ":" + id }

func (l *RateLimiter) record(ctx context.Context, rule rateRule, id string) (int64, time.Duration, error) {
	res, err := rateLimitScript.Run(ctx, l.rdb, []string{rateKey(rule, id)}, rule.window.Milliseconds()).Int64Slice()
	if err != nil {
		return 0, 0, err
	}
	if len(res) != 2 {
		return 0, 0, errors.New("unexpected rate limit script result")
	}
	return res[0], time.Duration(res[1]) * time.Millisecond, nil
}

// Allow counts one event and reports whether it is within the limit. When it is not, the duration is how long until the window resets.
func (l *RateLimiter) Allow(ctx context.Context, rule rateRule, id string) (bool, time.Duration, error) {
	if l == nil {
		return true, 0, nil
	}
	count, wait, err := l.record(ctx, rule, id)
	if err != nil {
		return false, 0, err
	}
	return count <= rule.limit, wait, nil
}

// Blocked reports whether id has already used up the rule, without counting a new event. Used with Fail for counters that only count failures.
func (l *RateLimiter) Blocked(ctx context.Context, rule rateRule, id string) (bool, time.Duration, error) {
	if l == nil {
		return false, 0, nil
	}
	count, err := l.rdb.Get(ctx, rateKey(rule, id)).Int64()
	if errors.Is(err, redis.Nil) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	if count < rule.limit {
		return false, 0, nil
	}
	wait, err := l.rdb.PTTL(ctx, rateKey(rule, id)).Result()
	if err != nil {
		return false, 0, err
	}
	if wait < 0 {
		wait = rule.window
	}
	return true, wait, nil
}

// Fail counts one failure against id.
func (l *RateLimiter) Fail(ctx context.Context, rule rateRule, id string) error {
	if l == nil {
		return nil
	}
	_, _, err := l.record(ctx, rule, id)
	return err
}

// Reset forgets the failures counted against id.
func (l *RateLimiter) Reset(ctx context.Context, rule rateRule, id string) error {
	if l == nil {
		return nil
	}
	return l.rdb.Del(ctx, rateKey(rule, id)).Err()
}

func tooManyRequests(w http.ResponseWriter, wait time.Duration, message string) {
	seconds := int(math.Ceil(wait.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeError(w, http.StatusTooManyRequests, message)
}

// limiterUnavailable is the answer when Redis cannot be reached.
func limiterUnavailable(w http.ResponseWriter, err error) {
	log.Printf("rate limiter: %v", err)
	writeError(w, http.StatusServiceUnavailable, "Service temporarily unavailable. Please try again shortly.")
}

// allowOTPSend limits how many sign-in codes one account can be emailed. It writes the error response itself and returns false when the caller must stop.
func (a *App) allowOTPSend(ctx context.Context, w http.ResponseWriter, userID string) bool {
	ok, wait, err := a.limiter.Allow(ctx, ruleOTPSends, userID)
	if err != nil {
		limiterUnavailable(w, err)
		return false
	}
	if !ok {
		tooManyRequests(w, wait, "Too many sign-in codes have been requested. Please try again later.")
		return false
	}
	return true
}

// ipRuleFor picks the per-IP rule for a request, if it has one.
func ipRuleFor(r *http.Request) (rateRule, bool) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/login":
		return ruleLoginIP, true
	case r.Method == http.MethodPost && r.URL.Path == "/api/login/verify-otp":
		return ruleOTPVerifyIP, true
	case r.Method == http.MethodPost && r.URL.Path == "/api/login/resend-otp":
		return ruleOTPResendIP, true
	case r.Method == http.MethodPost && r.URL.Path == "/api/login/qr":
		return ruleQRLoginIP, true
	case (r.Method == http.MethodGet || r.Method == http.MethodHead) && strings.HasPrefix(r.URL.Path, "/api/verify/"):
		return ruleVerifyBallotIP, true
	}
	return rateRule{}, false
}

func (a *App) rateLimitRequests(next http.Handler) http.Handler {
	if a.limiter == nil || a.cfg == nil || !a.cfg.ClientIPKnown {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rule, ok := ipRuleFor(r); ok {
			allowed, wait, err := a.limiter.Allow(r.Context(), rule, a.clientIP(r))
			if err != nil {
				limiterUnavailable(w, err)
				return
			}
			if !allowed {
				tooManyRequests(w, wait, "Too many attempts. Please try again later.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) clientIP(r *http.Request) string {
	hops := 0
	if a.cfg != nil {
		hops = a.cfg.TrustedProxyHops
	}
	return clientIP(r, hops)
}

func clientIP(r *http.Request, trustedHops int) string {
	peer := r.RemoteAddr
	if host, _, err := net.SplitHostPort(peer); err == nil {
		peer = host
	}
	ip := peer
	if trustedHops > 0 {
		var hops []string
		for _, header := range r.Header.Values("X-Forwarded-For") {
			for _, part := range strings.Split(header, ",") {
				if part = strings.TrimSpace(part); part != "" {
					hops = append(hops, part)
				}
			}
		}
		if i := len(hops) - trustedHops; i >= 0 {
			if _, err := netip.ParseAddr(hops[i]); err == nil {
				ip = hops[i]
			}
		}
	}
	return normalizeIP(ip)
}

// normalizeIP makes equivalent addresses share one bucket. IPv6 users can choose any address in their /64, so the whole /64 is treated as one client.
func normalizeIP(value string) string {
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return value
	}
	addr = addr.WithZone("").Unmap()
	if addr.Is6() {
		return netip.PrefixFrom(addr, 64).Masked().String()
	}
	return addr.String()
}
