package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	googleAuthEndpoint  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenEndpoint = "https://oauth2.googleapis.com/token"
	googleJWKSURL       = "https://www.googleapis.com/oauth2/v3/certs"

	// googleIssuer is the only issuer whose tokens are accepted.
	googleIssuer = "https://accounts.google.com"

	oAuthStateTTL = 10 * time.Minute

	// jwksMaxAge caps how long the signing keys are cached when Google omits
	// Cache-Control.
	jwksMaxAge = time.Hour

	// jwksMinRefresh stops a flood of unknown-key lookups from hammering the
	// key endpoint.
	jwksMinRefresh = 30 * time.Second

	// clockSkewSeconds tolerates small clock differences when checking exp.
	clockSkewSeconds = 60

	// oauthProviderGoogle is the provider value stored in oauth_identities.
	oauthProviderGoogle = "google"
)

// timeNow is indirected so tests can pin the clock.
var timeNow = time.Now

var (
	errStateInvalid = errors.New("oauth state is missing, expired or already used")
	errTokenInvalid = errors.New("google id token is invalid")
)

// oauthStateKey namespaces the single-use authorization state values in Redis.
func oauthStateKey(state string) string { return "oauth:state:" + state }

// googleClaims holds only the ID token claims this application makes decisions on.
type googleClaims struct {
	Issuer        string
	Audience      string
	Subject       string
	Email         string
	EmailVerified bool
	HostedDomain  string
	Nonce         string
	ExpiresAt     time.Time
}

// OAuthService drives the Google OpenID Connect authorization code flow.
type OAuthService struct {
	clientID     string
	clientSecret string
	redirectURL  string
	hd           string
	rdb          *redis.Client
	client       *http.Client

	keysMu        sync.RWMutex
	keys          map[string]*rsa.PublicKey
	keysFetched   time.Time
	keysAttempted time.Time
	keysCacheFor  time.Duration
}

func newOAuthService(cfg *Config, rdb *redis.Client) *OAuthService {
	return &OAuthService{
		clientID:     cfg.GoogleClientID,
		clientSecret: cfg.GoogleClientSecret,
		redirectURL:  cfg.GoogleRedirectURL,
		hd:           cfg.AllowedEmailDomain,
		rdb:          rdb,
		client:       &http.Client{Timeout: 10 * time.Second},
		keys:         map[string]*rsa.PublicKey{},
	}
}

func randomURLToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// StartState creates a single-use state value and remembers the nonce that the
// resulting ID token must carry.
func (s *OAuthService) StartState(ctx context.Context) (state, nonce string, err error) {
	state, err = randomURLToken()
	if err != nil {
		return "", "", err
	}
	nonce, err = randomURLToken()
	if err != nil {
		return "", "", err
	}
	if err := s.rdb.Set(ctx, oauthStateKey(state), nonce, oAuthStateTTL).Err(); err != nil {
		return "", "", err
	}
	return state, nonce, nil
}

// ConsumeState reads and deletes the state in one atomic step, so a replayed or
// concurrent callback can never redeem the same state twice.
func (s *OAuthService) ConsumeState(ctx context.Context, state string) (string, error) {
	if state == "" || len(state) > 128 {
		return "", errStateInvalid
	}
	nonce, err := s.rdb.GetDel(ctx, oauthStateKey(state)).Result()
	if errors.Is(err, redis.Nil) {
		return "", errStateInvalid
	}
	if err != nil {
		return "", err
	}
	if nonce == "" {
		return "", errStateInvalid
	}
	return nonce, nil
}

// AuthCodeURL builds the consent redirect. The hd parameter pre-filters the
// account chooser to the allowed domain; the hd claim is still verified later
// because a request parameter cannot be trusted.
func (s *OAuthService) AuthCodeURL(state, nonce string) string {
	q := url.Values{}
	q.Set("client_id", s.clientID)
	q.Set("redirect_uri", s.redirectURL)
	q.Set("response_type", "code")
	q.Set("scope", "openid email profile")
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("prompt", "select_account")
	if s.hd != "" {
		q.Set("hd", s.hd)
	}
	return googleAuthEndpoint + "?" + q.Encode()
}

