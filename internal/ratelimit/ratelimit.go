package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	last   time.Time
}

type Limiter struct {
	mu       sync.Mutex
	rate     float64
	burst    float64
	buckets  map[string]*bucket
	lastGC   time.Time
	gcEvery  time.Duration
	tokenTTL time.Duration
}

func New(rate float64, burst int) *Limiter {
	return &Limiter{
		rate:     rate,
		burst:    float64(burst),
		buckets:  make(map[string]*bucket),
		lastGC:   time.Now(),
		gcEvery:  time.Minute,
		tokenTTL: 10 * time.Minute,
	}
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}

	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *Limiter) gc(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.last) > l.tokenTTL {
			delete(l.buckets, k)
		}
	}
}

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
				http.Error(w, `{"error":"too many requests"}`, http.StatusTooManyRequests)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
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