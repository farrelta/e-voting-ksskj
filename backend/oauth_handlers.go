package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// oauthError codes are appended to the login page URL so the UI can explain
// what happened without leaking any account detail to an anonymous visitor.
const (
	oauthErrDisabled     = "oauth_disabled"
	oauthErrState        = "oauth_state"
	oauthErrExchange     = "oauth_exchange"
	oauthErrToken        = "oauth_token"
	oauthErrUnregistered = "oauth_unregistered"
	oauthErrDomain       = "oauth_domain"
	oauthErrRole         = "oauth_role"
	oauthErrAccount      = "oauth_account"
	oauthErrConflict     = "oauth_conflict"
	oauthErrOTP          = "oauth_otp"
)

// redirectBackToLogin sends the browser back to the sign-in page with a code
// that identifies the failure, and marks the response as uncacheable because
// the URL describes an authentication attempt.
func redirectBackToLogin(w http.ResponseWriter, r *http.Request, code string) {
	w.Header().Set("Cache-Control", "no-store")
	q := url.Values{}
	q.Set("error", code)
	http.Redirect(w, r, "/login?"+q.Encode(), http.StatusFound)
}

// GET /api/auth/google
// Starts the authorization code flow by redirecting to Google.
func (a *App) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	if !a.cfg.OAuthEnabled {
		redirectBackToLogin(w, r, oauthErrDisabled)
		return
	}

	state, nonce, err := a.oauth.StartState(r.Context())
	if err != nil {
		log.Printf("oauth start state: %v", err)
		redirectBackToLogin(w, r, oauthErrState)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, a.oauth.AuthCodeURL(state, nonce), http.StatusFound)
}

// GET /api/auth/google/callback
// Validates the response from Google and, on success, hands the user to the
// existing email OTP step. It never creates a session on its own.
func (a *App) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if !a.cfg.OAuthEnabled {
		redirectBackToLogin(w, r, oauthErrDisabled)
		return
	}

	// Google reports a denied consent or a broken request as an error parameter.
	if providerErr := r.URL.Query().Get("error"); providerErr != "" {
		log.Printf("oauth callback provider error: %s", providerErr)
		redirectBackToLogin(w, r, oauthErrExchange)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		redirectBackToLogin(w, r, oauthErrExchange)
		return
	}

	nonce, err := a.oauth.ConsumeState(ctx, r.URL.Query().Get("state"))
	if err != nil {
		if !errors.Is(err, errStateInvalid) {
			log.Printf("oauth consume state: %v", err)
		}
		redirectBackToLogin(w, r, oauthErrState)
		return
	}

	tokens, err := a.oauth.Exchange(ctx, code)
	if err != nil {
		log.Printf("oauth exchange: %v", err)
		redirectBackToLogin(w, r, oauthErrExchange)
		return
	}

	claims, err := a.oauth.VerifyIDToken(ctx, tokens.IDToken, nonce, timeNow())
	if err != nil {
		log.Printf("oauth verify id token: %v", err)
		redirectBackToLogin(w, r, oauthErrToken)
		return
	}

	// The hd claim is already enforced during verification; this repeats the
	// domain rule that guards participant registration.
	if !emailInDomain(claims.Email, a.cfg.AllowedEmailDomain) {
		redirectBackToLogin(w, r, oauthErrDomain)
		return
	}

	user, err := a.linkOAuthIdentity(ctx, claims)
	switch {
	case errors.Is(err, errOAuthUserUnknown):
		redirectBackToLogin(w, r, oauthErrUnregistered)
		return
	case errors.Is(err, errOAuthRoleNotAllowed):
		redirectBackToLogin(w, r, oauthErrRole)
		return
	case errors.Is(err, errOAuthIdentityConflict):
		redirectBackToLogin(w, r, oauthErrConflict)
		return
	case err != nil:
		log.Printf("oauth link identity: %v", err)
		redirectBackToLogin(w, r, oauthErrAccount)
		return
	}

	if !user.IsVerified {
		redirectBackToLogin(w, r, oauthErrAccount)
		return
	}

	// Hand over to the existing OTP step: store a pending challenge on the
	// session exactly as the password and QR flows do.
	challengeID, otp, err := a.otp.CreateChallenge(ctx, user.ID)
	if err != nil {
		log.Printf("oauth create otp challenge: %v", err)
		redirectBackToLogin(w, r, oauthErrOTP)
		return
	}

	if err := a.deliverOTP(ctx, user.Email, otp); err != nil {
		log.Printf("oauth send login OTP email: %v", err)
		if cleanupErr := a.otp.DeleteChallenge(ctx, challengeID); cleanupErr != nil {
			log.Printf("oauth delete unsent OTP challenge: %v", cleanupErr)
		}
		redirectBackToLogin(w, r, oauthErrOTP)
		return
	}

	// Start from a clean session. If the browser already had an authenticated
	// session, reusing it here would convert it into a pending sign-in for the
	// Google account instead, and abandoning the OTP step would strand the user
	// logged out of their own account.
	sess := sessionFrom(r)
	if err := a.sessions.Destroy(ctx, w, sess); err != nil {
		log.Printf("oauth reset session: %v", err)
		if cleanupErr := a.otp.DeleteChallenge(ctx, challengeID); cleanupErr != nil {
			log.Printf("oauth delete unsent OTP challenge: %v", cleanupErr)
		}
		redirectBackToLogin(w, r, oauthErrAccount)
		return
	}
	sess.PendingChallengeID = challengeID
	sess.PendingUserID = user.ID
	if err := a.sessions.Save(ctx, w, sess); err != nil {
		log.Printf("oauth save session: %v", err)
		if cleanupErr := a.otp.DeleteChallenge(ctx, challengeID); cleanupErr != nil {
			log.Printf("oauth delete unsaved OTP challenge: %v", cleanupErr)
		}
		redirectBackToLogin(w, r, oauthErrAccount)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/login?step=otp", http.StatusFound)
}

