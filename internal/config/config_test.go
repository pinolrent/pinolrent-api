package config

import (
	"net"
	"strings"
	"testing"
)

//nolint:gosec // test-only JWT secret, never used in production
const testJWTSecret = "test-secret-32-bytes-minimum-okay"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("JWT_SECRET", testJWTSecret)
	cfg := Load()
	if cfg.Port != "8080" || cfg.DatabaseURL != "pinolrent.db" || cfg.CORSAllowedOrigins != "*" || cfg.UploadDir != "uploads" {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("PORT", "9999")
	t.Setenv("DATABASE_URL", "custom.db")
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
	t.Setenv("TRUSTED_PROXY_CIDRS", "172.18.0.0/16")
	cfg := Load()
	if cfg.Port != "9999" || cfg.DatabaseURL != "custom.db" || cfg.CORSAllowedOrigins != "https://app.example.com" {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
	if cfg.TrustedProxyCIDRs != "172.18.0.0/16" {
		t.Fatalf("TRUSTED_PROXY_CIDRS not applied: %+v", cfg)
	}
}

func TestTrustedProxies(t *testing.T) {
	cfg := Config{TrustedProxyCIDRs: "172.18.0.0/16, 10.0.0.0/8"}
	nets, err := cfg.TrustedProxies()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nets) != 2 {
		t.Fatalf("got %d networks, want 2", len(nets))
	}
	if !nets[0].Contains(net.ParseIP("172.18.5.5")) {
		t.Fatalf("first network should contain 172.18.5.5: %v", nets[0])
	}

	// Unset means no proxy is trusted beyond loopback.
	nets, err = Config{}.TrustedProxies()
	if err != nil {
		t.Fatalf("unexpected error for empty value: %v", err)
	}
	if len(nets) != 0 {
		t.Fatalf("empty value should yield no networks, got %v", nets)
	}
}

func TestValidateBadTrustedProxy(t *testing.T) {
	cfg := Config{
		Port:              "8080",
		DatabaseURL:       "x.db",
		JWTSecret:         testJWTSecret,
		UploadDir:         "uploads",
		TrustedProxyCIDRs: "10.0.0.1", // a bare IP is not a CIDR
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for a bare IP")
	}
	if !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
		t.Fatalf("error %q should mention TRUSTED_PROXY_CIDRS", err.Error())
	}
}

func TestValidateMissing(t *testing.T) {
	cfg := Config{Port: "8080", DatabaseURL: "x.db"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing vars")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("error %q should mention JWT_SECRET", err.Error())
	}
}

func TestValidateShortSecret(t *testing.T) {
	cfg := Config{JWTSecret: "too-short"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for short JWT_SECRET")
	}
	if !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("error %q should mention 32 bytes", err.Error())
	}
}

func TestValidateLowEntropySecret(t *testing.T) {
	cfg := Config{
		Port:               "8080",
		DatabaseURL:        "x.db",
		JWTSecret:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		CORSAllowedOrigins: "*",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for low-entropy JWT_SECRET")
	}
}

func TestValidateOK(t *testing.T) {
	cfg := Config{Port: "8080", DatabaseURL: "x.db", JWTSecret: testJWTSecret, UploadDir: "uploads"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEmptyUploadDir(t *testing.T) {
	cfg := Config{Port: "8080", DatabaseURL: "x.db", JWTSecret: testJWTSecret, UploadDir: "  "}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for empty UPLOAD_DIR")
	}
}

func TestValidateBadCORS(t *testing.T) {
	cfg := Config{JWTSecret: testJWTSecret, CORSAllowedOrigins: "https://app.example.com/path"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for malformed CORS origin")
	}
}

func TestValidateBadPort(t *testing.T) {
	for _, tc := range []string{"abc", "0", "99999", "-1"} {
		cfg := Config{JWTSecret: testJWTSecret, Port: tc}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected error for PORT %q", tc)
		}
	}
}

func TestValidateCORSStarInProd(t *testing.T) {
	cfg := Config{JWTSecret: testJWTSecret, CORSAllowedOrigins: "*", Env: "prod"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for CORS=* in prod")
	}
	cfg.Env = "production"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for CORS=* in production")
	}
}

func TestValidOriginTrailingSlash(t *testing.T) {
	cfg := Config{CORSAllowedOrigins: "https://app.example.com/"}
	if _, err := cfg.CORSOrigins(); err != nil {
		t.Fatalf("trailing slash should be accepted: %v", err)
	}
}

