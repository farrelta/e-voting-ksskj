package main

import "testing"

// A nil *SMTPMailer stored in an OTPMailer interface is non-nil and panics
// when SendOTP is called. The constructor must return a genuinely nil
// interface when SMTP is disabled, so development falls back to logging OTPs.
func TestNewSMTPMailerReturnsNilInterfaceWhenDisabled(t *testing.T) {
	mailer, err := newSMTPMailer(&Config{})
	if err != nil {
		t.Fatalf("newSMTPMailer: %v", err)
	}
	if mailer != nil {
		t.Fatalf("mailer = %#v, want a nil interface", mailer)
	}
}

func TestNewSMTPMailerReturnsConfiguredMailer(t *testing.T) {
	mailer, err := newSMTPMailer(&Config{
		SMTPHost: "smtp.example.com",
		SMTPPort: "587",
		SMTPFrom: "Voting System <voting@example.com>",
	})
	if err != nil {
		t.Fatalf("newSMTPMailer: %v", err)
	}
	if mailer == nil {
		t.Fatal("configured SMTP should return a mailer")
	}
}
