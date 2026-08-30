package auth

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/pinolrent/pinolrent-api/internal/db"
	"github.com/pinolrent/pinolrent-api/internal/models"
)

const testSecret = "test-secret-32-bytes-minimum-okay"

func TestMain(m *testing.M) {
	// Silence the package's slog output during tests so the per-test
	// "migration applied" info logs do not clutter -v output.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.Run()
}

// newTestAuth returns an Auth backed by a fresh in-memory DB with the
// migrations applied.
func newTestAuth(t *testing.T) *Auth {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return New(testSecret, d)
}

// seedUser inserts a user with the given role and returns its id.
func seedUser(t *testing.T, a *Auth, email, role string) int64 {
	t.Helper()
	res, err := a.db.ExecContext(context.Background(),
		`INSERT INTO users (email, password_hash, role) VALUES (?, 'hash', ?)`, email, role)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last id: %v", err)
	}
	return id
}

func TestHashPasswordRoundtrip(t *testing.T) {
	a := newTestAuth(t)

	hash, err := a.HashPassword("secret123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !a.CheckPassword(hash, "secret123") {
		t.Fatal("correct password rejected")
	}
	if a.CheckPassword(hash, "wrong") {
		t.Fatal("wrong password accepted")
	}
	if a.CheckPassword(hash, "") {
		t.Fatal("empty password accepted")
	}
}

// TestHashPasswordIsBcrypt guards against a regression to a non-bcrypt
// algorithm: bcrypt hashes always start with $2a$, $2b$ or $2y$.
func TestHashPasswordIsBcrypt(t *testing.T) {
	a := newTestAuth(t)
	hash, err := a.HashPassword("x")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if len(hash) < 4 || hash[:4] != "$2a$" && hash[:4] != "$2b$" && hash[:4] != "$2y$" {
		t.Fatalf("hash %q is not bcrypt", hash)
	}
}

