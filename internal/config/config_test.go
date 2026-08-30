package config

import (
	"strings"
	"testing"
)

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

func TestValidateOK(t *testing.T) {
	cfg := Config{Port: "8080", DatabaseURL: "x.db", JWTSecret: "s"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateBadCORS(t *testing.T) {
	cfg := Config{JWTSecret: "s", CORSAllowedOrigins: "https://app.example.com/path"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for malformed CORS origin")
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
		{"with path", "https://app.example.com/", nil, true},
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
