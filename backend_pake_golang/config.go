package main

import (
	"errors"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port        string
	DBHost      string
	DBPort      string
	DBName      string
	DBUser      string
	DBPassword  string
	RedisURL    string
	OTPSecret   string
	Env         string
	FrontendDir string
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
		Port:        getenv("PORT", "3000"),
		DBHost:      getenv("DB_HOST", "localhost"),
		DBPort:      getenv("DB_PORT", "5432"),
		DBName:      os.Getenv("DB_NAME"),
		DBUser:      os.Getenv("DB_USER"),
		DBPassword:  os.Getenv("DB_PASSWORD"),
		RedisURL:    getenv("REDIS_URL", "redis://127.0.0.1:6379"),
		OTPSecret:   os.Getenv("OTP_SECRET"),
		Env:         getenv("APP_ENV", "production"),
		FrontendDir: getenv("FRONTEND_DIR", "../frontend"),
	}

	if cfg.DBName == "" || cfg.DBUser == "" {
		return nil, errors.New("DB_NAME and DB_USER must be set")
	}
	if len(cfg.OTPSecret) < 16 {
		return nil, errors.New("OTP_SECRET must be set (at least 16 characters)")
	}
	return cfg, nil
}
