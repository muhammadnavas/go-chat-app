package config

import "os"

// Config holds application configuration loaded from environment variables.
type Config struct {
	Port        string
	DatabaseURL string
	RedisAddr   string
	JWTSecret   string
}

// Load returns a Config populated from environment variables with sensible defaults.
func Load() Config {
	return Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/gochat?sslmode=disable"),
		RedisAddr:   getEnv("REDIS_ADDR", "localhost:6379"),
		JWTSecret:   getEnv("JWT_SECRET", "dev-secret-change-in-production"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

