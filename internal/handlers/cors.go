package handlers

import (
	"net/http"

	"github.com/rs/cors"
)

// WithCORS wraps a handler and answers cross-origin requests from the given
// allow-list. Entries may be "*" (any origin) or a full origin like
// "https://app.example.com". Preflight requests short-circuit with 204 without
// invoking the inner handler. Requests without an Origin header pass through
// with no CORS headers.
func WithCORS(origins []string) func(http.Handler) http.Handler {
	c := cors.New(cors.Options{
		AllowedOrigins: origins,
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodOptions},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
		MaxAge:         86400,
	})
	return c.Handler
}
