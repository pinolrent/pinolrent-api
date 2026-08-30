package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAllowBurst(t *testing.T) {
	l := New(1, 3)
	for i := 0; i < 3; i++ {
		if !l.Allow("ip") {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if l.Allow("ip") {
		t.Fatal("request beyond burst should be blocked")
	}
}

func TestAllowRefill(t *testing.T) {
	l := New(100, 1)
	if !l.Allow("ip") {
		t.Fatal("first request should be allowed")
	}
	if l.Allow("ip") {
		t.Fatal("second request should be blocked (no refill yet)")
	}
	time.Sleep(30 * time.Millisecond)
	if !l.Allow("ip") {
		t.Fatal("request after refill should be allowed")
	}
}

func TestAllowIsPerKey(t *testing.T) {
	l := New(1, 1)
	if !l.Allow("a") {
		t.Fatal("a allowed")
	}
	if l.Allow("a") {
		t.Fatal("a blocked")
	}
	if !l.Allow("b") {
		t.Fatal("b unaffected")
	}
}

func TestMiddleware(t *testing.T) {
	l := New(100, 2)
	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), "/auth/")

	do := func(path string) int {
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := do("/auth/login"); code != http.StatusOK {
		t.Fatalf("/auth/login (1): status = %d", code)
	}
	if code := do("/auth/register"); code != http.StatusOK {
		t.Fatalf("/auth/register (2): status = %d", code)
	}
	if code := do("/auth/login"); code != http.StatusTooManyRequests {
		t.Fatalf("third /auth request: status = %d, want 429", code)
	}
	if code := do("/cars"); code != http.StatusOK {
		t.Fatalf("/cars not limited: status = %d, want 200", code)
	}
}

func TestMatchesPrefix(t *testing.T) {
	if !matchesPrefix("/auth/register", []string{"/auth/"}) {
		t.Fatal("/auth/register should match")
	}
	if matchesPrefix("/cars", []string{"/auth/"}) {
		t.Fatal("/cars should not match")
	}
}