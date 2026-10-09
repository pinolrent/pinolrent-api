// Package auth provides bcrypt password hashing, JWT signing/validation, and
// HTTP middleware that identifies the current user from the Authorization
// header.
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/pinolrent/pinolrent-api/internal/httpx"
	"github.com/pinolrent/pinolrent-api/internal/models"
)

// jwtIssuer and jwtAudience scope tokens to this service so a token signed
// with the same secret by another service is rejected. Refresh tokens carry
// a distinct audience so they cannot be used as access tokens and vice versa.
const (
	jwtIssuer       = "pinolrent-api"
	jwtAudience     = "pinolrent-api"
	jwtRefreshAud   = "pinolrent-api-refresh"
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 7 * 24 * time.Hour
)

// Auth signs and validates HS256 JWTs and provides HTTP auth middleware.
type Auth struct {
	secret []byte
	db     *sql.DB
	// Cost is the bcrypt cost HashPassword uses. New sets
	// bcrypt.DefaultCost; tests lower it so the race-detector suite does
	// not spend its budget on blowfish work.
	Cost int
}

// New returns an Auth that signs tokens with the given secret and looks up
// users in the provided database.
func New(secret string, d *sql.DB) *Auth {
	return &Auth{secret: []byte(secret), db: d, Cost: bcrypt.DefaultCost}
}

// Claims is the JWT payload carried by issued tokens.
type Claims struct {
	UserID int64    `json:"uid"`
	Roles  []string `json:"roles"`
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
	b, err := bcrypt.GenerateFromPassword([]byte(pw), a.Cost)
	return string(b), err
}

// CheckPassword reports whether pw matches the given bcrypt hash.
func (a *Auth) CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// SignToken issues a short-lived access token for the user. Every token
// carries a unique jti so it can be revoked before its natural expiry via
// /auth/logout.
func (a *Auth) SignToken(u *models.User) (string, error) {
	return a.sign(u, jwtAudience, accessTokenTTL)
}

// SignRefreshToken issues a single-use refresh token for the user. Present
// it to POST /auth/refresh to obtain a fresh access+refresh pair; the used
// refresh token is revoked (rotation) so a leaked one cannot be replayed.
func (a *Auth) SignRefreshToken(u *models.User) (string, error) {
	return a.sign(u, jwtRefreshAud, refreshTokenTTL)
}

