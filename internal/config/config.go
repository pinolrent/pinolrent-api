package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Port          string
	DatabaseURL   string
	JWTSecret     string
	AdminEmail    string
	AdminPassword string
}

func Load() Config {
	return Config{
		Port:          getenv("PORT", "8080"),
		DatabaseURL:   getenv("DATABASE_URL", "pinolrent.db"),
		JWTSecret:     os.Getenv("JWT_SECRET"),
		AdminEmail:    getenv("ADMIN_EMAIL", "admin@pinolrent.com"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
	}
}

func (c Config) Validate() error {
	var missing []string
	if c.JWTSecret == "" {
		missing = append(missing, "JWT_SECRET")
	}
	if c.AdminPassword == "" {
		missing = append(missing, "ADMIN_PASSWORD")
	}
	if c.AdminEmail == "" {
		missing = append(missing, "ADMIN_EMAIL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}
	return nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}