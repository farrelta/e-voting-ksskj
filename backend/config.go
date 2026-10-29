package main

import (
	"errors"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port         string
	DBHost       string
	DBPort       string
	DBName       string
	DBUser       string
	DBPassword   string
	RedisURL     string
	OTPSecret    string
	SMTPHost     string
	SMTPPort     string
	SMTPUser     string
	SMTPPassword string
	SMTPFrom     string
	Env          string
	FrontendDir  string
	// AllowedEmailDomain restricts who can be added to an election's participant
	// list. Addresses on this domain or any of its subdomains are accepted.
	AllowedEmailDomain string
}

func (c *Config) IsDev() bool        { return c.Env == "development" }
func (c *Config) IsProduction() bool { return c.Env == "production" }

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func loadConfig() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		Port:         getenv("PORT", "3000"),
		DBHost:       getenv("DB_HOST", "localhost"),
		DBPort:       getenv("DB_PORT", "5432"),
		DBName:       os.Getenv("DB_NAME"),
		DBUser:       os.Getenv("DB_USER"),
		DBPassword:   os.Getenv("DB_PASSWORD"),
		RedisURL:     getenv("REDIS_URL", "redis://127.0.0.1:6379"),
		OTPSecret:    os.Getenv("OTP_SECRET"),
		SMTPHost:     os.Getenv("SMTP_HOST"),
		SMTPPort:     getenv("SMTP_PORT", "587"),
		SMTPUser:     os.Getenv("SMTP_USER"),
		SMTPPassword: os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:     os.Getenv("SMTP_FROM"),
		Env:          getenv("APP_ENV", "production"),
		FrontendDir:  getenv("FRONTEND_DIR", "../frontend"),
		AllowedEmailDomain: strings.ToLower(strings.TrimPrefix(
			strings.TrimSpace(getenv("ALLOWED_EMAIL_DOMAIN", "ugm.ac.id")), "@")),
	}

	if cfg.DBName == "" || cfg.DBUser == "" {
		return nil, errors.New("DB_NAME and DB_USER must be set")
	}
	if len(cfg.OTPSecret) < 16 {
		return nil, errors.New("OTP_SECRET must be set (at least 16 characters)")
	}
	if (cfg.SMTPHost == "") != (cfg.SMTPFrom == "") {
		return nil, errors.New("SMTP_HOST and SMTP_FROM must be configured together")
	}
	if (cfg.SMTPUser == "") != (cfg.SMTPPassword == "") {
		return nil, errors.New("SMTP_USER and SMTP_PASSWORD must be configured together")
	}
	if cfg.SMTPHost == "" && !cfg.IsDev() {
		return nil, errors.New("SMTP_HOST and SMTP_FROM are required outside development")
	}
	return cfg, nil
}
