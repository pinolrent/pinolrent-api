package handlers

import (
	"net/http"
	"strings"

	"github.com/pinolrent/pinolrent-api/internal/ratelimit"
)

// Rate limits, as requests per second and burst capacity per client IP.
// The standard burst leaves room for a legitimate batch of writes (a smoke
// test or a client retrying a form), while the sustained rate stays the real
// control at 120 requests per minute.
const (
	strictLimiterRate    = 0.5 // 30 per minute
	strictLimiterBurst   = 30
	standardLimiterRate  = 2 // 120 per minute
	standardLimiterBurst = 60
)

// NewRouter builds the production handler chain: every route from the table
// with its own limiter applied, wrapped outermost by CORS, security headers,
// request logging and panic recovery.
//
// Limiters are attached per route instead of by path prefix so the budget is
// exact: a pattern like "/reservations/{id}/payment" never matches a real
// request path, and a prefix like "/reservations/" would also catch endpoints
// that must stay unlimited.
//
// The nesting order matters: CORS ends up outermost so preflights
// short-circuit before reaching a rate limiter, and every response (even a
// 429) ships the CORS headers the browser needs to read it.
func NewRouter(a *API, origins []string) http.Handler {
	strict := ratelimit.New(strictLimiterRate, strictLimiterBurst)
	standard := ratelimit.New(standardLimiterRate, standardLimiterBurst)

	mux := http.NewServeMux()
	for _, r := range a.routes() {
		var h http.Handler = r.handler
		// /auth/ is metered as a namespace below, so even unregistered paths
		// there stay limited; adding a per-route limiter would double-charge.
		if !strings.HasPrefix(r.pattern, authNamespace) {
			h = limiterFor(r.limit, strict, standard)(h)
		}
		mux.Handle(r.method+" "+r.pattern, h)
	}

	inner := strict.Middleware(mux, authNamespace)
	return WithCORS(origins, a.allowedMethods())(WithSecurityHeaders(WithRequestLog(WithRecover(inner))))
}

// limiterFor maps a route's kind to the limiter that enforces it, or to a
// pass-through when the route is unprotected.
func limiterFor(kind limiterKind, strict, standard *ratelimit.Limiter) func(http.Handler) http.Handler {
	switch kind {
	case limitStrict:
		return strict.Handler
	case limitStandard:
		return standard.Handler
	default:
		return func(next http.Handler) http.Handler { return next }
	}
}
