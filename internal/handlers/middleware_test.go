package handlers

import (
	"bytes"
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	h := WithSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), "GET", "/", nil))
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing X-Content-Type-Options")
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("missing X-Frame-Options")
	}
	if rec.Header().Get("Referrer-Policy") == "" {
		t.Fatal("missing Referrer-Policy")
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("HSTS should not be set without TLS")
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/", nil)
	req.TLS = &tls.ConnectionState{}
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("HSTS should be set with TLS")
	}
}

func TestRecover(t *testing.T) {
	h := WithRecover(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), "GET", "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic: status = %d, want 500", rec.Code)
	}
}

// TestRecoverAfterPartialWrite pins the guard: once the status line is out,
// the recovery must not append a JSON error to whatever the client is
// already reading.
func TestRecoverAfterPartialWrite(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"partial":true}`))
		panic("boom")
	})
	// Same nesting as production: the recorder sits outside the recoverer.
	h := WithRequestLog(nil)(WithRecover(handler))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), "GET", "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "server error") {
		t.Fatalf("body = %q, the recovery appended to a started response", rec.Body.String())
	}
	if got := rec.Body.String(); got != `{"partial":true}` {
		t.Fatalf("body = %q, want the partial response untouched", got)
	}
}

// TestStatusRecorderUnwrapReachesFlusher pins Unwrap: without it
// http.ResponseController cannot find the original writer's Flusher.
func TestStatusRecorderUnwrapReachesFlusher(t *testing.T) {
	h := WithRequestLog(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush through the recorder: %v", err)
		}
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), "GET", "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestRequestLogWithRequestID(t *testing.T) {
	h := WithRequestLog(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/test", nil)
	req.Header.Set("X-Request-Id", "abc-123")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// TestRequestLogIncludesClientIP pins the claim in the deployment and
// configuration docs: a request line carries the client address, not only
// the method and path.
func TestRequestLogIncludesClientIP(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	h := WithRequestLog(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/test", nil)
	req.RemoteAddr = "203.0.113.7:1234"
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !strings.Contains(buf.String(), "ip=203.0.113.7") {
		t.Fatalf("log line %q does not carry the client IP", buf.String())
	}
}

func TestDecodeBodyUnknownField(t *testing.T) {
	a := newTestAPI(t)
	token := newSeller(t, a)
	rec := doJSON(t, a, "POST", "/seller/cars", token, map[string]any{
		"name": "X", "unknown_field": "oops",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: status = %d, want 400", rec.Code)
	}
}

func TestValidURLWithUserInfo(t *testing.T) {
	if validURL("https://user:pass@example.com/photo.jpg") {
		t.Fatal("URL with userinfo should be rejected")
	}
}

func TestPriceBoundary(t *testing.T) {
	a := newTestAPI(t)
	token := newSeller(t, a)
	rec := doJSON(t, a, "POST", "/seller/cars", token, map[string]any{"name": "Exact", "price_per_day": 100000000})
	if rec.Code != http.StatusCreated {
		t.Fatalf("price 100M: status = %d, want 201", rec.Code)
	}
	rec = doJSON(t, a, "POST", "/seller/cars", token, map[string]any{"name": "Over", "price_per_day": 100000001})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("price 100M+1: status = %d, want 400", rec.Code)
	}
}
