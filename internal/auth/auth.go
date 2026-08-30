// Package auth provides bcrypt password hashing, JWT signing/validation, and
// HTTP middleware that identifies the current user from the Authorization
// header.
package auth

import (
	"context"
	"database/sql"
	"encoding/json"
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

// HashPassword returns the bcrypt hash of pw.
func (a *Auth) HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword reports whether pw matches the given bcrypt hash.
func (a *Auth) CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// SignToken issues a signed token for the user, valid for 24 hours.
func (a *Auth) SignToken(u *models.User) (string, error) {
	claims := Claims{
		UserID: u.ID,
		Role:   u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(u.ID, 10),
			Issuer:    jwtIssuer,
			Audience:  jwt.ClaimStrings{jwtAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.secret)
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
	_, err := tokenParser.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		return a.secret, nil
	})
	if err != nil {
		return nil, err
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
