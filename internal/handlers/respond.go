// Package handlers wires the API routes to their HTTP handlers and provides
// shared request/response helpers.
package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/pinolrent/pinolrent-api/internal/auth"
)

const maxBodyBytes = 1 << 20

// Field length caps. Per-request body size is bounded separately by
// maxBodyBytes; these caps reject oversized individual fields before they
// reach the database.
const (
	maxEmailLen    = 254 // RFC 5321
	minPasswordLen = 8
	maxPasswordLen = 72 // bcrypt silently truncates beyond this
	maxNameLen     = 200
	maxURLLen      = 2048
)

// lenBetween reports whether s length is within [minLen, maxLen] inclusive.
// maxLen <= 0 means "no upper bound".
func lenBetween(s string, minLen, maxLen int) bool {
	if len(s) < minLen {
		return false
	}
	if maxLen > 0 && len(s) > maxLen {
		return false
	}
	return true
}

// API bundles the shared dependencies used by every handler: the database
// pool and the auth provider.
type API struct {
	DB      *sql.DB
	Auth    *auth.Auth
	Version string
}

// New returns an API bound to the given database pool and auth provider.
func New(db *sql.DB, a *auth.Auth) *API {
	return &API{DB: db, Auth: a}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func serverError(w http.ResponseWriter, err error) {
	slog.Error("internal error", "error", err)
	writeError(w, http.StatusInternalServerError, "server error")
}

var errBodyTooLarge = errors.New("request body too large")

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return errBodyTooLarge
		}
		return errors.New("invalid JSON body")
	}
	return nil
}

func writeBodyErr(w http.ResponseWriter, err error) {
	if errors.Is(err, errBodyTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid JSON body")
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

const (
	defaultPageLimit = 50
	maxPageLimit     = 200
)

// paginate extracts the limit/offset query params with defaults. It returns a
// non-empty error message when the caller provided invalid values.
func paginate(r *http.Request) (limit, offset int, errMsg string) {
	limit = defaultPageLimit
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxPageLimit {
			return 0, 0, "invalid limit"
		}
		limit = n
	}
	if s := r.URL.Query().Get("offset"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return 0, 0, "invalid offset"
		}
		offset = n
	}
	return limit, offset, ""
}

func validURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Host != ""
}
