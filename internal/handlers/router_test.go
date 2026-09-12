package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// doRouter sends a request through the full production chain (NewRouter), not
// just the mux, so the middleware order is what gets exercised.
func doRouter(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRouterSecurityHeaders(t *testing.T) {
	a := newTestAPI(t)
	rec := doRouter(t, NewRouter(a, []string{"*"}), "GET", "/health")

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

func TestRouterStrictLimiterCoversAuthNamespace(t *testing.T) {
	a := newTestAPI(t)
	router := NewRouter(a, []string{"https://app.example.com"})

	var last *httptest.ResponseRecorder
	for i := 0; i <= strictLimiterBurst; i++ {
		req := httptest.NewRequestWithContext(context.Background(), "POST", "/auth/login", nil)
		req.Header.Set("Origin", "https://app.example.com")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		last = rec
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("request %d: status = %d, want 429", strictLimiterBurst+1, last.Code)
	}
	if got := last.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q, want 60", got)
	}
	// CORS sits outside the limiter, so even the 429 is readable by browsers.
	if got := last.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("ACAO on 429 = %q, want the allowed origin", got)
	}
}

// TestRouterStrictLimiterCoversUnknownAuthPaths pins the namespace behaviour:
// metering cannot be dodged by hitting a path that is not registered.
func TestRouterStrictLimiterCoversUnknownAuthPaths(t *testing.T) {
	a := newTestAPI(t)
	router := NewRouter(a, []string{"*"})

	var last *httptest.ResponseRecorder
	for i := 0; i <= strictLimiterBurst; i++ {
		last = doRouter(t, router, "POST", "/auth/does-not-exist")
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", last.Code)
	}
}

// TestEveryLimitedRouteIsActuallyLimited walks the table and proves each route
// that declares a limiter really answers 429 once its burst is spent. This is
// the regression test for prefix matching: a pattern such as
// "/reservations/{id}/payment" never matches a real request path, so the
// limiter used to be silently inert.
func TestEveryLimitedRouteIsActuallyLimited(t *testing.T) {
	for _, tc := range []struct {
		kind  limiterKind
		burst int
	}{
		{limitStrict, strictLimiterBurst},
		{limitStandard, standardLimiterBurst},
	} {
		a := newTestAPI(t)
		for _, r := range a.routes() {
			if r.limit != tc.kind || strings.HasPrefix(r.pattern, authNamespace) {
				continue
			}
			t.Run(r.method+" "+r.pattern, func(t *testing.T) {
				// A router per route so the burst starts fresh.
				router := NewRouter(a, []string{"*"})
				path := strings.ReplaceAll(r.pattern, "{id}", "1")

				var last *httptest.ResponseRecorder
				for i := 0; i <= tc.burst; i++ {
					last = doRouter(t, router, r.method, path)
				}
				if last.Code != http.StatusTooManyRequests {
					t.Fatalf("request %d to %s %s: status = %d, want 429",
						tc.burst+1, r.method, path, last.Code)
				}
			})
		}
	}
}

// TestRouterPreflightSkipsLimiter proves CORS is outermost: hammering
// preflights must not consume the shared strict budget, and must answer 204
// without reaching the mux.
func TestRouterPreflightSkipsLimiter(t *testing.T) {
	a := newTestAPI(t)
	router := NewRouter(a, []string{"https://app.example.com"})

	for i := 0; i < strictLimiterBurst*2; i++ {
		req := httptest.NewRequestWithContext(context.Background(), "OPTIONS", "/auth/login", nil)
		req.Header.Set("Origin", "https://app.example.com")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("preflight %d: status = %d, want 204", i, rec.Code)
		}
	}

	if rec := doRouter(t, router, "POST", "/auth/login"); rec.Code == http.StatusTooManyRequests {
		t.Fatal("preflights consumed the strict rate limit budget")
	}
}