func (a *Auth) sign(u *models.User, aud string, ttl time.Duration) (string, error) {
	now := time.Now()
	jti, err := newJTI()
	if err != nil {
		return "", err
	}
	claims := Claims{
		UserID: u.ID,
		Roles:  append([]string(nil), u.Roles...),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(u.ID, 10),
			Issuer:    jwtIssuer,
			Audience:  jwt.ClaimStrings{aud},
			ID:        jti,
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
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

var refreshParser = jwt.NewParser(
	jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	jwt.WithIssuer(jwtIssuer),
	jwt.WithAudience(jwtRefreshAud),
	jwt.WithExpirationRequired(),
)

func (a *Auth) parseToken(token string) (*Claims, error) {
	return a.parseWith(tokenParser, token)
}

// parseRefreshToken validates a refresh token. The distinct audience keeps
// access tokens from being accepted here and vice versa.
func (a *Auth) parseRefreshToken(token string) (*Claims, error) {
	return a.parseWith(refreshParser, token)
}

func (a *Auth) parseWith(p *jwt.Parser, token string) (*Claims, error) {
	claims := &Claims{}
	//nolint:revive // t is part of the jwt.Keyfunc signature, even if unused here
	_, err := p.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		return a.secret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims.JTI() == "" {
		return nil, jwt.ErrTokenRequiredClaimMissing
	}
	// Tokens issued before the roles migration carry no roles claim; reject
	// them so their owners re-authenticate instead of running roleless.
	if len(claims.Roles) == 0 {
		return nil, jwt.ErrTokenRequiredClaimMissing
	}
	return claims, nil
}

type ctxKey struct{}

var userKey = ctxKey{}

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
		`INSERT OR IGNORE INTO revoked_tokens (jti, user_id, expires_at) VALUES (?, ?, ?)`,
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

// UserRoles returns the role memberships of the user, alphabetically ordered.
func (a *Auth) UserRoles(ctx context.Context, userID int64) ([]string, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT role FROM user_roles WHERE user_id = ? ORDER BY role`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return roles, nil
}

// userByID loads the full user row, the one place the users column list lives.
// Roles come from user_roles ordered alphabetically.
func (a *Auth) userByID(ctx context.Context, id int64) (models.User, error) {
	var u models.User
	var validAfter, suspended sql.NullInt64
	err := a.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, phone, token_valid_after, suspended_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Phone, &validAfter, &suspended)
	if err != nil {
		return u, err
	}
	roles, err := a.UserRoles(ctx, id)
	if err != nil {
		return u, err
	}
	u.Roles = roles
	if validAfter.Valid {
		u.TokenValidAfter = validAfter.Int64
	}
	if suspended.Valid {
		u.SuspendedAt = suspended.Int64
	}
	return u, nil
}

// InvalidateUserTokens stamps token_valid_after, so every JWT issued before
// stamp stops validating — access and refresh alike. It is the "log out
// everywhere" switch: used when a refresh token is replayed. The caller passes
// the stamp because the wall clock can step backwards (NTP): a bare `now` can
// land at or before the iat of tokens issued moments ago and leave them alive.
func (a *Auth) InvalidateUserTokens(ctx context.Context, userID int64, stamp int64) error {
	_, err := a.db.ExecContext(ctx,
		`UPDATE users SET token_valid_after = ? WHERE id = ?`, stamp, userID)
	return err
}

// tokenSuperseded reports whether the token was issued before the user's
// token_valid_after stamp. The comparison is strict: a token issued in the
// same second as the stamp survives (iat is second-granular), which is the
// accepted one-second race — worst case an old token lives out its remaining
// 15 minutes, and a login racing a password change is never killed twice.
func tokenSuperseded(u *models.User, claims *Claims) bool {
	return u.TokenValidAfter > 0 && claims.IssuedAt != nil && claims.IssuedAt.Unix() < u.TokenValidAfter
}

// RotateRefresh validates a single-use refresh token and swaps it for a
// fresh access+refresh pair. Consuming the token is an atomic insert of its
// jti, so concurrent presentations yield at most one pair; a reuse attempt
// fails with errRefreshReused and revokes every session of the user.
func (a *Auth) RotateRefresh(ctx context.Context, token string) (access, refresh string, err error) {
	claims, err := a.parseRefreshToken(strings.TrimSpace(token))
	if err != nil {
		return "", "", err
	}
	u, err := a.userByID(ctx, claims.UserID)
	if err != nil {
		return "", "", err
	}
	if tokenSuperseded(&u, claims) {
		return "", "", errTokenSuperseded
	}
	// A suspended account holds no valid session: like Login and RequireAuth,
	// rotation refuses to mint new tokens for it.
	if u.SuspendedAt > 0 {
		return "", "", ErrAccountSuspended
	}
	// The replay check below is the one-time gate: a token is consumed by its
	// jti insert, and of several concurrent presentations exactly one inserts.
	// The rest hit the primary key and take the replay path.
	if err := a.claimJTI(ctx, claims.UserID, claims.JTI(), claims.ExpiresAtUnix()); err != nil {
		if !errors.Is(err, errJTIClaimed) {
			return "", "", err
		}
		// A replay is either a stolen refresh or a buggy client, and the
		// server cannot tell which side holds the valid copy — so every
		// session of this user dies and both sides must log in again.
		// Best effort: on failure the caller still gets the 401 below.
		// The stamp never precedes the replayed token's iat+1: the wall
		// clock can step backwards, and a stamp from `now` alone would
		// leave it and its same-second siblings alive.
		stamp := time.Now().Unix()
		if claims.IssuedAt != nil && claims.IssuedAt.Unix()+1 > stamp {
			stamp = claims.IssuedAt.Unix() + 1
		}
		if err := a.InvalidateUserTokens(ctx, claims.UserID, stamp); err != nil {
			slog.Error("revoke sessions on refresh replay", "user_id", claims.UserID, "error", err)
		}
		return "", "", errRefreshReused
	}
	access, err = a.SignToken(&u)
	if err != nil {
		return "", "", err
	}
	refresh, err = a.SignRefreshToken(&u)
	if err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

var errRefreshReused = errors.New("refresh token already used")

// errJTIClaimed reports that the jti is already in revoked_tokens.
var errJTIClaimed = errors.New("jti already claimed")

// claimJTI inserts the jti as revoked in one statement. A duplicate key means
// another request already consumed this token and yields errJTIClaimed.
func (a *Auth) claimJTI(ctx context.Context, userID int64, jti string, expiresAtUnix int64) error {
	_, err := a.db.ExecContext(ctx,
		`INSERT INTO revoked_tokens (jti, user_id, expires_at) VALUES (?, ?, ?)`,
		jti, userID, expiresAtUnix)
	if err != nil {
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY {
			return errJTIClaimed
		}
	}
	return err
}

// ErrAccountSuspended marks a token that is cryptographically fine but belongs
// to a suspended account. Handlers map it to 403, unlike the 401 of dead
// tokens, so the client learns the account — not the token — is the problem.
var ErrAccountSuspended = errors.New("account suspended")

// errTokenSuperseded marks a token that was cryptographically fine but issued
// before the user's revocation stamp; handlers map it to the same 401 as an
// expired token so nothing leaks about why it died.
var errTokenSuperseded = errors.New("token superseded by a newer credential event")

// IsRevoked reports whether the given jti is in the revoked_tokens table.
func (a *Auth) IsRevoked(ctx context.Context, jti string) (bool, error) {
	var exists int
	err := a.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM revoked_tokens WHERE jti = ?)`, jti).Scan(&exists)
	return exists == 1, err
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

		u, err := a.userByID(r.Context(), claims.UserID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "user not found")
			return
		}
		if tokenSuperseded(&u, claims) {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		// A suspended account is refused on every authenticated route, token
		// valid or not. Unlike token_valid_after this is read fresh from the
		// user row on each request, so suspending takes effect on the next
		// call and lifting it restores access without logging in again.
		if u.SuspendedAt > 0 {
			writeError(w, http.StatusForbidden, "account suspended")
			return
		}

		ctx := context.WithValue(r.Context(), userKey, &u)
		next(w, r.WithContext(ctx))
	}
}

func serverErrorFromAuth(w http.ResponseWriter, err error) {
	slog.Error("auth internal error", "error", err)
	httpx.WriteError(w, http.StatusInternalServerError, "server error")
}

// RequireRole wraps a handler so it only runs for authenticated users holding
// the given role.
func (a *Auth) RequireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return a.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		u, ok := CurrentUser(r.Context())
		if !ok || !u.HasRole(role) {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
		next(w, r)
	})
}

func writeError(w http.ResponseWriter, status int, msg string) {
	httpx.WriteError(w, status, msg)
}
