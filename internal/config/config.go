// Package config loads the server configuration from environment variables.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
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
	// PhoneCountryPrefix is the E.164 country code that completes a local
	// phone number, and PhoneNationalLen its digit count: together they say
	// what a bare number means. Defaults describe Nicaragua (+505, 8 digits);
	// a Chilean deployment reconfigures both.
	PhoneCountryPrefix string `env:"PHONE_COUNTRY_PREFIX" envDefault:"505"`
	PhoneNationalLen   int    `env:"PHONE_NATIONAL_LEN" envDefault:"8"`
	// BusinessTimezone is the zone that decides what day it is for the API:
	// the past-date check and the future-reservation guard count days in it.
	// In a Managua deployment users book in Managua time, while the UTC clock
	// rolls the day over at 18:00 local.
	BusinessTimezone string `env:"BUSINESS_TIMEZONE" envDefault:"America/Managua"`
}

// Load reads the configuration from the environment, applying defaults for
// optional values. An empty variable falls back to its default instead of
// clearing it. Malformed values surface in Validate where that matters.
func Load() Config {
	return Config{
		Port:               getenv("PORT", "8080"),
		DatabaseURL:        getenv("DATABASE_URL", "pinolrent.db"),
		JWTSecret:          getenv("JWT_SECRET", ""),
		CORSAllowedOrigins: getenv("CORS_ALLOWED_ORIGINS", "*"),
		Env:                getenv("ENV", "dev"),
		UploadDir:          getenv("UPLOAD_DIR", "uploads"),
		UploadMaxTotalMB:   getenvInt("UPLOAD_MAX_TOTAL_MB", 1024),
		TrustedProxyCIDRs:  getenv("TRUSTED_PROXY_CIDRS", ""),
		AdminEmails:        getenv("ADMIN_EMAILS", ""),
		PhoneCountryPrefix: getenv("PHONE_COUNTRY_PREFIX", "505"),
		PhoneNationalLen:   getenvInt("PHONE_NATIONAL_LEN", 8),
		BusinessTimezone:   getenv("BUSINESS_TIMEZONE", "America/Managua"),
	}
}

// getenv returns the variable's value, or def when it is unset or empty.
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// getenvInt is getenv for numeric variables. A value that does not parse
// yields 0 — a parse error never reaches Validate, same as before this was
// hand-written, so 0 (unlimited) is what a typo leaves behind.
func getenvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
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
	if err := c.validatePhoneCountry(); err != nil {
		return err
	}
	return nil
}

// validatePhoneCountry checks the pair the normalizer relies on: a digit-only
// country code that does not start with 0 and a national length that fits.
// Both bounds together keep the sum inside E.164 (3 + 12 <= 15 digits). A
// typo here would silently corrupt every local number, so it fails startup.
func (c Config) validatePhoneCountry() error {
	// An unset pair (a Config built by hand, never through Load) has nothing
	// to validate; Load always fills both defaults.
	if c.PhoneCountryPrefix == "" && c.PhoneNationalLen == 0 {
		return nil
	}
	p := c.PhoneCountryPrefix
	if p == "" || len(p) > 3 {
		return fmt.Errorf("invalid PHONE_COUNTRY_PREFIX %q: want 1-3 digits like 505", p)
	}
	for _, r := range p {
		if r < '0' || r > '9' {
			return fmt.Errorf("invalid PHONE_COUNTRY_PREFIX %q: want 1-3 digits like 505", p)
		}
	}
	if p[0] == '0' {
		return fmt.Errorf("invalid PHONE_COUNTRY_PREFIX %q: country codes do not start with 0", p)
	}
	if c.PhoneNationalLen < 4 || c.PhoneNationalLen > 12 {
		return fmt.Errorf("invalid PHONE_NATIONAL_LEN %d: want 4-12 digits", c.PhoneNationalLen)
	}
	if _, err := c.BusinessLocation(); err != nil {
		return err
	}
	return nil
}

// BusinessLocation loads the time zone whose calendar day the API counts.
// Zone names come from the embedded time/tzdata (see cmd/api), so an unknown
// name is a typo that must fail startup rather than silently fall back to UTC.
func (c Config) BusinessLocation() (*time.Location, error) {
	loc, err := time.LoadLocation(c.BusinessTimezone)
	if err != nil {
		return nil, fmt.Errorf("invalid BUSINESS_TIMEZONE %q: want an IANA name like America/Managua", c.BusinessTimezone)
	}
	return loc, nil
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
// "https://app.example.com". Empty entries are skipped; Load never leaves the
// field empty (an empty variable falls back to the "*" default).
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
