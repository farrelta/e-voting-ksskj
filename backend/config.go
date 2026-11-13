package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"

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
	MasterKey    []byte
	SMTPHost     string
	SMTPPort     string
	SMTPUser     string
	SMTPPassword string
	SMTPFrom     string
	Env          string
	FrontendDir  string
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

	masterKeyRaw := os.Getenv("MASTER_KEY")
	if masterKeyRaw == "" {
		if cfg.IsDev() {
			h := sha256.Sum256([]byte("dev-master-key:" + cfg.OTPSecret))
			cfg.MasterKey = h[:]
		} else {
			return nil, errors.New("MASTER_KEY must be set outside development")
		}
	} else {
		if len(masterKeyRaw) == 64 {
			if decoded, err := hex.DecodeString(masterKeyRaw); err == nil && len(decoded) == 32 {
				cfg.MasterKey = decoded
			}
		}
		if len(cfg.MasterKey) == 0 && len(masterKeyRaw) == 44 {
			if decoded, err := base64.StdEncoding.DecodeString(masterKeyRaw); err == nil && len(decoded) == 32 {
				cfg.MasterKey = decoded
			}
		}
		if len(cfg.MasterKey) == 0 {
			h := sha256.Sum256([]byte(masterKeyRaw))
			cfg.MasterKey = h[:]
		}
	}

	return cfg, nil
}
