package handlers

import "net/http"

// WithCORS wraps a handler and answers cross-origin requests from the given
// allow-list. Entries may be "*" (any origin) or a full origin like
// "https://app.example.com". Preflight requests with an allowed origin are
// answered with 204 without invoking the inner handler. Requests without an
// Origin header (curl, server-to-server) pass through untouched.
func WithCORS(origins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			// No ACAO header is set for disallowed origins: the browser blocks
			// the request itself, and the inner handler still runs.
			allowAll := false
			matches := false
			for _, o := range origins {
				if o == "*" {
					allowAll = true
					matches = true
					break
				}
				if o == origin {
					matches = true
				}
			}
			if !matches {
				next.ServeHTTP(w, r)
				return
			}

			if allowAll {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}

			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Max-Age", "86400")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
