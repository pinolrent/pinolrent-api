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
	msg := err.Error()
	for _, want := range []string{"JWT_SECRET", "ADMIN_PASSWORD", "ADMIN_EMAIL"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q should mention %s", msg, want)
		}
	}
}

func TestValidateOK(t *testing.T) {
	cfg := Config{Port: "8080", DatabaseURL: "x.db", JWTSecret: "s", AdminEmail: "a@b.com", AdminPassword: "p"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