func TestCORSOrigins(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    []string
		wantErr bool
	}{
		{"wildcard", "*", []string{"*"}, false},
		{"explicit", "https://app.example.com, http://localhost:5173 ", []string{"https://app.example.com", "http://localhost:5173"}, false},
		{"empty", "", []string{}, false},
		{"with path", "https://app.example.com/foo", nil, true},
		{"not a url", "example.com", nil, true},
		{"bad scheme", "ftp://host", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{CORSAllowedOrigins: tc.in}
			got, err := cfg.CORSOrigins()
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestAdminEmailList(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    []string
		wantErr bool
	}{
		{"empty", "", []string{}, false},
		{"single", "admin@example.com", []string{"admin@example.com"}, false},
		{"several", "admin@example.com, ops@example.cl", []string{"admin@example.com", "ops@example.cl"}, false},
		{"lower-cased and padded", " Admin@Example.COM , ops@example.cl ", []string{"admin@example.com", "ops@example.cl"}, false},
		{"trailing comma", "admin@example.com,", []string{"admin@example.com"}, false},
		{"display name", "Admin <admin@example.com>", nil, true},
		{"no at", "not-an-email", nil, true},
		{"no domain dot", "admin@localhost", nil, true},
		{"numeric tld", "admin@example.1", nil, true},
		{"too long", strings.Repeat("a", 250) + "@example.com", nil, true},
		{"one bad entry fails the list", "admin@example.com, nope", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{AdminEmails: tc.in}
			got, err := cfg.AdminEmailList()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestValidateRejectsBadAdminEmails(t *testing.T) {
	cfg := Config{
		Port:        "8080",
		DatabaseURL: "x.db",
		JWTSecret:   testJWTSecret,
		UploadDir:   "uploads",
		AdminEmails: "not-an-email",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected Validate to reject a malformed ADMIN_EMAILS entry")
	}
	if !strings.Contains(err.Error(), "ADMIN_EMAILS") {
		t.Fatalf("error %q should mention ADMIN_EMAILS", err.Error())
	}

	cfg.AdminEmails = "admin@example.com"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid ADMIN_EMAILS rejected: %v", err)
	}
}

// TestLoadEmptyFallsBackToDefault pins the precedence rule: an empty
// variable behaves like an unset one and takes the default.
func TestLoadEmptyFallsBackToDefault(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	t.Setenv("UPLOAD_DIR", "")
	cfg := Load()
	if cfg.Port != "8080" || cfg.CORSAllowedOrigins != "*" || cfg.UploadDir != "uploads" {
		t.Fatalf("empty values did not fall back to defaults: %+v", cfg)
	}
}

// TestLoadMalformedUploadMax pins the parse-error behavior: a non-numeric
// value becomes 0 (unlimited) because Validate never sees the raw string.
func TestLoadMalformedUploadMax(t *testing.T) {
	t.Setenv("UPLOAD_MAX_TOTAL_MB", "abc")
	if got := Load().UploadMaxTotalMB; got != 0 {
		t.Fatalf("UPLOAD_MAX_TOTAL_MB = %d, want 0", got)
	}
}

// TestPhoneCountry pins the defaults (Nicaragua) and that a malformed pair
// fails startup instead of corrupting local numbers.
func TestPhoneCountry(t *testing.T) {
	t.Setenv("PHONE_COUNTRY_PREFIX", "")
	t.Setenv("PHONE_NATIONAL_LEN", "")
	cfg := Load()
	if cfg.PhoneCountryPrefix != "505" || cfg.PhoneNationalLen != 8 {
		t.Fatalf("defaults = %q/%d, want 505/8", cfg.PhoneCountryPrefix, cfg.PhoneNationalLen)
	}
	cfg.JWTSecret = testJWTSecret
	cfg.CORSAllowedOrigins = "https://app.example.com"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default phone country rejected: %v", err)
	}

	for _, tc := range []struct {
		prefix string
		natLen int
		valid  bool
	}{
		{"", 8, false},     // missing prefix
		{"56", 0, false},   // missing length
		{"056", 8, false},  // country codes do not start with 0
		{"50a", 8, false},  // not digits
		{"5051", 8, false}, // too long
		{"505", 2, false},  // too short
		{"505", 99, false}, // too long
		{"505", 8, true},   // defaults
		{"56", 9, true},    // chile configuration
	} {
		cfg.PhoneCountryPrefix = tc.prefix
		cfg.PhoneNationalLen = tc.natLen
		err := cfg.Validate()
		if tc.valid && err != nil {
			t.Errorf("prefix %q len %d rejected: %v", tc.prefix, tc.natLen, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("prefix %q len %d accepted", tc.prefix, tc.natLen)
		}
	}
}
