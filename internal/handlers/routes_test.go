package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// knownHTTPMethods are the methods the API is allowed to expose.
var knownHTTPMethods = map[string]bool{
	http.MethodGet:    true,
	http.MethodPost:   true,
	http.MethodPatch:  true,
	http.MethodPut:    true,
	http.MethodDelete: true,
}

// TestRouteTableInvariants keeps the route table honest: everything Routes,
// NewRouter, CORS and the docs derive from it must be well formed.
func TestRouteTableInvariants(t *testing.T) {
	a := newTestAPI(t)

	seen := make(map[string]bool)
	for _, r := range a.routes() {
		key := r.method + " " + r.pattern
		if seen[key] {
			t.Errorf("duplicate route %q: ServeMux panics on repeated patterns", key)
		}
		seen[key] = true

		if r.handler == nil {
			t.Errorf("route %q has no handler", key)
		}
		if !knownHTTPMethods[r.method] {
			t.Errorf("route %q uses unsupported method %q", key, r.method)
		}
		if !strings.HasPrefix(r.pattern, "/") {
			t.Errorf("route %q pattern must start with /", key)
		}
		if strings.HasPrefix(r.pattern, authNamespace) && r.limit != limitAuth {
			t.Errorf("route %q lives under %s and must use the auth limiter", key, authNamespace)
		}
	}

	if len(seen) == 0 {
		t.Fatal("empty route table")
	}
}

// TestEveryRouteIsRegistered walks the table through the real mux: a route
// that fails to register answers with the mux's plain-text 404/405 instead of
// reaching its handler.
func TestEveryRouteIsRegistered(t *testing.T) {
	a := newTestAPI(t)
	mux := Routes(a)

	for _, r := range a.routes() {
		path := strings.ReplaceAll(r.pattern, "{id}", "1")
		req := httptest.NewRequestWithContext(context.Background(), r.method, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code == http.StatusNotFound && strings.Contains(rec.Body.String(), "page not found") {
			t.Errorf("route %s %s is not registered", r.method, path)
		}
		if rec.Code == http.StatusMethodNotAllowed {
			t.Errorf("route %s %s is registered under another method", r.method, path)
		}
	}
}

// TestLimiterPatterns pin the derived prefixes: the auth limiter covers its
// whole namespace, and every route marked limitWrite (and no other) is listed.
func TestLimiterPatterns(t *testing.T) {
	a := newTestAPI(t)

	auth := a.limiterPatterns(limitAuth)
	if len(auth) != 1 || auth[0] != authNamespace {
		t.Fatalf("auth patterns = %v, want [%s]", auth, authNamespace)
	}

	write := a.limiterPatterns(limitWrite)
	got := make(map[string]bool, len(write))
	for _, p := range write {
		got[p] = true
	}
	for _, r := range a.routes() {
		key := r.method + " " + r.pattern
		if want := r.limit == limitWrite; got[key] != want {
			t.Errorf("route %q: listed as write-limited = %v, want %v", key, got[key], want)
		}
	}
}
