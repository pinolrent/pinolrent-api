package handlers

import (
	"database/sql"
	"net/http"
	"regexp"
	"strings"

	"github.com/pinolrent/pinolrent-api/internal/models"
)

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// Health reports that the server is up.
func (a *API) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Register creates a new client account.
func (a *API) Register(w http.ResponseWriter, r *http.Request) {
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
	if len(in.Password) < 6 {
		writeError(w, http.StatusBadRequest, "password must be at least 6 characters")
		return
	}

	hash, err := a.Auth.HashPassword(in.Password)
	if err != nil {
		serverError(w, err)
		return
	}

	res, err := a.DB.ExecContext(r.Context(),
		`INSERT INTO users (email, password_hash, role) VALUES (?, ?, 'client')`, in.Email, hash)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "email already registered")
			return
		}
		serverError(w, err)
		return
	}

	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "email": in.Email})
}

// Login validates credentials and returns a signed token.
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
	if err == sql.ErrNoRows {
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
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}
