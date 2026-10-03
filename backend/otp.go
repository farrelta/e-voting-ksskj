package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	otpTTL            = 5 * time.Minute
	otpMaxAttempts    = 5
	otpResendCooldown = 30 * time.Second
)

const (
	reasonExpiredOrInvalid = "expired_or_invalid"
	reasonTooManyAttempts  = "too_many_attempts"
	reasonInvalidOTP       = "invalid_otp"
)

type OTPService struct {
	rdb    *redis.Client
	secret []byte
}

func newOTPService(rdb *redis.Client, secret string) *OTPService {
	return &OTPService{rdb: rdb, secret: []byte(secret)}
}

type otpChallenge struct {
	UserID    string `json:"userId"`
	OTPHash   string `json:"otpHash"`
	CreatedAt int64  `json:"createdAt"`
}

type VerifyResult struct {
	Success bool
	Reason  string
	UserID  string
}

func loginKey(id string) string    { return "otp:login:" + id }
func attemptsKey(id string) string { return "otp:attempts:" + id }
func resendKey(id string) string   { return "otp:resend:" + id }
func activeKey(uid string) string  { return "otp:active:" + uid }

func generateOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func (s *OTPService) hash(otp string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(otp))
	return hex.EncodeToString(mac.Sum(nil))
}

func safeCompare(a, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}

func (s *OTPService) storeChallenge(ctx context.Context, challengeID string, c otpChallenge) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, loginKey(challengeID), data, otpTTL).Err()
}

func (s *OTPService) loadChallenge(ctx context.Context, challengeID string) (*otpChallenge, error) {
	raw, err := s.rdb.Get(ctx, loginKey(challengeID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c otpChallenge
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, nil
	}
	return &c, nil
}

func (s *OTPService) CreateChallenge(ctx context.Context, userID string) (challengeID, otp string, err error) {
	prev, err := s.rdb.Get(ctx, activeKey(userID)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return "", "", err
	}
	if prev != "" {
		if err := s.rdb.Del(ctx, loginKey(prev), attemptsKey(prev), resendKey(prev)).Err(); err != nil {
			return "", "", err
		}
	}

	challengeID = uuid.NewString()
	otp, err = generateOTP()
	if err != nil {
		return "", "", err
	}

	err = s.storeChallenge(ctx, challengeID, otpChallenge{
		UserID:    userID,
		OTPHash:   s.hash(otp),
		CreatedAt: time.Now().UnixMilli(),
	})
	if err != nil {
		return "", "", err
	}
	if err := s.rdb.Set(ctx, activeKey(userID), challengeID, otpTTL).Err(); err != nil {
		return "", "", err
	}
	return challengeID, otp, nil
}

func (s *OTPService) VerifyChallenge(ctx context.Context, challengeID, submitted string) (VerifyResult, error) {
	challenge, err := s.loadChallenge(ctx, challengeID)
	if err != nil {
		return VerifyResult{}, err
	}
	if challenge == nil {
		return VerifyResult{Reason: reasonExpiredOrInvalid}, nil
	}

	attempts, err := s.rdb.Incr(ctx, attemptsKey(challengeID)).Result()
	if err != nil {
		return VerifyResult{}, err
	}
	if attempts == 1 {
		if err := s.rdb.Expire(ctx, attemptsKey(challengeID), otpTTL).Err(); err != nil {
			return VerifyResult{}, err
		}
	}

	if attempts >= otpMaxAttempts {
		if err := s.rdb.Del(ctx, loginKey(challengeID), attemptsKey(challengeID), activeKey(challenge.UserID)).Err(); err != nil {
			return VerifyResult{}, err
		}
		return VerifyResult{Reason: reasonTooManyAttempts}, nil
	}

	if !safeCompare(s.hash(submitted), challenge.OTPHash) {
		return VerifyResult{Reason: reasonInvalidOTP}, nil
	}

	if err := s.rdb.Del(ctx, loginKey(challengeID), attemptsKey(challengeID), activeKey(challenge.UserID)).Err(); err != nil {
		return VerifyResult{}, err
	}
	return VerifyResult{Success: true, UserID: challenge.UserID}, nil
}

func (s *OTPService) CanResend(ctx context.Context, challengeID string) (bool, error) {
	return s.rdb.SetNX(ctx, resendKey(challengeID), "1", otpResendCooldown).Result()
}

func (s *OTPService) Resend(ctx context.Context, challengeID string) (string, error) {
	challenge, err := s.loadChallenge(ctx, challengeID)
	if err != nil {
		return "", err
	}
	if challenge == nil {
		return "", nil
	}

	otp, err := generateOTP()
	if err != nil {
		return "", err
	}
	err = s.storeChallenge(ctx, challengeID, otpChallenge{
		UserID:    challenge.UserID,
		OTPHash:   s.hash(otp),
		CreatedAt: time.Now().UnixMilli(),
	})
	if err != nil {
		return "", err
	}

	if err := s.rdb.Del(ctx, attemptsKey(challengeID), activeKey(challenge.UserID)).Err(); err != nil {
		return "", err
	}
	return otp, nil
}
