// Package config loads the server configuration from environment variables.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Config holds all runtime settings for the server.
type Config struct {
	Port               string
	DatabaseURL        string
	JWTSecret          string
	CORSAllowedOrigins string
}

// Load reads the configuration from the environment, applying defaults for
// optional values.
func Load() Config {
	return Config{
		Port:               getenv("PORT", "8080"),
		DatabaseURL:        getenv("DATABASE_URL", "pinolrent.db"),
		JWTSecret:          os.Getenv("JWT_SECRET"),
		CORSAllowedOrigins: getenv("CORS_ALLOWED_ORIGINS", "*"),
	}
}

// Validate returns an error when a required setting is missing or malformed.
// Failing fast here prevents the server from starting with insecure defaults.
func (c Config) Validate() error {
	if c.JWTSecret == "" {
		return fmt.Errorf("missing required env vars: %s", "JWT_SECRET")
	}
	if _, err := c.CORSOrigins(); err != nil {
		return err
	}
	return nil
}

// CORSOrigins parses the comma-separated allow-list for cross-origin requests.
// Each entry must be "*" (any origin) or a full origin like
// "https://app.example.com". An empty value disables CORS entirely.
func (c Config) CORSOrigins() ([]string, error) {
	var origins []string
	for _, o := range strings.Split(c.CORSAllowedOrigins, ",") {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		if o != "*" && !validOrigin(o) {
			return nil, fmt.Errorf("invalid CORS_ALLOWED_ORIGINS entry %q: want * or scheme://host[:port]", o)
		}
		origins = append(origins, o)
	}
	return origins, nil
}

func validOrigin(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if u.Host == "" {
		return false
	}
	if u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return true
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
