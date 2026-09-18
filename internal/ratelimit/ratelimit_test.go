package ratelimit

import (
	"context"
	"net"
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
	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), "/auth/")

	do := func(path string) int {
		req := httptest.NewRequestWithContext(context.Background(), "GET", path, nil)
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

// mustCIDRs parses networks for the trusted-proxy tests.
func mustCIDRs(t *testing.T, entries ...string) []*net.IPNet {
	t.Helper()
	var nets []*net.IPNet
	for _, e := range entries {
		_, n, err := net.ParseCIDR(e)
		if err != nil {
			t.Fatalf("parse CIDR %q: %v", e, err)
		}
		nets = append(nets, n)
	}
	return nets
}

func ipRequest(remoteAddr string, headers map[string]string) *http.Request {
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/", nil)
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func TestClientIPIgnoresSpoofedHeaders(t *testing.T) {
	headers := map[string]string{"X-Forwarded-For": "1.2.3.4", "X-Real-IP": "5.6.7.8"}

	req := ipRequest("203.0.113.7:1234", headers)
	if got := clientIP(req, nil); got != "203.0.113.7" {
		t.Fatalf("spoofed headers honored: got %q, want RemoteAddr host", got)
	}

	// A peer outside the configured networks is just as untrusted.
	req = ipRequest("203.0.113.7:1234", headers)
	if got := clientIP(req, mustCIDRs(t, "172.18.0.0/16")); got != "203.0.113.7" {
		t.Fatalf("peer outside the CIDRs honored headers: got %q", got)
	}
}

func TestClientIPHonorsLoopbackProxy(t *testing.T) {
	req := ipRequest("127.0.0.1:1234", map[string]string{"X-Forwarded-For": "1.2.3.4"})
	if got := clientIP(req, nil); got != "1.2.3.4" {
		t.Fatalf("proxy XFF ignored: got %q, want 1.2.3.4", got)
	}
}

func TestClientIPHonorsTrustedProxyNetwork(t *testing.T) {
	req := ipRequest("172.18.0.5:443", map[string]string{"X-Forwarded-For": "198.51.100.9"})
	if got := clientIP(req, mustCIDRs(t, "172.18.0.0/16")); got != "198.51.100.9" {
		t.Fatalf("got %q, want 198.51.100.9", got)
	}
}

// TestClientIPPrefersRightmostUntrusted pins the anti-spoofing rule: a client
// that sends its own X-Forwarded-For cannot choose the bucket, because the
// address our proxy appended is the rightmost value in the chain.
func TestClientIPPrefersRightmostUntrusted(t *testing.T) {
	req := ipRequest("127.0.0.1:1234", map[string]string{
		"X-Forwarded-For": "1.2.3.4, 203.0.113.5",
	})
	if got := clientIP(req, nil); got != "203.0.113.5" {
		t.Fatalf("got %q, want the rightmost value 203.0.113.5", got)
	}
}

// TestClientIPSkipsTrustedHops covers a chain of two trusted proxies: the
// client is the first value that is not a proxy itself.
func TestClientIPSkipsTrustedHops(t *testing.T) {
	req := ipRequest("172.18.0.5:443", map[string]string{
		"X-Forwarded-For": "203.0.113.5, 172.18.0.9",
	})
	if got := clientIP(req, mustCIDRs(t, "172.18.0.0/16")); got != "203.0.113.5" {
		t.Fatalf("got %q, want 203.0.113.5", got)
	}
}

func TestClientIPFallsBackToXRealIP(t *testing.T) {
	req := ipRequest("127.0.0.1:1234", map[string]string{"X-Real-IP": "198.51.100.7"})
	if got := clientIP(req, nil); got != "198.51.100.7" {
		t.Fatalf("got %q, want 198.51.100.7", got)
	}
}

// TestClientIPIgnoresEmptyChainEntries covers proxies that append an empty
// value, which must not become the bucket key.
func TestClientIPIgnoresEmptyChainEntries(t *testing.T) {
	req := ipRequest("127.0.0.1:1234", map[string]string{"X-Forwarded-For": "203.0.113.5, "})
	if got := clientIP(req, nil); got != "203.0.113.5" {
		t.Fatalf("got %q, want 203.0.113.5", got)
	}
}

// TestHandlerKeysByForwardedIPFromTrustedProxy checks the wiring end to end:
// two different clients arriving through the same trusted proxy must not share
// a bucket, while a spoofed header from an untrusted peer must.
func TestHandlerKeysByForwardedIPFromTrustedProxy(t *testing.T) {
	l := New(1, 1, mustCIDRs(t, "172.18.0.0/16")...)
	h := l.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	do := func(remoteAddr, xff string) int {
		req := httptest.NewRequestWithContext(context.Background(), "GET", "/", nil)
		req.RemoteAddr = remoteAddr
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := do("172.18.0.5:443", "1.2.3.4"); code != http.StatusOK {
		t.Fatalf("first client: status = %d, want 200", code)
	}
	if code := do("172.18.0.5:443", "5.6.7.8"); code != http.StatusOK {
		t.Fatalf("second client through the same proxy: status = %d, want 200", code)
	}
	if code := do("172.18.0.5:443", "1.2.3.4"); code != http.StatusTooManyRequests {
		t.Fatalf("repeat client: status = %d, want 429", code)
	}
}

func TestHandlerIgnoresForwardedHeaderFromUntrustedPeer(t *testing.T) {
	l := New(1, 1)
	h := l.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	do := func(xff string) int {
		req := httptest.NewRequestWithContext(context.Background(), "GET", "/", nil)
		req.RemoteAddr = "203.0.113.7:1234"
		req.Header.Set("X-Forwarded-For", xff)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := do("1.2.3.4"); code != http.StatusOK {
		t.Fatalf("first request: status = %d, want 200", code)
	}
	if code := do("5.6.7.8"); code != http.StatusTooManyRequests {
		t.Fatal("a spoofed header rotated the bucket of an untrusted peer")
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
