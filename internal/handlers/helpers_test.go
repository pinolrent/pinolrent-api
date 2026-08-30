package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/db"
)

func newTestAPI(t *testing.T) *API {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })

	if err := db.SeedAdmin(d, "admin@pinolrent.com", "admin123"); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	a := auth.New("test-secret", d)
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

	req := httptest.NewRequest(method, path, rdr)
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

func registerClient(t *testing.T, a *API, email, password string) string {
	t.Helper()
	rec := doJSON(t, a, "POST", "/auth/register", "", map[string]any{
		"email": email, "password": password,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status %d body %s", rec.Code, rec.Body.String())
	}
	return loginClient(t, a, email, password)
}

func loginClient(t *testing.T, a *API, email, password string) string {
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

func loginAdmin(t *testing.T, a *API) string {
	t.Helper()
	return loginClient(t, a, "admin@pinolrent.com", "admin123")
}