package handlers

import (
	"net/http"
	"sort"

	"github.com/rs/cors"
)

// WithCORS wraps a handler and answers cross-origin requests from the given
// allow-list. Entries may be "*" (any origin) or a full origin like
// "https://app.example.com". Preflight requests short-circuit with 204 without
// invoking the inner handler. Requests without an Origin header pass through
// with no CORS headers.
func WithCORS(origins []string, methods []string) func(http.Handler) http.Handler {
	c := cors.New(cors.Options{
		AllowedOrigins: origins,
		AllowedMethods: methods,
		AllowedHeaders: []string{"Authorization", "Content-Type"},
		MaxAge:         86400,
	})
	return c.Handler
}

// allowedMethods returns the CORS methods derived from the route table, so a
// method a new route uses (PUT, DELETE, ...) is accepted in preflights without
// touching the CORS configuration.
func (a *API) allowedMethods() []string {
	seen := make(map[string]bool)
	methods := []string{http.MethodOptions}
	for _, r := range a.routes() {
		if !seen[r.method] {
			seen[r.method] = true
			methods = append(methods, r.method)
		}
	}
	sort.Strings(methods)
	return methods
}
