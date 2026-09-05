package handlers

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	h := WithSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
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
	req := httptest.NewRequest("GET", "/", nil)
	req.TLS = &tls.ConnectionState{}
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("HSTS should be set with TLS")
	}
}

func TestRecover(t *testing.T) {
	h := WithRecover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic: status = %d, want 500", rec.Code)
	}
}

func TestRequestLogWithRequestID(t *testing.T) {
	h := WithRequestLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-Id", "abc-123")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
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
