package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func withCORSRequest(t *testing.T, method, path, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, path, nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	mux := Routes(newTestAPI(t))
	WithCORS([]string{"https://app.example.com"})(mux).ServeHTTP(rec, req)
	return rec
}

func TestNoOriginPassesThrough(t *testing.T) {
	rec := withCORSRequest(t, "GET", "/health", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("no ACAO expected without an Origin header")
	}
}

func TestAllowedOrigin(t *testing.T) {
	rec := withCORSRequest(t, "GET", "/health", "https://app.example.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("ACAO = %q", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("Vary = %q, want Origin", got)
	}
}

func TestDisallowedOrigin(t *testing.T) {
	rec := withCORSRequest(t, "GET", "/health", "https://evil.example.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (handler still runs)", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("no ACAO expected for a disallowed origin")
	}
}

func TestWildcardOrigin(t *testing.T) {
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/health", nil)
	req.Header.Set("Origin", "https://anything.example.com")
	rec := httptest.NewRecorder()

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	WithCORS([]string{"*"})(inner).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("ACAO = %q, want *", got)
	}
	if !called {
		t.Fatal("inner handler was not invoked")
	}
}

func TestPreflight(t *testing.T) {
	req := httptest.NewRequestWithContext(context.Background(), "OPTIONS", "/auth/login", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
	rec := httptest.NewRecorder()

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	WithCORS([]string{"https://app.example.com"})(inner).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if called {
		t.Fatal("preflight must not invoke the inner handler")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("ACAO = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "POST" {
		t.Fatalf("Allow-Methods = %q, want requested method echoed", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "authorization, content-type" {
		t.Fatalf("Allow-Headers = %q, want requested headers echoed", got)
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "86400" {
		t.Fatalf("Max-Age = %q, want 86400", got)
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("no credentials header expected (no cookies used)")
	}
}

func TestPreflightDisallowedOrigin(t *testing.T) {
	req := httptest.NewRequestWithContext(context.Background(), "OPTIONS", "/auth/login", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	WithCORS([]string{"https://app.example.com"})(inner).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if called {
		t.Fatal("preflight must not invoke the inner handler")
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("no ACAO expected for a disallowed origin")
	}
	if rec.Header().Get("Access-Control-Allow-Methods") != "" {
		t.Fatal("no Allow-Methods expected for a disallowed origin")
	}
}
