package handlers

import (
	"net/http"

	"github.com/pinolrent/pinolrent-api/internal/ratelimit"
)

// Rate limits, as requests per second and burst capacity per client IP.
const (
	authLimiterRate   = 0.5 // 30 per minute
	authLimiterBurst  = 30
	writeLimiterRate  = 2 // 120 per minute
	writeLimiterBurst = 20
)

// NewRouter builds the production handler chain: the route table wrapped with
// its rate limiters and, outermost, CORS, security headers, request logging
// and panic recovery.
//
// The nesting order matters: CORS ends up outermost so preflights
// short-circuit before reaching the rate limiter, and every response (even a
// 429) ships the CORS headers the browser needs to read it.
func NewRouter(a *API, origins []string) http.Handler {
	authLimiter := ratelimit.New(authLimiterRate, authLimiterBurst)
	writeLimiter := ratelimit.New(writeLimiterRate, writeLimiterBurst)

	var inner http.Handler = Routes(a)
	inner = authLimiter.Middleware(inner, a.limiterPatterns(limitAuth)...)
	inner = writeLimiter.Middleware(inner, a.limiterPatterns(limitWrite)...)
	return WithCORS(origins)(WithSecurityHeaders(WithRequestLog(WithRecover(inner))))
}
