package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func TestClassifyDeliveryError(t *testing.T) {
	rcpt := func(code int) error {
		return &recipientError{err: fmt.Errorf("set SMTP recipient: %w", &textproto.Error{Code: code, Msg: "x"})}
	}
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"550 no such user", rcpt(550), failRejected},
		{"553 bad mailbox name", rcpt(553), failRejected},
		{"451 try later", rcpt(451), failTemporary},
		{"recipient step, not an SMTP code", &recipientError{err: errors.New("boom")}, failSystem},
		{"login rejected", fmt.Errorf("authenticate with SMTP server: %w", &textproto.Error{Code: 535}), failSystem},
		{"connection refused", errors.New("connect to SMTP server: refused"), failSystem},
	}
	for _, tc := range cases {
		if got := classifyDeliveryError(tc.err); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestDeriveOTPState(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	ago := func(d time.Duration) int64 { return now.Add(-d).Unix() }
	cases := []struct {
		name string
		rec  otpRecord
		want string
	}{
		{"nothing yet", otpRecord{}, otpStateNoActivity},
		{"one code just sent", otpRecord{Sent: 1, LastSent: ago(time.Minute)}, otpStatePending},
		{"exactly 2 sent is not enough", otpRecord{Sent: 2, LastSent: ago(time.Hour)}, otpStatePending},
		{"3 sent, none submitted, 14 min", otpRecord{Sent: 3, LastSent: ago(14 * time.Minute)}, otpStatePending},
		{"3 sent, none submitted, 15 min", otpRecord{Sent: 3, LastSent: ago(15 * time.Minute)}, otpStateCheckEmail},
		{"many sent, one wrong code typed", otpRecord{Sent: 5, Submitted: 1, LastSent: ago(time.Hour)}, otpStatePending},
		{"address refused", otpRecord{FailKind: failRejected, FailAt: ago(time.Minute)}, otpStateDeliveryFailed},
		{"temporary failure only", otpRecord{FailKind: failTemporary, FailAt: ago(time.Minute)}, otpStateNoActivity},
		{"system failure only", otpRecord{FailKind: failSystem, FailAt: ago(time.Minute)}, otpStateNoActivity},
		{"signed in once", otpRecord{Sent: 9, OKAt: ago(time.Hour), LastSent: ago(time.Hour)}, otpStateOK},
		{"signed in, then refused", otpRecord{OKAt: ago(time.Hour), FailKind: failRejected, FailAt: ago(time.Minute)}, otpStateOK},
	}
	for _, tc := range cases {
		if got := deriveOTPState(tc.rec, now); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestStatusesFromRedis(t *testing.T) {
	s, mr := newTestOTP(t)
	ctx := context.Background()
	now := time.Now()

	refused := &recipientError{err: &textproto.Error{Code: 550}}

	// bad: address refused.
	s.NoteSend(ctx, "bad", refused)

	// ghost: three codes sent, never typed one in. Backdate the last send.
	for i := 0; i < 3; i++ {
		s.NoteSend(ctx, "ghost", nil)
	}
	mr.HSet(otpStatusKey("ghost"), "last_sent", fmt.Sprint(now.Add(-20*time.Minute).Unix()))

	// typo: three codes sent but the voter typed a (wrong) code.
	for i := 0; i < 3; i++ {
		s.NoteSend(ctx, "typo", nil)
	}
	s.NoteSubmission(ctx, "typo")
	mr.HSet(otpStatusKey("typo"), "last_sent", fmt.Sprint(now.Add(-20*time.Minute).Unix()))

	// fine: signed in.
	s.NoteSend(ctx, "fine", nil)
	s.NoteSubmission(ctx, "fine")
	s.NoteLoginSuccess(ctx, "fine")

	// recovered: refused, then a later send went through, then still nothing typed.
	s.NoteSend(ctx, "recovered", refused)
	s.NoteSend(ctx, "recovered", nil)

	got, problem := s.Statuses(ctx, []string{"bad", "ghost", "typo", "fine", "recovered", "never"}, now)
	want := map[string]string{
		"bad":       otpStateDeliveryFailed,
		"ghost":     otpStateCheckEmail,
		"typo":      otpStatePending,
		"fine":      otpStateOK,
		"recovered": otpStatePending,
		"never":     otpStateNoActivity,
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s: got %q, want %q", id, got[id], w)
		}
	}
	if problem {
		t.Error("an address refusal must not be reported as a mail system problem")
	}
}

func TestMailSystemProblem(t *testing.T) {
	s, _ := newTestOTP(t)
	ctx := context.Background()
	now := time.Now()

	// A login failure is the mail system's fault, not the voter's.
	s.NoteSend(ctx, "u1", errors.New("authenticate with SMTP server: 535"))
	states, problem := s.Statuses(ctx, []string{"u1"}, now)
	if !problem {
		t.Error("expected a mail system problem")
	}
	if states["u1"] != otpStateNoActivity {
		t.Errorf("voter must not be blamed for a system failure, got %q", states["u1"])
	}

	// Once a send works again the banner goes away.
	time.Sleep(1100 * time.Millisecond)
	s.NoteSend(ctx, "u2", nil)
	if _, problem := s.Statuses(ctx, []string{"u1"}, time.Now()); problem {
		t.Error("problem should clear after a successful send")
	}

	// And it fades by itself after an hour.
	s.NoteSend(ctx, "u1", errors.New("connect to SMTP server: refused"))
	if _, problem := s.Statuses(ctx, nil, time.Now().Add(2*time.Hour)); problem {
		t.Error("an old problem should not be shown")
	}
}

func TestStatusStoresNoSecrets(t *testing.T) {
	s, mr := newTestOTP(t)
	ctx := context.Background()
	s.NoteSend(ctx, "u", &recipientError{err: errors.New("550 mailbox someone@ugm.ac.id unknown, code 123456")})
	s.NoteSend(ctx, "u", nil)
	for _, key := range mr.Keys() {
		if !strings.HasPrefix(key, "otp:status:") && key != mailHealthKey {
			continue
		}
		fields, _ := mr.HKeys(key)
		for _, field := range fields {
			val := mr.HGet(key, field)
			if strings.Contains(val, "@") || strings.Contains(val, "123456") {
				t.Errorf("%s %s holds sensitive text: %q", key, field, val)
			}
		}
	}
	if ttl := mr.TTL(otpStatusKey("u")); ttl <= 0 {
		t.Errorf("status record must expire, ttl=%v", ttl)
	}
}

func TestNilServiceIsSafe(t *testing.T) {
	var s *OTPService
	ctx := context.Background()
	s.NoteSend(ctx, "u", nil)
	s.NoteSubmission(ctx, "u")
	s.NoteLoginSuccess(ctx, "u")
	got, problem := s.Statuses(ctx, []string{"u"}, time.Now())
	if problem || got["u"] != otpStatePending {
		t.Errorf("nil service: got %v problem=%v", got, problem)
	}
}

// A real net/smtp client talking to a server that refuses the recipient must produce an error that classifyDeliveryError calls "rejected".
func TestRealSMTPRefusalIsClassified(t *testing.T) {
	for _, tc := range []struct {
		reply string
		want  string
	}{
		{"550 5.1.1 No such user here", failRejected},
		{"451 4.3.0 Try again later", failTemporary},
	} {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			tp := textproto.NewConn(conn)
			_ = tp.PrintfLine("220 fake ESMTP")
			for {
				line, err := tp.ReadLine()
				if err != nil {
					return
				}
				switch {
				case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
					_ = tp.PrintfLine("250 fake")
				case strings.HasPrefix(line, "MAIL"):
					_ = tp.PrintfLine("250 ok")
				case strings.HasPrefix(line, "RCPT"):
					_ = tp.PrintfLine("%s", tc.reply)
				case strings.HasPrefix(line, "QUIT"):
					_ = tp.PrintfLine("221 bye")
					return
				}
			}
		}()
		client, err := smtp.Dial(ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		if err := client.Mail("from@example.com"); err != nil {
			t.Fatal(err)
		}
		rcptErr := &recipientError{err: fmt.Errorf("set SMTP recipient: %w", client.Rcpt("typo@ugm.ac.id"))}
		if got := classifyDeliveryError(rcptErr); got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.reply, got, tc.want)
		}
		client.Close()
		ln.Close()
	}
}

