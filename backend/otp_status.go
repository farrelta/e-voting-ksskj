package main

import (
	"context"
	"errors"
	"log"
	"net/textproto"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	otpStatusTTL      = 30 * 24 * time.Hour
	checkEmailMinSent = 3 // check for: more than 2" codes sent, none submitted, for at least 15 minutes.
	checkEmailWait    = 15 * time.Minute
	mailProblemWindow = time.Hour
)

const (
	otpStateOK             = "ok"
	otpStateDeliveryFailed = "delivery_failed"
	otpStateCheckEmail     = "check_email"
	otpStateNoActivity     = "no_activity"
	otpStatePending        = "pending"
)

const (
	failRejected  = "rejected"  // code 5xx at the recipient step: the address was refused
	failTemporary = "temporary" // code 4xx at the recipient step: try again later
	failSystem    = "system"    // connection, TLS or login problem: not the voter's fault
)

const mailHealthKey = "otp:mail:health"

func otpStatusKey(userID string) string { return "otp:status:" + userID }

// recipientError marks an SMTP failure that happened at the recipient step.
type recipientError struct{ err error }

func (e *recipientError) Error() string { return e.err.Error() }
func (e *recipientError) Unwrap() error { return e.err }

// classifyDeliveryError decides what a failed send says about the voter.
func classifyDeliveryError(err error) string {
	var re *recipientError
	if !errors.As(err, &re) {
		return failSystem
	}
	var te *textproto.Error
	if errors.As(re.err, &te) {
		switch {
		case te.Code >= 500:
			return failRejected
		case te.Code >= 400:
			return failTemporary
		}
	}
	return failSystem
}

type otpRecord struct {
	Sent      int64
	Submitted int64
	LastSent  int64
	OKAt      int64
	FailKind  string
	FailAt    int64
}

func deriveOTPState(rec otpRecord, now time.Time) string {
	if rec.OKAt > 0 {
		return otpStateOK
	}
	if rec.FailKind == failRejected && rec.FailAt > 0 {
		return otpStateDeliveryFailed
	}
	if rec.Sent == 0 {
		return otpStateNoActivity
	}
	if rec.Sent >= checkEmailMinSent && rec.Submitted == 0 &&
		now.Sub(time.Unix(rec.LastSent, 0)) >= checkEmailWait {
		return otpStateCheckEmail
	}
	return otpStatePending
}

func parseOTPRecord(m map[string]string) otpRecord {
	num := func(k string) int64 {
		n, _ := strconv.ParseInt(m[k], 10, 64)
		return n
	}
	return otpRecord{
		Sent:      num("sent"),
		Submitted: num("submitted"),
		LastSent:  num("last_sent"),
		OKAt:      num("ok_at"),
		FailKind:  m["fail_kind"],
		FailAt:    num("fail_at"),
	}
}

func (s *OTPService) statusEnabled() bool { return s != nil && s.rdb != nil }

// NoteSend records the result of one attempt to email a code.
func (s *OTPService) NoteSend(ctx context.Context, userID string, sendErr error) {
	if !s.statusEnabled() || userID == "" {
		return
	}
	now := time.Now().Unix()
	key := otpStatusKey(userID)
	pipe := s.rdb.Pipeline()
	if sendErr == nil {
		pipe.HIncrBy(ctx, key, "sent", 1)
		pipe.HSet(ctx, key, "last_sent", now)
		pipe.HDel(ctx, key, "fail_kind", "fail_at")
		pipe.HSet(ctx, mailHealthKey, "last_ok", now)
	} else {
		kind := classifyDeliveryError(sendErr)
		pipe.HSet(ctx, key, "fail_kind", kind, "fail_at", now)
		if kind == failSystem {
			pipe.HSet(ctx, mailHealthKey, "last_system_fail", now)
		}
	}
	pipe.Expire(ctx, key, otpStatusTTL)
	pipe.Expire(ctx, mailHealthKey, otpStatusTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("record otp send status: %v", err)
	}
}

// NoteSubmission records that the voter typed a code in, right or wrong. Someone who submits a wrong code did receive an email, so it is not a delivery problem.
func (s *OTPService) NoteSubmission(ctx context.Context, userID string) {
	if !s.statusEnabled() || userID == "" {
		return
	}
	key := otpStatusKey(userID)
	pipe := s.rdb.Pipeline()
	pipe.HIncrBy(ctx, key, "submitted", 1)
	pipe.Expire(ctx, key, otpStatusTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("record otp submission: %v", err)
	}
}

// NoteLoginSuccess records a completed sign-in: the address works.
func (s *OTPService) NoteLoginSuccess(ctx context.Context, userID string) {
	if !s.statusEnabled() || userID == "" {
		return
	}
	key := otpStatusKey(userID)
	pipe := s.rdb.Pipeline()
	pipe.HSet(ctx, key, "ok_at", time.Now().Unix())
	pipe.Expire(ctx, key, otpStatusTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("record otp login: %v", err)
	}
}

// Statuses returns the state for each user ID, and whether the mail system itself seems to be failing (a banner for the whole page, not a voter).
func (s *OTPService) Statuses(ctx context.Context, userIDs []string, now time.Time) (map[string]string, bool) {
	out := make(map[string]string, len(userIDs))
	if !s.statusEnabled() {
		for _, id := range userIDs {
			out[id] = otpStatePending
		}
		return out, false
	}

	pipe := s.rdb.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(userIDs))
	for i, id := range userIDs {
		cmds[i] = pipe.HGetAll(ctx, otpStatusKey(id))
	}
	health := pipe.HGetAll(ctx, mailHealthKey)
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		log.Printf("read otp status: %v", err)
		for _, id := range userIDs {
			out[id] = otpStatePending
		}
		return out, false
	}

	for i, id := range userIDs {
		out[id] = deriveOTPState(parseOTPRecord(cmds[i].Val()), now)
	}

	h := health.Val()
	lastOK, _ := strconv.ParseInt(h["last_ok"], 10, 64)
	lastFail, _ := strconv.ParseInt(h["last_system_fail"], 10, 64)
	problem := lastFail > 0 && lastFail >= lastOK &&
		now.Sub(time.Unix(lastFail, 0)) < mailProblemWindow
	return out, problem
}

// sendOTP emails a code and records how it went.
func (a *App) sendOTP(ctx context.Context, userID, recipient, code string) error {
	err := a.deliverOTP(ctx, recipient, code)
	a.otp.NoteSend(ctx, userID, err)
	return err
}
