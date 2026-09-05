// Package ratelimit provides an in-memory token bucket rate limiter keyed by
// client IP, built on the audited golang.org/x/time/rate limiter.
package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pinolrent/pinolrent-api/internal/httpx"
	"golang.org/x/time/rate"
)

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	httpx.WriteError(w, status, msg)
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
// any of the given prefixes, returning 429 when over the limit. Each prefix
// may optionally be prefixed with a method like "POST /path" to limit only
// that method; a plain "/path" limits all methods.
func (l *Limiter) Middleware(next http.Handler, limitPaths ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if matchesRequest(r, limitPaths) {
			l.mu.Lock()
			if time.Since(l.lastGC) > l.gcEvery {
				l.gc(time.Now())
				l.lastGC = time.Now()
			}
			l.mu.Unlock()

			if !l.Allow(clientIP(r)) {
				w.Header().Set("Retry-After", "60")
				writeJSONError(w, http.StatusTooManyRequests, "too many requests")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP returns the client IP. Proxy headers (X-Forwarded-For/X-Real-IP)
// are honored only when the direct peer is a loopback proxy that overwrites
// them; otherwise any client could spoof an arbitrary IP and get a fresh
// rate-limit bucket. Direct connections always key on RemoteAddr.
func clientIP(r *http.Request) string {
	if isLocalProxy(r.RemoteAddr) {
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
	}
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// isLocalProxy reports whether remoteAddr belongs to a loopback peer, i.e.
// the request arrived via a reverse proxy on the same host that sets the
// forwarding headers itself.
func isLocalProxy(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}

func matchesPrefix(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func matchesRequest(r *http.Request, prefixes []string) bool {
	for _, p := range prefixes {
		method, prefix, hasMethod := strings.Cut(p, " ")
		if hasMethod {
			if r.Method != method {
				continue
			}
			if strings.HasPrefix(r.URL.Path, prefix) {
				return true
			}
		} else if strings.HasPrefix(r.URL.Path, p) {
			return true
		}
	}
	return false
}
