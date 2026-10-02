package main

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// These tests use miniredis (an in-memory fake Redis), so they need
// neither Postgres nor a running Redis server.

func newTestOTP(t *testing.T) (*OTPService, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return newOTPService(rdb, "test-secret-test-secret"), mr
}

func TestOTPFormat(t *testing.T) {
	s, _ := newTestOTP(t)
	_, otp, err := s.CreateChallenge(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^\d{6}$`).MatchString(otp) {
		t.Fatalf("OTP should be 6 digits, got %q", otp)
	}
}

func TestOTPCorrectCode(t *testing.T) {
	s, _ := newTestOTP(t)
	ctx := context.Background()
	id, otp, _ := s.CreateChallenge(ctx, "user-1")

	res, err := s.VerifyChallenge(ctx, id, otp)
	if err != nil || !res.Success || res.UserID != "user-1" {
		t.Fatalf("expected success for user-1, got %+v err=%v", res, err)
	}

	// A used OTP must not work a second time.
	res, _ = s.VerifyChallenge(ctx, id, otp)
	if res.Success || res.Reason != reasonExpiredOrInvalid {
		t.Fatalf("reused OTP should be rejected, got %+v", res)
	}
}

func TestOTPWrongCode(t *testing.T) {
	s, _ := newTestOTP(t)
	ctx := context.Background()
	id, otp, _ := s.CreateChallenge(ctx, "user-1")

	wrong := "000000"
	if otp == wrong {
		wrong = "111111"
	}
	res, _ := s.VerifyChallenge(ctx, id, wrong)
	if res.Success || res.Reason != reasonInvalidOTP {
		t.Fatalf("expected invalid_otp, got %+v", res)
	}
	// The right code still works after one wrong try.
	res, _ = s.VerifyChallenge(ctx, id, otp)
	if !res.Success {
		t.Fatalf("correct code should still work, got %+v", res)
	}
}

func TestOTPLockoutAfterFiveAttempts(t *testing.T) {
	s, _ := newTestOTP(t)
	ctx := context.Background()
	id, otp, _ := s.CreateChallenge(ctx, "user-1")

	wrong := "000000"
	if otp == wrong {
		wrong = "111111"
	}
	for i := 1; i <= 5; i++ {
		res, _ := s.VerifyChallenge(ctx, id, wrong)
		if res.Reason != reasonInvalidOTP {
			t.Fatalf("attempt %d: expected invalid_otp, got %+v", i, res)
		}
	}
	res, _ := s.VerifyChallenge(ctx, id, otp) // even the right code is refused now
	if res.Success || res.Reason != reasonTooManyAttempts {
		t.Fatalf("attempt 6 should be locked out, got %+v", res)
	}
}

func TestOTPExpires(t *testing.T) {
	s, mr := newTestOTP(t)
	ctx := context.Background()
	id, otp, _ := s.CreateChallenge(ctx, "user-1")

	mr.FastForward(otpTTL + time.Second)

	res, _ := s.VerifyChallenge(ctx, id, otp)
	if res.Success || res.Reason != reasonExpiredOrInvalid {
		t.Fatalf("expired OTP should be rejected, got %+v", res)
	}
}

func TestOTPResendCooldownAndNewCode(t *testing.T) {
	s, mr := newTestOTP(t)
	ctx := context.Background()
	id, oldOTP, _ := s.CreateChallenge(ctx, "user-1")

	ok, _ := s.CanResend(ctx, id)
	if !ok {
		t.Fatal("first resend should be allowed")
	}
	ok, _ = s.CanResend(ctx, id)
	if ok {
		t.Fatal("second resend within 30s should be blocked")
	}
	mr.FastForward(otpResendCooldown + time.Second)
	if ok, _ = s.CanResend(ctx, id); !ok {
		t.Fatal("resend should be allowed again after the cooldown")
	}

	newOTP, err := s.Resend(ctx, id)
	if err != nil || newOTP == "" {
		t.Fatalf("resend failed: %q %v", newOTP, err)
	}
	if newOTP != oldOTP {
		if res, _ := s.VerifyChallenge(ctx, id, oldOTP); res.Success {
			t.Fatal("old OTP must stop working after a resend")
		}
	}
	if res, _ := s.VerifyChallenge(ctx, id, newOTP); !res.Success {
		t.Fatalf("new OTP should work, got %+v", res)
	}
}

func TestNewLoginInvalidatesOldChallenge(t *testing.T) {
	s, _ := newTestOTP(t)
	ctx := context.Background()
	id1, otp1, _ := s.CreateChallenge(ctx, "user-1")
	_, _, _ = s.CreateChallenge(ctx, "user-1") // user logs in again

	res, _ := s.VerifyChallenge(ctx, id1, otp1)
	if res.Success {
		t.Fatal("first challenge should be invalidated by the second login")
	}
}
