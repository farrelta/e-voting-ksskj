package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	sessionCookieName = "evoting.sid"
	sessionTTL        = 24 * time.Hour
)

type SessionUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

type Session struct {
	ID                 string       `json:"-"`
	PendingChallengeID string       `json:"pendingChallengeId,omitempty"`
	User               *SessionUser `json:"user,omitempty"`
}

type SessionStore struct {
	rdb    *redis.Client
	secure bool
}

func newSessionStore(rdb *redis.Client, secure bool) *SessionStore {
	return &SessionStore{rdb: rdb, secure: secure}
}

func sessionKey(id string) string { return "sess:" + id }

func newSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *SessionStore) Load(ctx context.Context, r *http.Request) (*Session, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return &Session{}, nil
	}

	raw, err := s.rdb.Get(ctx, sessionKey(cookie.Value)).Bytes()
	if errors.Is(err, redis.Nil) {
		return &Session{}, nil
	}
	if err != nil {
		return nil, err
	}

	sess := &Session{ID: cookie.Value}
	if err := json.Unmarshal(raw, sess); err != nil {
		return &Session{}, nil
	}
	return sess, nil
}

func (s *SessionStore) Save(ctx context.Context, w http.ResponseWriter, sess *Session) error {
	if sess.ID == "" {
		id, err := newSessionID()
		if err != nil {
			return err
		}
		sess.ID = id
	}

	data, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	if err := s.rdb.Set(ctx, sessionKey(sess.ID), data, sessionTTL).Err(); err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sess.ID,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (s *SessionStore) Destroy(ctx context.Context, w http.ResponseWriter, sess *Session) error {
	if sess.ID != "" {
		if err := s.rdb.Del(ctx, sessionKey(sess.ID)).Err(); err != nil {
			return err
		}
	}
	*sess = Session{}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (s *SessionStore) Regenerate(ctx context.Context, sess *Session) error {
	if sess.ID != "" {
		if err := s.rdb.Del(ctx, sessionKey(sess.ID)).Err(); err != nil {
			return err
		}
	}
	*sess = Session{}
	return nil
}
