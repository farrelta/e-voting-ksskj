package main

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

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
	res, _ = s.VerifyChallenge(ctx, id, otp)
	if !res.Success {
		t.Fatalf("correct OTP should still work, got %+v", res)
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
	for i := 1; i < otpMaxAttempts; i++ {
		res, _ := s.VerifyChallenge(ctx, id, wrong)
		if res.Reason != reasonInvalidOTP {
			t.Fatalf("attempt %d: expected invalid_otp, got %+v", i, res)
		}
	}
	res, _ := s.VerifyChallenge(ctx, id, wrong)
	if res.Success || res.Reason != reasonTooManyAttempts {
		t.Fatalf("attempt %d should be locked out, got %+v", otpMaxAttempts, res)
	}
	res, _ = s.VerifyChallenge(ctx, id, otp)
	if res.Success || res.Reason != reasonExpiredOrInvalid {
		t.Fatalf("locked challenge should be invalidated, got %+v", res)
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
	if ok, _ := s.CanResend(ctx, id); !ok {
		t.Fatal("first resend should be allowed")
	}
	if ok, _ := s.CanResend(ctx, id); ok {
		t.Fatal("second resend within 30s should be blocked")
	}
	mr.FastForward(otpResendCooldown + time.Second)
	if ok, _ := s.CanResend(ctx, id); !ok {
		t.Fatal("resend should be allowed after cooldown")
	}
	newOTP, err := s.Resend(ctx, id)
	if err != nil || newOTP == "" {
		t.Fatalf("resend failed: %q %v", newOTP, err)
	}
	if newOTP != oldOTP {
		if res, _ := s.VerifyChallenge(ctx, id, oldOTP); res.Success {
			t.Fatal("old OTP must stop working after resend")
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
	_, _, _ = s.CreateChallenge(ctx, "user-1")
	res, _ := s.VerifyChallenge(ctx, id1, otp1)
	if res.Success {
		t.Fatal("first challenge should be invalidated by the second login")
	}
}