var (
	errOAuthUserUnknown      = errors.New("no account matches the google email")
	errOAuthRoleNotAllowed   = errors.New("account role cannot sign in with google")
	errOAuthIdentityConflict = errors.New("another google account is already linked to this user")
)

// oauthAccount is the account a verified Google identity resolves to.
type oauthAccount struct {
	ID         string
	Email      string
	Role       string
	IsVerified bool
}

// linkOAuthIdentity resolves a verified Google identity to an existing account.
// It links the identity on first use, but never creates an account.
func (a *App) linkOAuthIdentity(ctx context.Context, claims *googleClaims) (*oauthAccount, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// 1. An already linked identity is the only trusted path: the subject is
	// immutable while a Google account email can change.
	var acct oauthAccount
	err = tx.QueryRow(ctx, `
		SELECT u.id::text, u.email, u.role::text, u.is_verified
		FROM evoting.oauth_identities oi
		JOIN evoting.users u ON u.id = oi.user_id
		WHERE oi.provider = $1 AND oi.subject = $2`,
		oauthProviderGoogle, claims.Subject,
	).Scan(&acct.ID, &acct.Email, &acct.Role, &acct.IsVerified)
	if err == nil {
		if _, err := tx.Exec(ctx, `
			UPDATE evoting.oauth_identities SET last_login_at = CURRENT_TIMESTAMP
			WHERE provider = $1 AND subject = $2`,
			oauthProviderGoogle, claims.Subject); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &acct, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	// 2. First sign-in. An account can only be linked by one request at a time,
	// so take a row lock. This also closes the race where two concurrent
	// callbacks both pass the subject lookup above.
	err = tx.QueryRow(ctx, `
		SELECT u.id::text, u.email, u.role::text, u.is_verified
		FROM evoting.users u
		WHERE lower(u.email) = $1
		FOR UPDATE`,
		strings.ToLower(claims.Email),
	).Scan(&acct.ID, &acct.Email, &acct.Role, &acct.IsVerified)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errOAuthUserUnknown
	}
	if err != nil {
		return nil, err
	}

	if acct.Role != "voter" && acct.Role != "admin" {
		return nil, errOAuthRoleNotAllowed
	}

	// Re-check under the lock: another request may have linked this subject
	// while we waited for the row.
	err = tx.QueryRow(ctx, `
		SELECT u.id::text, u.email, u.role::text, u.is_verified
		FROM evoting.oauth_identities oi
		JOIN evoting.users u ON u.id = oi.user_id
		WHERE oi.provider = $1 AND oi.subject = $2`,
		oauthProviderGoogle, claims.Subject,
	).Scan(&acct.ID, &acct.Email, &acct.Role, &acct.IsVerified)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &acct, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO evoting.oauth_identities (user_id, provider, subject, email, last_login_at)
		VALUES ($1::uuid, $2, $3, $4, CURRENT_TIMESTAMP)`,
		acct.ID, oauthProviderGoogle, claims.Subject, acct.Email,
	); err != nil {
		// One app account may only be linked to a given provider once. A
		// different Google account resolving to the same user is a conflict the
		// administrator has to resolve, not something to silently accept.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
			pgErr.ConstraintName == "unique_user_provider" {
			return nil, errOAuthIdentityConflict
		}
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &acct, nil
}
