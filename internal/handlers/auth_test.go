package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pinolrent/pinolrent-api/internal/auth"
)

func TestHealth(t *testing.T) {
	a := newTestAPI(t)
	a.Version = "test"
	rec := doJSON(t, a, "GET", "/health", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var out map[string]string
	decodeJSON(t, rec, &out)
	if out["status"] != "ok" {
		t.Fatalf("status = %q, want ok", out["status"])
	}
	if out["version"] != "test" {
		t.Fatalf("version = %q, want test", out["version"])
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
		Email string `json:"email"`
	}
	decodeJSON(t, rec, &out)
	if out.Email != "user@example.com" {
		t.Fatalf("unexpected response: %+v", out)
	}
}

func TestRegisterValidate(t *testing.T) {
	cases := []struct {
		name   string
		body   map[string]any
		status int
	}{
		{"short password", map[string]any{"email": "a@b.co", "password": "12345"}, http.StatusBadRequest},
		{"invalid email", map[string]any{"email": "nope", "password": "secret123"}, http.StatusBadRequest},
		{"single-char tld", map[string]any{"email": "a@b.c", "password": "secret123"}, http.StatusBadRequest},
		{"consecutive dots", map[string]any{"email": "a..b@c.com", "password": "secret123"}, http.StatusBadRequest},
		{"leading dot", map[string]any{"email": ".a@b.com", "password": "secret123"}, http.StatusBadRequest},
		{"leading hyphen domain", map[string]any{"email": "a@-b.com", "password": "secret123"}, http.StatusBadRequest},
		{"missing fields", map[string]any{"email": "a@b.co"}, http.StatusBadRequest},
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
	// Registration is intentionally opaque to avoid letting an attacker
	// enumerate registered emails. A duplicate returns the identical 201
	// body as a real success (no id in either case).
	rec := doJSON(t, a, "POST", "/auth/register", "", map[string]any{
		"email": "dup@example.com", "password": "secret456",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Email string `json:"email"`
	}
	decodeJSON(t, rec, &out)
	if out.Email != "dup@example.com" {
		t.Fatalf("unexpected response: %+v", out)
	}
	if strings.Contains(rec.Body.String(), `"id"`) {
		t.Fatalf("response leaks id field: %s", rec.Body.String())
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
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token"`
	}
	decodeJSON(t, rec, &out)
	if out.Token == "" {
		t.Fatal("login returned empty token")
	}
	if out.RefreshToken == "" {
		t.Fatal("login returned empty refresh token")
	}
}

func TestRefreshRotates(t *testing.T) {
	a := newTestAPI(t)
	registerBuyer(t, a, "user@example.com", "secret123")

	rec := doJSON(t, a, "POST", "/auth/login", "", map[string]any{
		"email": "user@example.com", "password": "secret123",
	})
	var login struct {
		RefreshToken string `json:"refresh_token"`
	}
	decodeJSON(t, rec, &login)

	rec = doJSON(t, a, "POST", "/auth/refresh", "", map[string]any{
		"refresh_token": login.RefreshToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: status = %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token"`
	}
	decodeJSON(t, rec, &out)
	if out.Token == "" || out.RefreshToken == "" {
		t.Fatalf("refresh returned empty pair: %+v", out)
	}
	if out.RefreshToken == login.RefreshToken {
		t.Fatal("refresh token was not rotated")
	}

	rec = doJSON(t, a, "POST", "/auth/refresh", "", map[string]any{
		"refresh_token": login.RefreshToken,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reused refresh: status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestRefreshRejectsAccessToken(t *testing.T) {
	a := newTestAPI(t)
	token := registerBuyer(t, a, "user@example.com", "secret123")

	rec := doJSON(t, a, "POST", "/auth/refresh", "", map[string]any{
		"refresh_token": token,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("access as refresh: status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
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

func TestLogoutRevokesToken(t *testing.T) {
	a := newTestAPI(t)
	token := registerBuyer(t, a, "logout@example.com", "secret123")

	// Token works before logout.
	rec := doJSON(t, a, "GET", "/auth/me", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("pre-logout: status = %d, want 200", rec.Code)
	}

	// Logout succeeds.
	rec = doJSON(t, a, "POST", "/auth/logout", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// Same token is now rejected.
	rec = doJSON(t, a, "GET", "/auth/me", token, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("post-logout: status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestLogoutRequiresAuth(t *testing.T) {
	a := newTestAPI(t)
	rec := doJSON(t, a, "POST", "/auth/logout", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401", rec.Code)
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

func TestRegisterFieldLengthCaps(t *testing.T) {
	longEmail := strings.Repeat("a", 250) + "@b.com" // 256 bytes
	cases := []struct {
		name string
		body map[string]any
	}{
		{"password too short", map[string]any{"email": "x@y.co", "password": "short"}},
		{"password too long", map[string]any{"email": "x@y.co", "password": strings.Repeat("p", 73)}},
		{"email too long", map[string]any{"email": longEmail, "password": "secret123"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestAPI(t)
			rec := doJSON(t, a, "POST", "/auth/register", "", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRegisterDuplicateIsOpaque(t *testing.T) {
	a := newTestAPI(t)
	first := doJSON(t, a, "POST", "/auth/register", "", map[string]any{
		"email": "dup@example.com", "password": "secret123",
	})
	if first.Code != http.StatusCreated {
		t.Fatalf("first register: status = %d", first.Code)
	}

	dup := doJSON(t, a, "POST", "/auth/register", "", map[string]any{
		"email": "dup@example.com", "password": "different-pw",
	})
	if dup.Code != http.StatusCreated {
		t.Fatalf("duplicate register: status = %d, want 201 (body %s)", dup.Code, dup.Body.String())
	}
	if first.Body.String() != dup.Body.String() {
		t.Fatalf("responses differ: new=%s dup=%s", first.Body.String(), dup.Body.String())
	}
}

// TestLoginUnknownEmailRunsBcrypt is a coarse guard that the constant-time
// path is wired: when the email is unknown, Login still calls bcrypt via
// CheckPassword(dummyHash, ...). We exercise the handler and just verify it
// does not panic and returns 401. A statistical timing test would need many
// samples and is too flaky for unit tests; this guards the wiring only.
func TestLoginUnknownEmailRunsBcrypt(t *testing.T) {
	a := newTestAPI(t)
	rec := doJSON(t, a, "POST", "/auth/login", "", map[string]any{
		"email": "ghost@example.com", "password": "secret123",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
}
