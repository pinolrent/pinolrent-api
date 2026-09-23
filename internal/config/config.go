// Package config loads the server configuration from environment variables.
package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
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
	UploadDir          string `env:"UPLOAD_DIR" envDefault:"uploads"`
	TrustedProxyCIDRs  string `env:"TRUSTED_PROXY_CIDRS"`
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
	if uniqueBytes(c.JWTSecret) < 16 {
		return fmt.Errorf("JWT_SECRET has too little entropy (only %d unique bytes, want >= 16); generate one with `openssl rand -base64 32`", uniqueBytes(c.JWTSecret))
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
	if strings.TrimSpace(c.UploadDir) == "" {
		return fmt.Errorf("UPLOAD_DIR must not be empty")
	}
	if c.UploadMaxTotalMB < 0 {
		return fmt.Errorf("UPLOAD_MAX_TOTAL_MB must be >= 0 (got %d); 0 disables the quota", c.UploadMaxTotalMB)
	}
	if _, err := c.TrustedProxies(); err != nil {
		return err
	}
	return nil
}

// TrustedProxies parses the comma-separated list of networks whose forwarding
// headers (X-Forwarded-For/X-Real-IP) the server believes. Loopback is always
// trusted, so the empty default keeps the single-host behaviour: only a proxy
// running on the same machine can set the client IP.
func (c Config) TrustedProxies() ([]*net.IPNet, error) {
	var nets []*net.IPNet
	for _, entry := range strings.Split(c.TrustedProxyCIDRs, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid TRUSTED_PROXY_CIDRS entry %q: want a CIDR like 172.18.0.0/16", entry)
		}
		nets = append(nets, ipNet)
	}
	return nets, nil
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
		if o != "*" {
			o = strings.TrimSuffix(o, "/")
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

// uniqueBytes counts distinct byte values in s. A 32-byte secret made of a
// single repeated character passes a length check but has no entropy, so
// require a minimum spread of distinct bytes.
func uniqueBytes(s string) int {
	var seen [256]bool
	n := 0
	for i := 0; i < len(s); i++ {
		if !seen[s[i]] {
			seen[s[i]] = true
			n++
		}
	}
	return n
}

func parsePort(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("PORT must be a number (got %q)", s)
	}
	if n < 1 || n > 65535 {
		return 0, fmt.Errorf("PORT out of range 1-65535 (got %q)", s)
	}
	return n, nil
}
