package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/db"
)

func newTestAPI(t *testing.T) *API {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	a := auth.New("test-secret-32-bytes-minimum-okay", d)
	return New(d, a)
}

func doJSON(t *testing.T, a *API, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(b)
	}

	req := httptest.NewRequestWithContext(context.Background(), method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	Routes(a).ServeHTTP(rec, req)
	return rec
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if rec.Body.Len() == 0 {
		t.Fatalf("empty body, status %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response %s: %v", rec.Body.String(), err)
	}
}

func registerBuyer(t *testing.T, a *API, email, password string) string {
	t.Helper()
	rec := doJSON(t, a, "POST", "/auth/register", "", map[string]any{
		"email": email, "password": password,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status %d body %s", rec.Code, rec.Body.String())
	}
	return loginBuyer(t, a, email, password)
}

func loginBuyer(t *testing.T, a *API, email, password string) string {
	return login(t, a, email, password)
}

// registerSeller registers a seller account and returns its login token.
func registerSeller(t *testing.T, a *API, email, password string) string {
	t.Helper()
	rec := doJSON(t, a, "POST", "/auth/register/seller", "", map[string]any{
		"email": email, "password": password,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register seller: status %d body %s", rec.Code, rec.Body.String())
	}
	return login(t, a, email, password)
}

// newSeller registers a fresh seller with a unique email and returns its token.
func newSeller(t *testing.T, a *API) string {
	t.Helper()
	sellerSeq.Add(1)
	return registerSeller(t, a, fmt.Sprintf("seller-%d@example.com", sellerSeq.Load()), "secret123")
}

var sellerSeq atomic.Int64

func login(t *testing.T, a *API, email, password string) string {
	t.Helper()
	rec := doJSON(t, a, "POST", "/auth/login", "", map[string]any{
		"email": email, "password": password,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Token string `json:"token"`
	}
	decodeJSON(t, rec, &out)
	if out.Token == "" {
		t.Fatal("login: empty token")
	}
	return out.Token
}

func futureDate(days int) string {
	return time.Now().UTC().AddDate(0, 0, days).Format(dateLayout)
}
