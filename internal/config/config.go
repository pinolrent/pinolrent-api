// Package config loads the server configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strings"
)

// Config holds all runtime settings for the server.
type Config struct {
	Port          string
	DatabaseURL   string
	JWTSecret     string
	AdminEmail    string
	AdminPassword string
}

// Load reads the configuration from the environment, applying defaults for
// optional values.
func Load() Config {
	return Config{
		Port:          getenv("PORT", "8080"),
		DatabaseURL:   getenv("DATABASE_URL", "pinolrent.db"),
		JWTSecret:     os.Getenv("JWT_SECRET"),
		AdminEmail:    getenv("ADMIN_EMAIL", "admin@pinolrent.com"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
	}
}

// Validate returns an error when a required setting is missing. Failing fast
// here prevents the server from starting with insecure defaults.
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
