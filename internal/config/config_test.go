package config

import (
	"strings"
	"testing"
)

//nolint:gosec // test-only JWT secret, never used in production
const testJWTSecret = "test-secret-32-bytes-minimum-okay"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("JWT_SECRET", testJWTSecret)
	cfg := Load()
	if cfg.Port != "8080" || cfg.DatabaseURL != "pinolrent.db" || cfg.CORSAllowedOrigins != "*" {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("PORT", "9999")
	t.Setenv("DATABASE_URL", "custom.db")
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
	cfg := Load()
	if cfg.Port != "9999" || cfg.DatabaseURL != "custom.db" || cfg.CORSAllowedOrigins != "https://app.example.com" {
		t.Fatalf("overrides not applied: %+v", cfg)
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
	cfg := Config{Port: "8080", DatabaseURL: "x.db", JWTSecret: testJWTSecret}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
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
