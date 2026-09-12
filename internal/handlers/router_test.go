package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// doRouter sends a request through the full production chain (NewRouter), not
// just the mux, so the middleware order is what gets exercised.
func doRouter(t *testing.T, h http.Handler, method, path, token, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRouterSecurityHeaders(t *testing.T) {
	a := newTestAPI(t)
	rec := doRouter(t, NewRouter(a, []string{"*"}), "GET", "/health", "", "")

	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
		"Permissions-Policy":     "camera=(), microphone=(), geolocation=()",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestRouterAuthLimiter(t *testing.T) {
	a := newTestAPI(t)
	router := NewRouter(a, []string{"https://app.example.com"})

	var last *httptest.ResponseRecorder
	for i := 0; i <= authLimiterBurst; i++ {
		last = doRouter(t, router, "POST", "/auth/login", "", "https://app.example.com")
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("request %d: status = %d, want 429", authLimiterBurst+1, last.Code)
	}
	if got := last.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q, want 60", got)
	}
	// CORS sits outside the limiter, so even the 429 is readable by browsers.
	if got := last.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("ACAO on 429 = %q, want the allowed origin", got)
	}
}

func TestRouterWriteLimiter(t *testing.T) {
	a := newTestAPI(t)
	router := NewRouter(a, []string{"*"})

	var last *httptest.ResponseRecorder
	for i := 0; i <= writeLimiterBurst; i++ {
		last = doRouter(t, router, "POST", "/uploads", "", "")
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("request %d: status = %d, want 429", writeLimiterBurst+1, last.Code)
	}
}

// TestRouterPreflightSkipsLimiter proves CORS is outermost: hammering
// preflights must not consume the shared auth budget, and must answer 204
// without reaching the mux.
func TestRouterPreflightSkipsLimiter(t *testing.T) {
	a := newTestAPI(t)
	router := NewRouter(a, []string{"https://app.example.com"})

	for i := 0; i < authLimiterBurst*2; i++ {
		req := httptest.NewRequestWithContext(context.Background(), "OPTIONS", "/auth/login", nil)
		req.Header.Set("Origin", "https://app.example.com")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("preflight %d: status = %d, want 204", i, rec.Code)
		}
	}

	rec := doRouter(t, router, "POST", "/auth/login", "", "https://app.example.com")
	if rec.Code == http.StatusTooManyRequests {
		t.Fatal("preflights consumed the auth rate limit budget")
	}
}
