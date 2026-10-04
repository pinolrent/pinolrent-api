// Package config loads the server configuration from environment variables.
package config

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
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
	// UploadMaxTotalMB caps the total size of UploadDir in MB; 0 disables
	// the cap. Bounds disk usage against a client uploading at the rate
	// limit forever.
	UploadMaxTotalMB  int    `env:"UPLOAD_MAX_TOTAL_MB" envDefault:"1024"`
	TrustedProxyCIDRs string `env:"TRUSTED_PROXY_CIDRS"`
	// AdminEmails is the allow-list of accounts that hold the admin role.
	// Empty (the default) means the deployment has no administrator, which
	// is a valid state: every route still works, only /admin is unreachable.
	AdminEmails string `env:"ADMIN_EMAILS"`
}

// Load reads the configuration from the environment, applying defaults for
// optional values. Malformed values surface in Validate where that matters.
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
	if _, err := c.AdminEmailList(); err != nil {
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

// AdminEmailList parses the comma-separated allow-list of administrator
// accounts. Entries are lower-cased so the lookup matches the email the
// account registered with, and blanks are dropped so a trailing comma is not
// an error. A malformed entry fails the whole list: a typo in an
// administrator address must not silently leave the deployment unadministered.
func (c Config) AdminEmailList() ([]string, error) {
	var emails []string
	for _, entry := range strings.Split(c.AdminEmails, ",") {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		// The shape must match what registration accepts: an entry that
		// cannot be registered could never match an account, leaving the
		// deployment unadministered with no error to explain it.
		if len(entry) > MaxEmailLen || !ValidEmail(entry) {
			return nil, fmt.Errorf("invalid ADMIN_EMAILS entry %q: want a plain address like admin@example.com", entry)
		}
		emails = append(emails, entry)
	}
	return emails, nil
}

// emailRe is the single email rule for the platform: local and domain labels
// cannot start or end with a dot or hyphen, no consecutive dots, and the TLD
// has at least 2 letters. It is pragmatic, not fully RFC 5322, and it governs
// both account registration and the ADMIN_EMAILS allow-list so an allow-listed
// address can always be registered afterwards.
var emailRe = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+/=?^_` + "`" + `{|}~-]+(?:\.[A-Za-z0-9!#$%&'*+/=?^_` + "`" + `{|}~-]+)*@[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)*\.[A-Za-z]{2,}$`)

// ValidEmail reports whether s has an acceptable account address shape.
// Length is checked separately by each caller so empty and overlong inputs
// keep their own messages.
func ValidEmail(s string) bool {
	return emailRe.MatchString(s)
}

// MaxEmailLen bounds an account address at the width registration and the
// allow-list both accept, so an allow-listed account can always be registered
// afterwards. It lives here so the two callers cannot drift apart.
const MaxEmailLen = 254

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
