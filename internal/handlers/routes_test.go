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

// isMutating reports whether a method changes server state, which means the
// route needs the standard limiter.
func isMutating(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
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
		if strings.HasPrefix(r.pattern, authNamespace) && r.limit != limitStrict {
			t.Errorf("route %q lives under %s and must use the strict limiter", key, authNamespace)
		}
		if isMutating(r.method) && !strings.HasPrefix(r.pattern, authNamespace) && r.limit != limitStandard {
			t.Errorf("route %q mutates state and must declare the standard limiter", key)
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
