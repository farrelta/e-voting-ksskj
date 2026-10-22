package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

type OTPMailer interface {
	SendOTP(ctx context.Context, recipient, code string) error
}

type SMTPMailer struct {
	host     string
	port     string
	username string
	password string
	from     mail.Address
}

func newSMTPMailer(cfg *Config) (*SMTPMailer, error) {
	if cfg.SMTPHost == "" {
		return nil, nil
	}
	from, err := mail.ParseAddress(cfg.SMTPFrom)
	if err != nil || strings.ContainsAny(from.Address, "\r\n") {
		return nil, fmt.Errorf("SMTP_FROM must be a valid email address")
	}
	return &SMTPMailer{
		host:     cfg.SMTPHost,
		port:     cfg.SMTPPort,
		username: cfg.SMTPUser,
		password: cfg.SMTPPassword,
		from:     *from,
	}, nil
}

func (m *SMTPMailer) SendOTP(ctx context.Context, recipient, code string) error {
	to, err := mail.ParseAddress(recipient)
	if err != nil || strings.ContainsAny(to.Address, "\r\n") {
		return fmt.Errorf("invalid OTP recipient")
	}

	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(m.host, m.port))
	if err != nil {
		return fmt.Errorf("connect to SMTP server: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	}

	client, err := smtp.NewClient(conn, m.host)
	if err != nil {
		return fmt.Errorf("start SMTP session: %w", err)
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); !ok {
		return fmt.Errorf("SMTP server does not support STARTTLS")
	}
	if err := client.StartTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: m.host}); err != nil {
		return fmt.Errorf("start SMTP TLS: %w", err)
	}

	if m.username != "" {
		if ok, _ := client.Extension("AUTH"); !ok {
			return fmt.Errorf("SMTP server does not support authentication")
		}
		if err := client.Auth(smtp.PlainAuth("", m.username, m.password, m.host)); err != nil {
			return fmt.Errorf("authenticate with SMTP server: %w", err)
		}
	}

	if err := client.Mail(m.from.Address); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(to.Address); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP message: %w", err)
	}
	_, writeErr := fmt.Fprintf(writer,
		"From: %s\r\nTo: %s\r\nSubject: Your e-voting sign-in code\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nYour one-time sign-in code is %s. It expires in 5 minutes. If you did not request this code, you can ignore this email.\r\n",
		m.from.String(), to.String(), code,
	)
	closeErr := writer.Close()
	if writeErr != nil {
		return fmt.Errorf("write SMTP message: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("send SMTP message: %w", closeErr)
	}
	return client.Quit()
}
