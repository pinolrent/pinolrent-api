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
	mu         sync.Mutex
	rate       float64
	burst      int
	trustedFor []*net.IPNet
	limits     map[string]*entry
	lastGC     time.Time
	gcEvery    time.Duration
	tokenTTL   time.Duration
}

// New returns a Limiter that refills at the given tokens-per-second rate with
// the given burst capacity for each key. trustedProxies are the networks whose
// forwarding headers are believed (loopback always is); pass none when the
// server is reached directly.
func New(rate float64, burst int, trustedProxies ...*net.IPNet) *Limiter {
	return &Limiter{
		rate:       rate,
		burst:      burst,
		trustedFor: trustedProxies,
		limits:     make(map[string]*entry),
		lastGC:     time.Now(),
		gcEvery:    time.Minute,
		tokenTTL:   10 * time.Minute,
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

// Handler wraps a handler so every request it receives is rate-limited. Unlike
// Middleware there is no path matching involved: it belongs to a single route,
// which is what lets a route table attach an exact limiter per endpoint.
func (l *Limiter) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.maybeGC()
		if !l.Allow(clientIP(r, l.trustedFor)) {
			w.Header().Set("Retry-After", "60")
			writeJSONError(w, http.StatusTooManyRequests, "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Middleware wraps a handler and rate-limits requests whose path starts with
// any of the given prefixes, returning 429 when over the limit. Each prefix
// may optionally be prefixed with a method like "POST /path" to limit only
// that method; a plain "/path" limits all methods.
func (l *Limiter) Middleware(next http.Handler, limitPaths ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if matchesRequest(r, limitPaths) {
			l.maybeGC()

			if !l.Allow(clientIP(r, l.trustedFor)) {
				w.Header().Set("Retry-After", "60")
				writeJSONError(w, http.StatusTooManyRequests, "too many requests")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// maybeGC drops idle buckets once per gcEvery.
func (l *Limiter) maybeGC() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if time.Since(l.lastGC) > l.gcEvery {
		l.gc(time.Now())
		l.lastGC = time.Now()
	}
}

// clientIP returns the client IP for rate limiting. Forwarding headers are
// honored only when the direct peer is a trusted proxy (loopback or one of the
// configured networks); otherwise any client could spoof a header and get a
// fresh bucket. Within a trusted chain the header is read from right to left
// and the first value that is not itself a trusted proxy wins, so a client that
// injects its own X-Forwarded-For cannot choose the bucket: our proxy appends
// the address it saw at the end of the chain.
func clientIP(r *http.Request, trustedProxies []*net.IPNet) string {
	peer := remoteHost(r.RemoteAddr)
	if isTrustedProxy(peer, trustedProxies) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			for i := len(parts) - 1; i >= 0; i-- {
				ip := strings.TrimSpace(parts[i])
				if ip == "" || isTrustedProxy(ip, trustedProxies) {
					continue
				}
				return ip
			}
		}
		if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
			return xri
		}
	}
	return peer
}

// remoteHost strips the port from a RemoteAddr-shaped string.
func remoteHost(remoteAddr string) string {
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return h
	}
	return remoteAddr
}

// isTrustedProxy reports whether a host belongs to a loopback peer or to one of
// the configured proxy networks.
func isTrustedProxy(host string, trustedProxies []*net.IPNet) bool {
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, n := range trustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
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
