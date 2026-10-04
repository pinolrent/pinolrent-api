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
	"github.com/pinolrent/pinolrent-api/internal/db"
	"github.com/pinolrent/pinolrent-api/internal/models"
)

// emailRe is a pragmatic (not fully RFC 5322) validation: local and domain
// labels cannot start or end with a dot or hyphen, no consecutive dots, and
// the TLD has at least 2 characters.
var emailRe = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+/=?^_` + "`" + `{|}~-]+(?:\.[A-Za-z0-9!#$%&'*+/=?^_` + "`" + `{|}~-]+)*@[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)*\.[A-Za-z]{2,}$`)

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
		"roles": u.Roles,
		"phone": u.Phone,
	})
}

// UpdateMe updates the authenticated user's own profile. Only the phone is
// mutable: the email identifies the account and roles are granted at
// registration or via become-seller.
func (a *API) UpdateMe(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	var in struct {
		Phone string `json:"phone"`
	}
	if err := decodeBody(w, r, &in); err != nil {
		writeBodyErr(w, err)
		return
	}

	phone, ok := normalizePhone(in.Phone)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid phone")
		return
	}
	// Same rule as registration: a seller without a phone cannot be contacted.
	// Saving a phone here never grants the seller role; only become-seller
	// does that.
	if u.HasRole(db.RoleSeller) && phone == "" {
		writeError(w, http.StatusBadRequest, "phone is required for sellers")
		return
	}

	if _, err := a.DB.ExecContext(r.Context(),
		`UPDATE users SET phone = ? WHERE id = ?`, phone, u.ID); err != nil {
		serverError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":    u.ID,
		"email": u.Email,
		"roles": u.Roles,
		"phone": phone,
	})
}

// UpdatePassword changes the authenticated user's password. It also stamps
// token_valid_after, which revokes every session of the user — including the
// token that made this request — so the client must log in again with the new
// password. That is the whole point: a password change is an account-takeover
// response, and stale tokens must not survive it.
func (a *API) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	var in struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeBody(w, r, &in); err != nil {
		writeBodyErr(w, err)
		return
	}
	if !lenBetween(in.NewPassword, minPasswordLen, maxPasswordLen) {
		writeError(w, http.StatusBadRequest, "password must be 8-72 characters")
		return
	}
	// The caller is authenticated, but proving the current password keeps a
	// stolen session from silently becoming permanent.
	if !a.Auth.CheckPassword(u.PasswordHash, in.CurrentPassword) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	hash, err := a.Auth.HashPassword(in.NewPassword)
	if err != nil {
		serverError(w, err)
		return
	}
	if _, err := a.DB.ExecContext(r.Context(),
		`UPDATE users SET password_hash = ?, token_valid_after = ? WHERE id = ?`,
		hash, time.Now().Unix(), u.ID); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Register creates a new account. Without a phone it is buyer-only; with a
// valid phone it is buyer and seller from the start. A buyer-only account
// can become a seller later via become-seller.
func (a *API) Register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Phone    string `json:"phone"`
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

	phone, ok := normalizePhone(in.Phone)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid phone")
		return
	}

	hash, err := a.Auth.HashPassword(in.Password)
	if err != nil {
		serverError(w, err)
		return
	}

	roles := []string{db.RoleBuyer}
	if phone != "" {
		roles = append(roles, db.RoleSeller)
	}
	// An address on ADMIN_EMAILS registers as an administrator right away, so
	// a new admin account does not need a restart to become usable. The role
	// is additive: the account stays a working buyer, and only the allow-list
	// ever grants it.
	if a.isAdminEmail(in.Email) {
		roles = append(roles, db.RoleAdmin)
	}

	// The response intentionally omits the user id: returning id=0 for a
	// duplicate and the real id for a new account would let an attacker
	// enumerate registered emails. Both paths return the identical body.
	registered := false
	err = db.WithImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()

		res, err := conn.ExecContext(ctx,
			`INSERT INTO users (email, password_hash, phone) VALUES (?, ?, ?)`,
			in.Email, hash, phone)
		if err != nil {
			if !isUniqueViolation(err) {
				serverError(w, err)
				return db.ErrTxHandled
			}
			writeJSON(w, http.StatusCreated, map[string]any{"email": in.Email})
			return db.ErrTxHandled
		}
		id, err := res.LastInsertId()
		if err != nil {
			serverError(w, err)
			return db.ErrTxHandled
		}
		for _, role := range roles {
			if _, err := conn.ExecContext(ctx,
				`INSERT INTO user_roles (user_id, role) VALUES (?, ?)`, id, role); err != nil {
				serverError(w, err)
				return db.ErrTxHandled
			}
		}
		registered = true
		return nil
	})
	if err != nil {
		serverError(w, err)
		return
	}
	if !registered {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"email": in.Email})
}

// BecomeSeller grants the seller role to the authenticated buyer account and
// stores its contact phone. It is idempotent: a seller calling it again gets
// its current profile back.
func (a *API) BecomeSeller(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	var in struct {
		Phone string `json:"phone"`
	}
	if err := decodeBody(w, r, &in); err != nil {
		writeBodyErr(w, err)
		return
	}

	phone, ok := normalizePhone(in.Phone)
	if !ok || phone == "" {
		writeError(w, http.StatusBadRequest, "phone is required for sellers")
		return
	}

	if !u.HasRole(db.RoleSeller) {
		tx, err := a.DB.BeginTx(r.Context(), nil)
		if err != nil {
			serverError(w, err)
			return
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()
		if _, err := tx.ExecContext(r.Context(),
			`UPDATE users SET phone = ? WHERE id = ?`, phone, u.ID); err != nil {
			serverError(w, err)
			return
		}
		if _, err := tx.ExecContext(r.Context(),
			`INSERT OR IGNORE INTO user_roles (user_id, role) VALUES (?, ?)`, u.ID, db.RoleSeller); err != nil {
			serverError(w, err)
			return
		}
		if err := tx.Commit(); err != nil {
			serverError(w, err)
			return
		}
		committed = true
		u.Phone = phone
		u.Roles = append(u.Roles, db.RoleSeller)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":    u.ID,
		"email": u.Email,
		"roles": u.Roles,
		"phone": u.Phone,
	})
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
		`SELECT id, email, password_hash, phone FROM users WHERE email = ?`,
		strings.ToLower(strings.TrimSpace(in.Email))).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Phone)
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
	roles, err := a.Auth.UserRoles(r.Context(), u.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	u.Roles = roles

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
