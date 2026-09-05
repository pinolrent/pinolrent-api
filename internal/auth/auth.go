// Package auth provides bcrypt password hashing, JWT signing/validation, and
// HTTP middleware that identifies the current user from the Authorization
// header.
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/pinolrent/pinolrent-api/internal/models"
)

// jwtIssuer and jwtAudience are the iss/aud claims set on every token and
// required by the parser. They scope tokens to this service so a token signed
// with the same secret by another service is rejected.
const (
	jwtIssuer   = "pinolrent-api"
	jwtAudience = "pinolrent-api"
)

// Auth signs and validates HS256 JWTs and provides HTTP auth middleware.
type Auth struct {
	secret []byte
	db     *sql.DB
}

// New returns an Auth that signs tokens with the given secret and looks up
// users in the provided database.
func New(secret string, d *sql.DB) *Auth {
	return &Auth{secret: []byte(secret), db: d}
}

// Claims is the JWT payload carried by issued tokens.
type Claims struct {
	UserID int64  `json:"uid"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// JTI returns the token's unique identifier, or "" if missing. Callers
// that need the value should use this helper instead of reaching into
// RegisteredClaims.ID directly so tests can mock it cleanly.
func (c *Claims) JTI() string {
	return c.ID
}

// ExpiresAtUnix returns the token's expiry as a Unix timestamp, or 0 if
// the claim is missing. Used to store the expiry in revoked_tokens so the
// GC can drop rows once the token would have expired anyway.
func (c *Claims) ExpiresAtUnix() int64 {
	if c.ExpiresAt == nil {
		return 0
	}
	return c.ExpiresAt.Unix()
}

// HashPassword returns the bcrypt hash of pw.
func (a *Auth) HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword reports whether pw matches the given bcrypt hash.
func (a *Auth) CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// SignToken issues a signed token for the user, valid for 24 hours. Every
// token carries a unique jti (UUID) so it can be revoked before its natural
// expiry via /auth/logout or any future invalidation path.
func (a *Auth) SignToken(u *models.User) (string, error) {
	now := time.Now()
	jti, err := newJTI()
	if err != nil {
		return "", err
	}
	claims := Claims{
		UserID: u.ID,
		Role:   u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(u.ID, 10),
			Issuer:    jwtIssuer,
			Audience:  jwt.ClaimStrings{jwtAudience},
			ID:        jti,
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.secret)
}

// newJTI returns a 16-byte random identifier encoded as 32 hex characters.
func newJTI() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// tokenParser enforces the signing algorithm, requires the iss/aud claims,
// and rejects tokens without an expiry. Built once and reused for every
// parse so each check is done by the library, not the keyfunc.
var tokenParser = jwt.NewParser(
	jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	jwt.WithIssuer(jwtIssuer),
	jwt.WithAudience(jwtAudience),
	jwt.WithExpirationRequired(),
)

func (a *Auth) parseToken(token string) (*Claims, error) {
	claims := &Claims{}
	//nolint:revive // t is part of the jwt.Keyfunc signature, even if unused here
	_, err := tokenParser.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		return a.secret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims.JTI() == "" {
		return nil, jwt.ErrTokenRequiredClaimMissing
	}
	return claims, nil
}

type ctxKey int

const userKey ctxKey = 0

// CurrentUser returns the user stored in the request context, if any.
func CurrentUser(ctx context.Context) (*models.User, bool) {
	u, ok := ctx.Value(userKey).(*models.User)
	return u, ok
}

// Revoke marks the token identified by the given jti as no longer valid
// until its natural expiry. Subsequent requests carrying the same token
// (or a token with the same jti) are rejected by RequireAuth with 401.
//
// The expires_at argument is the Unix timestamp copied from the token's
// exp claim; the revoked_tokens GC can drop the row once that time passes.
func (a *Auth) Revoke(ctx context.Context, userID int64, jti string, expiresAtUnix int64) error {
	_, err := a.db.ExecContext(ctx,
		`INSERT INTO revoked_tokens (jti, user_id, expires_at) VALUES (?, ?, ?)`,
		jti, userID, expiresAtUnix)
	return err
}

// RevokeFromRequest parses the bearer token from r and inserts its jti
// into revoked_tokens so it cannot be reused. Returns the (httpStatus,
// message) pair to write back. Use this from the /auth/logout handler so
// the caller can rely on a single helper instead of re-implementing the
// header parsing and jti extraction.
func (a *Auth) RevokeFromRequest(r *http.Request) (status int, msg string) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || strings.TrimSpace(token) == "" {
		return http.StatusUnauthorized, "missing bearer token"
	}
	claims, err := a.parseToken(strings.TrimSpace(token))
	if err != nil {
		return http.StatusUnauthorized, "invalid or expired token"
	}
	if claims.JTI() == "" {
		// Tokens issued before the jti migration (or with a custom
		// parser) cannot be revoked individually; treat as bad
		// request so the operator knows to rotate the secret instead.
		return http.StatusBadRequest, "token cannot be revoked"
	}
	if err := a.Revoke(r.Context(), claims.UserID, claims.JTI(), claims.ExpiresAtUnix()); err != nil {
		return http.StatusInternalServerError, "server error"
	}
	return http.StatusOK, ""
}

// GCRevoked drops rows from revoked_tokens whose tokens would have
// already expired. Runs in a background goroutine that owns the request
// context for the lifetime of the process, so passing it here lets the
// GC honor the same cancellation signal as the HTTP server.
func (a *Auth) GCRevoked(ctx context.Context) error {
	_, err := a.db.ExecContext(ctx, `DELETE FROM revoked_tokens WHERE expires_at < ?`, time.Now().Unix())
	return err
}

// IsRevoked reports whether the given jti is in the revoked_tokens table.
func (a *Auth) IsRevoked(ctx context.Context, jti string) (bool, error) {
	var n int
	err := a.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM revoked_tokens WHERE jti = ?`, jti).Scan(&n)
	return n > 0, err
}

// RequireAuth wraps a handler so it only runs for valid, non-expired tokens.
// The authenticated user is added to the request context.
func (a *Auth) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}

		claims, err := a.parseToken(strings.TrimSpace(token))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		// Reject tokens that have been revoked before their natural
		// expiry. The lookup is on the jti (primary key) so it is a
		// single index hit. Same response shape as "user not found" so
		// we do not leak whether the jti was real.
		revoked, err := a.IsRevoked(r.Context(), claims.JTI())
		if err != nil {
			serverErrorFromAuth(w, err)
			return
		}
		if revoked {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		var u models.User
		err = a.db.QueryRowContext(r.Context(),
			`SELECT id, email, password_hash, role FROM users WHERE id = ?`, claims.UserID).
			Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "user not found")
			return
		}

		ctx := context.WithValue(r.Context(), userKey, &u)
		next(w, r.WithContext(ctx))
	}
}

// serverErrorFromAuth is the auth package's analogue of handlers.serverError:
// it logs the error and returns 500. The package is intentionally lean and
// does not import handlers, so the helper is inlined.
func serverErrorFromAuth(w http.ResponseWriter, err error) {
	slog.Error("auth internal error", "error", err)
	writeError(w, http.StatusInternalServerError, "server error")
}

// RequireRole wraps a handler so it only runs for authenticated users with the
// given role.
func (a *Auth) RequireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return a.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		u, ok := CurrentUser(r.Context())
		if !ok || u.Role != role {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
		next(w, r)
	})
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
