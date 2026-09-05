// Package ratelimit provides an in-memory token bucket rate limiter keyed by
// client IP, built on the audited golang.org/x/time/rate limiter.
package ratelimit

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

type entry struct {
	limiter *rate.Limiter
	last    time.Time
}

// Limiter is a token bucket rate limiter with per-key refill and expiry.
type Limiter struct {
	mu       sync.Mutex
	rate     float64
	burst    int
	limits   map[string]*entry
	lastGC   time.Time
	gcEvery  time.Duration
	tokenTTL time.Duration
}

// New returns a Limiter that refills at the given tokens-per-second rate with
// the given burst capacity for each key.
func New(rate float64, burst int) *Limiter {
	return &Limiter{
		rate:     rate,
		burst:    burst,
		limits:   make(map[string]*entry),
		lastGC:   time.Now(),
		gcEvery:  time.Minute,
		tokenTTL: 10 * time.Minute,
	}
}

// Allow consumes a token for the key and reports whether the request is
// within the limit.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	e, ok := l.limits[key]
	if !ok {
		e = &entry{
			limiter: rate.NewLimiter(rate.Limit(l.rate), l.burst),
			last:    now,
		}
		l.limits[key] = e
	}
	e.last = now

	return e.limiter.Allow()
}

func (l *Limiter) gc(now time.Time) {
	for k, e := range l.limits {
		if now.Sub(e.last) > l.tokenTTL {
			delete(l.limits, k)
		}
	}
}

// Middleware wraps a handler and rate-limits requests whose path starts with
// any of the given prefixes, returning 429 when over the limit.
func (l *Limiter) Middleware(next http.Handler, limitPaths ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if matchesPrefix(r.URL.Path, limitPaths) {
			l.mu.Lock()
			if time.Since(l.lastGC) > l.gcEvery {
				l.gc(time.Now())
				l.lastGC = time.Now()
			}
			l.mu.Unlock()

			if !l.Allow(clientIP(r)) {
				writeJSONError(w, http.StatusTooManyRequests, "too many requests")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		if ip := strings.TrimSpace(xri); ip != "" {
			return ip
		}
	}
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

func matchesPrefix(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
