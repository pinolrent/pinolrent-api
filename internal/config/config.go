// Package config loads the server configuration from environment variables.
package config

import (
	"fmt"
	"os"
)

// Config holds all runtime settings for the server.
type Config struct {
	Port        string
	DatabaseURL string
	JWTSecret   string
}

// Load reads the configuration from the environment, applying defaults for
// optional values.
func Load() Config {
	return Config{
		Port:        getenv("PORT", "8080"),
		DatabaseURL: getenv("DATABASE_URL", "pinolrent.db"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
	}
}

// Validate returns an error when a required setting is missing. Failing fast
// here prevents the server from starting with insecure defaults.
func (c Config) Validate() error {
	if c.JWTSecret == "" {
		return fmt.Errorf("missing required env vars: %s", "JWT_SECRET")
	}
	return nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
