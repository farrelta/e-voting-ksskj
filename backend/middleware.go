package main

import (
	"context"
	"log"
	"net/http"
)

type ctxKey struct{}

func (a *App) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := a.sessions.Load(r.Context(), r)
		if err != nil {
			log.Printf("load session: %v", err)
			writeError(w, http.StatusInternalServerError, "Session store unavailable")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess)))
	})
}

func sessionFrom(r *http.Request) *Session {
	sess, _ := r.Context().Value(ctxKey{}).(*Session)
	if sess == nil {
		return &Session{}
	}
	return sess
}

func requireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sessionFrom(r).User == nil {
			writeError(w, http.StatusUnauthorized, "Not authenticated")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := sessionFrom(r).User
		if user == nil {
			writeError(w, http.StatusUnauthorized, "Not authenticated")
			return
		}
		if user.Role != "admin" {
			writeError(w, http.StatusForbidden, "Admin access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
