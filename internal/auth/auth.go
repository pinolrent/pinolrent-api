package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/pinolrent/pinolrent-api/internal/models"
)

type Auth struct {
	secret []byte
	db     *sql.DB
}

func New(secret string, d *sql.DB) *Auth {
	return &Auth{secret: []byte(secret), db: d}
}

type Claims struct {
	UserID int64  `json:"uid"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

func (a *Auth) HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func (a *Auth) CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

func (a *Auth) SignToken(u *models.User) (string, error) {
	claims := Claims{
		UserID: u.ID,
		Role:   u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(u.ID, 10),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.secret)
}

func (a *Auth) parseToken(token string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return a.secret, nil
	})
	if err != nil {
		return nil, err
	}
	return claims, nil
}

type ctxKey int

const userKey ctxKey = 0

func CurrentUser(ctx context.Context) (*models.User, bool) {
	u, ok := ctx.Value(userKey).(*models.User)
	return u, ok
}

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
		err = a.db.QueryRow(`SELECT id, email, password_hash, role FROM users WHERE id = ?`, claims.UserID).
			Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "user not found")
			return
		}

		ctx := context.WithValue(r.Context(), userKey, &u)
		next(w, r.WithContext(ctx))
	}
}

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