package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pinolrent/pinolrent-api/internal/auth"
)

func TestHealth(t *testing.T) {
	a := newTestAPI(t)
	rec := doJSON(t, a, "GET", "/health", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var out map[string]string
	decodeJSON(t, rec, &out)
	if out["status"] != "ok" {
		t.Fatalf("status = %q, want ok", out["status"])
	}
}

func TestRegister(t *testing.T) {
	a := newTestAPI(t)
	rec := doJSON(t, a, "POST", "/auth/register", "", map[string]any{
		"email": "user@example.com", "password": "secret123",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		ID    int64  `json:"id"`
		Email string `json:"email"`
	}
	decodeJSON(t, rec, &out)
	if out.ID == 0 || out.Email != "user@example.com" {
		t.Fatalf("unexpected response: %+v", out)
	}
}

func TestRegisterValidate(t *testing.T) {
	cases := []struct {
		name   string
		body   map[string]any
		status int
	}{
		{"short password", map[string]any{"email": "a@b.com", "password": "12345"}, http.StatusBadRequest},
		{"invalid email", map[string]any{"email": "nope", "password": "secret123"}, http.StatusBadRequest},
		{"missing fields", map[string]any{"email": "a@b.com"}, http.StatusBadRequest},
		{"malformed json", nil, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestAPI(t)
			rec := doJSON(t, a, "POST", "/auth/register", "", tc.body)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

func TestRegisterNormalizesEmail(t *testing.T) {
	a := newTestAPI(t)
	rec := doJSON(t, a, "POST", "/auth/register", "", map[string]any{
		"email": "  User@Example.COM ", "password": "secret123",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Email string `json:"email"`
	}
	decodeJSON(t, rec, &out)
	if out.Email != "user@example.com" {
		t.Fatalf("email = %q, want normalized", out.Email)
	}
}

func TestRegisterDuplicate(t *testing.T) {
	a := newTestAPI(t)
	registerBuyer(t, a, "dup@example.com", "secret123")
	rec := doJSON(t, a, "POST", "/auth/register", "", map[string]any{
		"email": "dup@example.com", "password": "secret456",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestLogin(t *testing.T) {
	a := newTestAPI(t)
	registerBuyer(t, a, "user@example.com", "secret123")

	rec := doJSON(t, a, "POST", "/auth/login", "", map[string]any{
		"email": "user@example.com", "password": "secret123",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Token string `json:"token"`
	}
	decodeJSON(t, rec, &out)
	if out.Token == "" {
		t.Fatal("login returned empty token")
	}
}

func TestLoginRejects(t *testing.T) {
	a := newTestAPI(t)
	registerBuyer(t, a, "user@example.com", "secret123")

	cases := []struct {
		name string
		body map[string]any
	}{
		{"wrong password", map[string]any{"email": "user@example.com", "password": "wrongpass"}},
		{"unknown email", map[string]any{"email": "ghost@example.com", "password": "secret123"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, a, "POST", "/auth/login", "", tc.body)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestMe(t *testing.T) {
	a := newTestAPI(t)

	if rec := doJSON(t, a, "GET", "/auth/me", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401", rec.Code)
	}

	token := registerBuyer(t, a, "me@example.com", "secret123")
	rec := doJSON(t, a, "GET", "/auth/me", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		ID    int64  `json:"id"`
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	decodeJSON(t, rec, &out)
	if out.ID == 0 || out.Email != "me@example.com" || out.Role != "buyer" {
		t.Fatalf("unexpected profile: %+v", out)
	}

	rec = doJSON(t, a, "GET", "/auth/me", newSeller(t, a), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("seller status = %d body %s", rec.Code, rec.Body.String())
	}
	decodeJSON(t, rec, &out)
	if out.Role != "seller" {
		t.Fatalf("seller role = %q, want seller", out.Role)
	}
}

func TestRequireAuth(t *testing.T) {
	a := newTestAPI(t)

	mux := http.NewServeMux()
	mux.Handle("GET /auth/me", a.Auth.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		u, _ := auth.CurrentUser(r.Context())
		writeJSON(w, http.StatusOK, map[string]string{"email": u.Email, "role": u.Role})
	}))

	serve := func(method, path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(context.Background(), method, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := serve("GET", "/auth/me", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: status = %d, want 401", rec.Code)
	}
	if rec := serve("GET", "/auth/me", "bogus.token.x"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token: status = %d, want 401", rec.Code)
	}

	token := registerBuyer(t, a, "me@example.com", "secret123")
	rec := serve("GET", "/auth/me", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid token: status = %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	decodeJSON(t, rec, &out)
	if out.Email != "me@example.com" || out.Role != "buyer" {
		t.Fatalf("unexpected user: %+v", out)
	}
}

func TestRequireRole(t *testing.T) {
	a := newTestAPI(t)

	mux := http.NewServeMux()
	mux.Handle("GET /seller/probe", a.Auth.RequireRole("seller", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	}))

	serve := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(context.Background(), "GET", "/seller/probe", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := serve(newSeller(t, a)); rec.Code != http.StatusOK {
		t.Fatalf("seller: status = %d body %s", rec.Code, rec.Body.String())
	}

	if rec := serve(registerBuyer(t, a, "cli@example.com", "secret123")); rec.Code != http.StatusForbidden {
		t.Fatalf("buyer on seller route: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}
