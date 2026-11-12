package main

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestQR(t *testing.T) (*QRService, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return newQRService(rdb), mr
}

func TestQRTokenIsSingleUse(t *testing.T) {
	s, _ := newTestQR(t)
	ctx := context.Background()
	token, err := s.Issue(ctx, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	uid, ok, err := s.Redeem(ctx, token)
	if err != nil || !ok || uid != "user-1" {
		t.Fatalf("first redeem = (%q, %v, %v), want (user-1, true, nil)", uid, ok, err)
	}
	if _, ok, _ := s.Redeem(ctx, token); ok {
		t.Fatal("token must not be redeemable twice")
	}
}

func TestQRTokenExpires(t *testing.T) {
	s, mr := newTestQR(t)
	ctx := context.Background()
	token, _ := s.Issue(ctx, "user-1")
	mr.FastForward(qrTokenTTL + time.Second)
	if _, ok, _ := s.Redeem(ctx, token); ok {
		t.Fatal("expired token must be rejected")
	}
}

func TestQRNewTokenInvalidatesOldOne(t *testing.T) {
	s, _ := newTestQR(t)
	ctx := context.Background()
	oldTok, _ := s.Issue(ctx, "user-1")
	newTok, _ := s.Issue(ctx, "user-1")
	if _, ok, _ := s.Redeem(ctx, oldTok); ok {
		t.Fatal("old token must be invalid after a new one is issued")
	}
	if uid, ok, _ := s.Redeem(ctx, newTok); !ok || uid != "user-1" {
		t.Fatal("new token should work")
	}
}

func TestQRRejectsGarbageAndStoresOnlyHashes(t *testing.T) {
	s, mr := newTestQR(t)
	ctx := context.Background()
	for _, bad := range []string{"", "nope", string(make([]byte, 200))} {
		if _, ok, err := s.Redeem(ctx, bad); ok || err != nil {
			t.Fatalf("Redeem(%q) = ok:%v err:%v, want rejected", bad, ok, err)
		}
	}
	token, _ := s.Issue(ctx, "user-1")
	for _, k := range mr.Keys() {
		if k == "qr:login:"+token {
			t.Fatal("raw token must never be used as a Redis key")
		}
	}
	if mr.Exists("qr:login:"+hashQRToken(token)) == false {
		t.Fatal("hashed token key should exist")
	}
}

func TestQRTokensAreUnique(t *testing.T) {
	s, _ := newTestQR(t)
	ctx := context.Background()
	a, _ := s.Issue(ctx, "user-1")
	b, _ := s.Issue(ctx, "user-2")
	if a == b || len(a) < 40 {
		t.Fatalf("tokens should be long and unique: %q %q", a, b)
	}
}