func TestSignAndParseToken(t *testing.T) {
	a := newTestAuth(t)
	u := &models.User{ID: 42, Role: "buyer"}

	tok, err := a.SignToken(u)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	claims, err := a.parseToken(tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.UserID != 42 {
		t.Fatalf("UserID = %d, want 42", claims.UserID)
	}
	if claims.Role != "buyer" {
		t.Fatalf("Role = %q, want buyer", claims.Role)
	}
	if claims.Subject != "42" {
		t.Fatalf("Subject = %q, want 42", claims.Subject)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != jwtAudience {
		t.Fatalf("Audience = %v, want [%s]", claims.Audience, jwtAudience)
	}
	if claims.Issuer != jwtIssuer {
		t.Fatalf("Issuer = %q, want %s", claims.Issuer, jwtIssuer)
	}
	if claims.ExpiresAt == nil {
		t.Fatal("ExpiresAt is nil")
	}
	if time.Until(claims.ExpiresAt.Time) > 25*time.Hour {
		t.Fatalf("ExpiresAt too far: %v", claims.ExpiresAt.Time)
	}
}

func TestParseTokenExpired(t *testing.T) {
	a := newTestAuth(t)

	claims := Claims{
		UserID: 1,
		Role:   "buyer",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    jwtIssuer,
			Audience:  jwt.ClaimStrings{jwtAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := a.parseToken(tok); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestParseTokenBadSignature(t *testing.T) {
	a := newTestAuth(t)
	other := New("another-secret-also-32-bytes-long-xxx", a.db)

	tok, err := other.SignToken(&models.User{ID: 1, Role: "buyer"})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := a.parseToken(tok); err == nil {
		t.Fatal("token signed with different secret accepted")
	}
}

func TestParseTokenWrongAlg(t *testing.T) {
	a := newTestAuth(t)

	claims := Claims{
		UserID: 1,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    jwtIssuer,
			Audience:  jwt.ClaimStrings{jwtAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS384, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := a.parseToken(tok); err == nil {
		t.Fatal("HS384 token accepted by HS256-only parser")
	}
}

func TestParseTokenMissingExp(t *testing.T) {
	a := newTestAuth(t)

	claims := Claims{
		UserID: 1,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:   jwtIssuer,
			Audience: jwt.ClaimStrings{jwtAudience},
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := a.parseToken(tok); err == nil {
		t.Fatal("token without exp accepted")
	}
}

func TestParseTokenMissingIssuer(t *testing.T) {
	a := newTestAuth(t)

	claims := Claims{
		UserID: 1,
		RegisteredClaims: jwt.RegisteredClaims{
			Audience:  jwt.ClaimStrings{jwtAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := a.parseToken(tok); err == nil {
		t.Fatal("token without iss accepted")
	}
}

func TestParseTokenMissingAudience(t *testing.T) {
	a := newTestAuth(t)

	claims := Claims{
		UserID: 1,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    jwtIssuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := a.parseToken(tok); err == nil {
		t.Fatal("token without aud accepted")
	}
}

func TestParseTokenMalformed(t *testing.T) {
	a := newTestAuth(t)
	for _, s := range []string{"", "not-a-jwt", "a.b.c", "..."} {
		if _, err := a.parseToken(s); err == nil {
			t.Fatalf("malformed token %q accepted", s)
		}
	}
}

// runHandler builds the middleware-under-test around an observer handler,
// executes it with an optional Authorization header, and returns the
// response plus whatever user the middleware placed in the request context.
//
// The first argument is a builder that takes a downstream handler and
// returns the wrapped handler to exercise. This matches the shape of
// a.RequireAuth and a.RequireRole, which both have the form
// `func(downstream) -> wrapped`.
func runHandler(build func(http.HandlerFunc) http.HandlerFunc, header string) (*httptest.ResponseRecorder, *models.User) {
	var seen *models.User
	downstream := func(w http.ResponseWriter, r *http.Request) {
		seen, _ = CurrentUser(r.Context())
		w.WriteHeader(http.StatusOK)
	}
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	rec := httptest.NewRecorder()
	build(downstream).ServeHTTP(rec, req)
	return rec, seen
}

func TestRequireAuthMissingHeader(t *testing.T) {
	a := newTestAuth(t)
	rec, _ := runHandler(a.RequireAuth, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), "missing bearer token") {
		t.Fatalf("body = %s, want 'missing bearer token'", rec.Body.String())
	}
}

func TestRequireAuthBadHeader(t *testing.T) {
	a := newTestAuth(t)
	for _, h := range []string{"Token abc", "Bearer", "Bearer  ", "bearer abc"} {
		// "bearer abc" has the right scheme (lowercase) but is rejected by
		// the strict prefix check; the others are not Bearer at all.
		rec, _ := runHandler(a.RequireAuth, h)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("header %q: status = %d, want 401 (body %s)", h, rec.Code, rec.Body.String())
		}
	}
}

func TestRequireAuthInvalidToken(t *testing.T) {
	a := newTestAuth(t)
	rec, _ := runHandler(a.RequireAuth, "Bearer not-a-jwt")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !contains(rec.Body.String(), "invalid or expired token") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestRequireAuthUserNotFound(t *testing.T) {
	a := newTestAuth(t)
	// Sign a token for a user that does not exist in the DB.
	tok, err := a.SignToken(&models.User{ID: 9999, Role: "buyer"})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	rec, _ := runHandler(a.RequireAuth, "Bearer "+tok)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), "user not found") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestRequireAuthHappyPath(t *testing.T) {
	a := newTestAuth(t)
	id := seedUser(t, a, "u@example.com", "buyer")
	tok, err := a.SignToken(&models.User{ID: id, Role: "buyer"})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	rec, seen := runHandler(a.RequireAuth, "Bearer "+tok)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if seen == nil {
		t.Fatal("user not placed in context")
	}
	if seen.ID != id {
		t.Fatalf("context user id = %d, want %d", seen.ID, id)
	}
	if seen.Role != "buyer" {
		t.Fatalf("context user role = %q, want buyer", seen.Role)
	}
}

func TestRequireRoleWrongRole(t *testing.T) {
	a := newTestAuth(t)
	id := seedUser(t, a, "buyer@example.com", "buyer")
	tok, err := a.SignToken(&models.User{ID: id, Role: "buyer"})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	rec, _ := runHandler(func(next http.HandlerFunc) http.HandlerFunc {
		return a.RequireRole("seller", next)
	}, "Bearer "+tok)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), "insufficient permissions") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestRequireRoleHappyPath(t *testing.T) {
	a := newTestAuth(t)
	id := seedUser(t, a, "seller@example.com", "seller")
	tok, err := a.SignToken(&models.User{ID: id, Role: "seller"})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	rec, seen := runHandler(func(next http.HandlerFunc) http.HandlerFunc {
		return a.RequireRole("seller", next)
	}, "Bearer "+tok)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if seen == nil || seen.Role != "seller" {
		t.Fatalf("user in context: %+v", seen)
	}
}

func TestRequireRoleUnauthenticated(t *testing.T) {
	a := newTestAuth(t)
	rec, _ := runHandler(func(next http.HandlerFunc) http.HandlerFunc {
		return a.RequireRole("seller", next)
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestCurrentUserAbsent(t *testing.T) {
	if u, ok := CurrentUser(context.Background()); ok || u != nil {
		t.Fatalf("CurrentUser on bare context = (%+v, %v), want (nil, false)", u, ok)
	}
}

// TestErrorResponseIsJSON checks the contract that all auth errors are
// returned as application/json (not http.Error's text/plain).
func TestErrorResponseIsJSON(t *testing.T) {
	a := newTestAuth(t)
	rec, _ := runHandler(a.RequireAuth, "")
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %s is not JSON: %v", rec.Body.String(), err)
	}
	if _, ok := body["error"]; !ok {
		t.Fatalf("body %s missing 'error' field", rec.Body.String())
	}
}

// contains is a tiny helper to avoid pulling in strings just for a few checks.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}