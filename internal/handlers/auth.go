package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/models"
)

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// Health reports liveness, build version, and database reachability.
func (a *API) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()

	status := "ok"
	code := http.StatusOK
	if err := a.DB.PingContext(ctx); err != nil {
		status = "degraded"
		code = http.StatusServiceUnavailable
		slog.Error("health: db ping", "error", err)
	}
	writeJSON(w, code, map[string]string{
		"status":  status,
		"version": a.Version,
	})
}

// Me returns the authenticated user's public profile.
func (a *API) Me(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"id":    u.ID,
		"email": u.Email,
		"role":  u.Role,
	})
}

// Register creates a new buyer account.
func (a *API) Register(w http.ResponseWriter, r *http.Request) {
	a.register(w, r, "buyer")
}

// RegisterSeller creates a new seller account that can publish and manage its
// own cars.
func (a *API) RegisterSeller(w http.ResponseWriter, r *http.Request) {
	a.register(w, r, "seller")
}

func (a *API) register(w http.ResponseWriter, r *http.Request, role string) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeBody(w, r, &in); err != nil {
		writeBodyErr(w, err)
		return
	}

	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if !emailRe.MatchString(in.Email) {
		writeError(w, http.StatusBadRequest, "invalid email")
		return
	}
	if !lenBetween(in.Email, 1, maxEmailLen) {
		writeError(w, http.StatusBadRequest, "email is too long")
		return
	}
	if !lenBetween(in.Password, minPasswordLen, maxPasswordLen) {
		writeError(w, http.StatusBadRequest, "password must be 8-72 characters")
		return
	}

	hash, err := a.Auth.HashPassword(in.Password)
	if err != nil {
		serverError(w, err)
		return
	}

	// The response intentionally omits the user id: returning id=0 for a
	// duplicate and the real id for a new account would let an attacker
	// enumerate registered emails. Both paths return the identical body.
	_, err = a.DB.ExecContext(r.Context(),
		`INSERT INTO users (email, password_hash, role) VALUES (?, ?, ?)`, in.Email, hash, role)
	if err != nil {
		if !isUniqueViolation(err) {
			serverError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"email": in.Email})
}

// Logout revokes the bearer token used on the request by inserting its
// jti into revoked_tokens. The same token (or any token with the same
// jti) is rejected with 401 by RequireAuth from then on. Other tokens
// for the same user keep working: revocation is per-token, not per-user.
func (a *API) Logout(w http.ResponseWriter, r *http.Request) {
	status, msg := a.Auth.RevokeFromRequest(r)
	switch status {
	case http.StatusOK:
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case http.StatusUnauthorized, http.StatusBadRequest:
		writeError(w, status, msg)
	default:
		serverError(w, nil)
	}
}

// dummyHash is a fixed bcrypt hash used by Login to keep the response time
// constant whether the email exists or not, so an attacker cannot enumerate
// registered accounts by measuring timing. Generated once at startup with
// default cost so CompareHashAndPassword takes the same time as a real check.
var dummyHash string

func init() {
	const decoy = "decoy-password-for-timing-equality"
	a := auth.New("dummy", nil)
	h, err := a.HashPassword(decoy)
	if err != nil {
		panic("dummy hash: " + err.Error())
	}
	dummyHash = h
}

// Login validates credentials and returns an access+refresh token pair.
func (a *API) Login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeBody(w, r, &in); err != nil {
		writeBodyErr(w, err)
		return
	}

	var u models.User
	err := a.DB.QueryRowContext(r.Context(),
		`SELECT id, email, password_hash, role FROM users WHERE email = ?`,
		strings.ToLower(strings.TrimSpace(in.Email))).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role)
	if errors.Is(err, sql.ErrNoRows) {
		// Run a bcrypt comparison against a fixed dummy hash so the
		// response time is independent of whether the email exists.
		_ = a.Auth.CheckPassword(dummyHash, in.Password)
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if !a.Auth.CheckPassword(u.PasswordHash, in.Password) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := a.Auth.SignToken(&u)
	if err != nil {
		serverError(w, err)
		return
	}
	refresh, err := a.Auth.SignRefreshToken(&u)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token, "refresh_token": refresh})
}

// Refresh swaps a single-use refresh token for a fresh access+refresh pair.
// The presented token is revoked (rotation); replaying it returns 401.
func (a *API) Refresh(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := decodeBody(w, r, &in); err != nil {
		writeBodyErr(w, err)
		return
	}
	if strings.TrimSpace(in.RefreshToken) == "" {
		writeError(w, http.StatusBadRequest, "refresh_token is required")
		return
	}
	access, refresh, err := a.Auth.RotateRefresh(r.Context(), in.RefreshToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": access, "refresh_token": refresh})
}
