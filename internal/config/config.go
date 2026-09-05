// Package config loads the server configuration from environment variables.
package config

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/caarlos0/env/v11"
)

// Config holds all runtime settings for the server.
type Config struct {
	Port               string `env:"PORT" envDefault:"8080"`
	DatabaseURL        string `env:"DATABASE_URL" envDefault:"pinolrent.db"`
	JWTSecret          string `env:"JWT_SECRET"`
	CORSAllowedOrigins string `env:"CORS_ALLOWED_ORIGINS" envDefault:"*"`
	Env                string `env:"ENV" envDefault:"dev"`
}

// Load reads the configuration from the environment, applying defaults for
// optional values. All fields are strings, so parsing cannot fail here;
// semantic validation happens in Validate.
func Load() Config {
	cfg := Config{}
	_ = env.Parse(&cfg)
	return cfg
}

// Validate returns an error when a required setting is missing or malformed.
// Failing fast here prevents the server from starting with insecure defaults.
func (c Config) Validate() error {
	if c.JWTSecret == "" {
		return fmt.Errorf("missing required env vars: %s", "JWT_SECRET")
	}
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("JWT_SECRET must be at least 32 bytes (got %d); generate one with `openssl rand -base64 32`", len(c.JWTSecret))
	}
	if _, err := parsePort(c.Port); err != nil {
		return err
	}
	if _, err := c.CORSOrigins(); err != nil {
		return err
	}
	if (c.Env == "prod" || c.Env == "production") && c.CORSAllowedOrigins == "*" {
		return fmt.Errorf("CORS_ALLOWED_ORIGINS=* not allowed when ENV=%s", c.Env)
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
	s = strings.TrimSuffix(s, "/")
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

func parsePort(s string) (int, error) {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("PORT must be a number (got %q)", s)
		}
		n = n*10 + int(ch-'0')
	}
	if n < 1 || n > 65535 {
		return 0, fmt.Errorf("PORT out of range 1-65535 (got %q)", s)
	}
	return n, nil
}