type statusFakeMailer struct{ err error }

func (m statusFakeMailer) SendOTP(ctx context.Context, recipient, code string) error { return m.err }

// sendOTP is what every sign-in path calls: it must record the outcome.
func TestSendOTPRecordsOutcome(t *testing.T) {
	a := newLimitedApp(t, 0)
	ctx := context.Background()

	a.mailer = statusFakeMailer{err: &recipientError{err: &textproto.Error{Code: 550}}}
	if err := a.sendOTP(ctx, "u1", "typo@ugm.ac.id", "123456"); err == nil {
		t.Fatal("sendOTP must still return the error to its caller")
	}
	states, _ := a.otp.Statuses(ctx, []string{"u1"}, time.Now())
	if states["u1"] != otpStateDeliveryFailed {
		t.Errorf("got %q, want delivery_failed", states["u1"])
	}

	a.mailer = statusFakeMailer{}
	if err := a.sendOTP(ctx, "u1", "typo@ugm.ac.id", "123456"); err != nil {
		t.Fatal(err)
	}
	states, _ = a.otp.Statuses(ctx, []string{"u1"}, time.Now())
	if states["u1"] == otpStateDeliveryFailed {
		t.Error("a later successful send should clear the failure")
	}
}

// Typing in a wrong code proves the voter got an email, so the voter must not be flagged.
func TestWrongCodeCountsAsSubmission(t *testing.T) {
	a := newLimitedApp(t, 0)
	ctx := context.Background()
	const userID = "u1"
	for i := 0; i < 3; i++ {
		a.otp.NoteSend(ctx, userID, nil)
	}
	id, otp, err := a.otp.CreateChallenge(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	wrong := "000000"
	if otp == wrong {
		wrong = "000001"
	}
	raw, _ := json.Marshal(map[string]string{"otp": wrong})
	r := httptest.NewRequest(http.MethodPost, "/api/login/verify-otp", bytes.NewReader(raw))
	r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, &Session{PendingChallengeID: id, PendingUserID: userID}))
	a.handleVerifyOTP(httptest.NewRecorder(), r)

	later := time.Now().Add(time.Hour)
	states, _ := a.otp.Statuses(ctx, []string{userID}, later)
	if states[userID] != otpStatePending {
		t.Errorf("got %q, want pending (a wrong code was typed)", states[userID])
	}
}