// TokenResponse is the subset of the token endpoint payload that is used.
type TokenResponse struct {
	IDToken     string `json:"id_token"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
}

// Exchange swaps an authorization code for tokens. The request is made server
// to server, so the ID token never travels through the browser.
func (s *OAuthService) Exchange(ctx context.Context, code string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", s.clientID)
	form.Set("client_secret", s.clientSecret)
	form.Set("redirect_uri", s.redirectURL)
	form.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenEndpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tr TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK || tr.Error != "" {
		return nil, fmt.Errorf("token endpoint returned %d: %s %s", resp.StatusCode, tr.Error, tr.ErrorDesc)
	}
	if tr.IDToken == "" {
		return nil, errors.New("token response carried no id_token")
	}
	return &tr, nil
}

// VerifyIDToken validates the ID token signature and every claim that the
// application depends on, and returns the verified claims.
func (s *OAuthService) VerifyIDToken(ctx context.Context, rawIDToken, expectedNonce string, now time.Time) (*googleClaims, error) {
	parts := strings.Split(rawIDToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: not a JWS", errTokenInvalid)
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: bad header encoding", errTokenInvalid)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("%w: bad header", errTokenInvalid)
	}
	// Only RS256 is accepted; anything else would allow a downgrade attack.
	if header.Alg != "RS256" {
		return nil, fmt.Errorf("%w: unexpected alg %q", errTokenInvalid, header.Alg)
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: bad signature encoding", errTokenInvalid)
	}

	key, err := s.signingKey(ctx, header.Kid)
	if err != nil {
		return nil, err
	}

	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return nil, fmt.Errorf("%w: signature verification failed", errTokenInvalid)
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: bad payload encoding", errTokenInvalid)
	}
	var payload struct {
		Iss           string `json:"iss"`
		Aud           string `json:"aud"`
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		HD            string `json:"hd"`
		Nonce         string `json:"nonce"`
		Exp           int64  `json:"exp"`
	}
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, fmt.Errorf("%w: bad payload", errTokenInvalid)
	}

	if payload.Iss != googleIssuer && payload.Iss != "accounts.google.com" {
		return nil, fmt.Errorf("%w: unexpected issuer %q", errTokenInvalid, payload.Iss)
	}
	if payload.Aud != s.clientID {
		return nil, fmt.Errorf("%w: token was not issued for this client", errTokenInvalid)
	}
	if payload.Sub == "" {
		return nil, fmt.Errorf("%w: missing subject", errTokenInvalid)
	}
	if payload.Exp == 0 || now.Add(clockSkewSeconds*time.Second).Unix() >= payload.Exp {
		return nil, fmt.Errorf("%w: token is expired", errTokenInvalid)
	}
	if expectedNonce == "" || payload.Nonce != expectedNonce {
		return nil, fmt.Errorf("%w: nonce mismatch", errTokenInvalid)
	}
	if !payload.EmailVerified {
		return nil, fmt.Errorf("%w: email is not verified", errTokenInvalid)
	}
	if payload.Email == "" {
		return nil, fmt.Errorf("%w: token carried no email", errTokenInvalid)
	}
	// The email domain is not trustworthy for organisation membership, so the
	// signed hosted domain claim is the gate. It is required to be configured,
	// so an account without one is rejected rather than let through: Google
	// omits hd for consumer accounts.
	if !hostedDomainAllowed(payload.HD, s.hd) {
		if payload.HD == "" {
			return nil, fmt.Errorf("%w: account has no hosted domain, so it is not a managed %s account", errTokenInvalid, s.hd)
		}
		return nil, fmt.Errorf("%w: hosted domain %q is not part of %s", errTokenInvalid, payload.HD, s.hd)
	}

	return &googleClaims{
		Issuer:        payload.Iss,
		Audience:      payload.Aud,
		Subject:       payload.Sub,
		Email:         strings.ToLower(payload.Email),
		EmailVerified: payload.EmailVerified,
		HostedDomain:  payload.HD,
		Nonce:         payload.Nonce,
		ExpiresAt:     time.Unix(payload.Exp, 0),
	}, nil
}

// hostedDomainAllowed accepts the configured Google Workspace domain and its
// managed subdomains. The value being checked comes from Google's signed hd
// claim, not from the untrusted email string.
func hostedDomainAllowed(hostedDomain, allowedDomain string) bool {
	hostedDomain = strings.ToLower(strings.TrimSpace(hostedDomain))
	allowedDomain = strings.ToLower(strings.TrimSpace(allowedDomain))
	if hostedDomain == "" || allowedDomain == "" {
		return false
	}
	return hostedDomain == allowedDomain || strings.HasSuffix(hostedDomain, "."+allowedDomain)
}

// signingKey returns the RSA key for kid, refreshing the cached key set when
// the key is unknown or the cache has aged out.
func (s *OAuthService) signingKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	if kid == "" {
		return nil, fmt.Errorf("%w: token header has no kid", errTokenInvalid)
	}

	s.keysMu.RLock()
	key, ok := s.keys[kid]
	fresh := time.Since(s.keysFetched) < s.keysCacheFor
	s.keysMu.RUnlock()
	if ok && fresh {
		return key, nil
	}

	// A key that is not in the cache may have been rotated in, so try a
	// refresh. The anti-flood window inside refreshKeys bounds how often that
	// can actually reach Google.
	if err := s.refreshKeys(ctx); err != nil {
		return nil, err
	}

	s.keysMu.RLock()
	key, ok = s.keys[kid]
	s.keysMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: unknown signing key %q", errTokenInvalid, kid)
	}
	return key, nil
}

// refreshKeys reloads the JWKS. The anti-flood window is always honoured, so a
// rotated key costs at most one extra fetch per window.
func (s *OAuthService) refreshKeys(ctx context.Context) error {
	s.keysMu.RLock()
	recent := time.Since(s.keysAttempted) < jwksMinRefresh
	s.keysMu.RUnlock()
	if recent {
		// A fetch was attempted moments ago; if the key were known it would be
		// in the set already. Wait for the next window rather than piling on.
		return nil
	}
	// Record the attempt up front so concurrent callers cannot all slip past
	// the window at once.
	s.keysMu.Lock()
	s.keysAttempted = time.Now()
	s.keysMu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleJWKSURL, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch google signing keys: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch google signing keys: unexpected status %d", resp.StatusCode)
	}

	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("decode jwks: %w", err)
	}

	parsed := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		pub, err := parseRSAPublicKey(k.N, k.E)
		if err != nil {
			continue
		}
		parsed[k.Kid] = pub
	}
	if len(parsed) == 0 {
		return errors.New("jwks contained no usable RSA keys")
	}

	cacheFor := jwksMaxAge
	if cc := resp.Header.Get("Cache-Control"); cc != "" {
		if secs, ok := parseMaxAge(cc); ok && secs > 0 {
			cacheFor = time.Duration(secs) * time.Second
		}
	}

	s.keysMu.Lock()
	s.keys = parsed
	s.keysFetched = time.Now()
	s.keysCacheFor = cacheFor
	s.keysMu.Unlock()
	return nil
}

// parseRSAPublicKey rebuilds an RSA public key from the base64url modulus and
// exponent of a JWK.
func parseRSAPublicKey(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, err
	}
	if len(nBytes) == 0 || len(eBytes) == 0 {
		return nil, errors.New("empty modulus or exponent")
	}

	e := 0
	for _, b := range eBytes {
		e = e<<8 | int(b)
	}
	if e < 3 {
		return nil, errors.New("exponent too small")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}, nil
}

// parseMaxAge pulls max-age out of a Cache-Control header.
func parseMaxAge(header string) (int, bool) {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if v, ok := strings.CutPrefix(part, "max-age="); ok {
			var secs int
			if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &secs); err == nil {
				return secs, true
			}
		}
	}
	return 0, false
}
