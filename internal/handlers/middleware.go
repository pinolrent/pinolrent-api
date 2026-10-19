package handlers

import (
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/pinolrent/pinolrent-api/internal/ratelimit"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

// Unwrap exposes the wrapped writer, so http.ResponseController can still
// reach interfaces such as Flusher on the original ResponseWriter.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// WriteHeader records the first status only: the net/http server ignores
// superfluous calls, and wrote tells WithRecover whether the status line is
// already on the wire.
func (r *statusRecorder) WriteHeader(status int) {
	if r.wrote {
		return
	}
	r.wrote = true
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wrote = true
	return r.ResponseWriter.Write(b)
}

// WithRequestLog wraps a handler and logs one line per request with method,
// client IP, path, status, and duration. If the request carries
// X-Request-Id, it is included in the log for correlation. trustedProxies
// are passed to ratelimit.ClientIP so the logged address matches the one the
// rate limiter uses.
func WithRequestLog(trustedProxies []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			args := []any{
				"method", r.Method,
				"ip", ratelimit.ClientIP(r, trustedProxies),
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
			}
			if id := r.Header.Get("X-Request-Id"); id != "" {
				args = append(args, "request_id", id)
			}
			slog.Info("request", args...)
		})
	}
}

// WithSecurityHeaders adds basic hardening headers to every response.
func WithSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// WithRecover wraps a handler so panics are caught, logged, and turned into a
// 500 JSON response instead of closing the connection. If the response has
// already started, the status line is on the wire and only the log happens:
// appending a JSON error would corrupt what the client is reading.
func WithRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered",
					"method", r.Method,
					"path", r.URL.Path,
					"error", rec,
					"stack", string(debug.Stack()),
				)
				if sr, ok := w.(interface{ wroteHeader() bool }); ok && sr.wroteHeader() {
					return
				}
				writeError(w, http.StatusInternalServerError, "server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// wroteHeader reports whether the status line has been written. It lets
// WithRecover detect an already-started response through the recorder.
func (r *statusRecorder) wroteHeader() bool { return r.wrote }
