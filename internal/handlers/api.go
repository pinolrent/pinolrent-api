// Package handlers wires the API routes to their HTTP handlers and provides
// shared request/response helpers.
package handlers

import (
	"database/sql"
	"net"
	"strings"
	"time"

	"github.com/pinolrent/pinolrent-api/internal/auth"
)

// API bundles the shared dependencies used by every handler: the database
// pool, the auth provider and the operational settings wired at startup.
type API struct {
	DB             *sql.DB
	Auth           *auth.Auth
	Version        string
	UploadDir      string
	UploadMaxTotal int64
	TrustedProxies []*net.IPNet
	// AdminEmails is the set of lower-cased addresses from ADMIN_EMAILS.
	// Registration consults it so an administrator account created after the
	// last startup already carries the role.
	AdminEmails map[string]bool
	// PhoneCountryPrefix and PhoneNationalLen say what a bare phone number
	// means: it is completed with +PhoneCountryPrefix and must have
	// PhoneNationalLen digits. New defaults them to Nicaragua (+505, 8);
	// cmd/api overwrites them from PHONE_COUNTRY_PREFIX / PHONE_NATIONAL_LEN.
	PhoneCountryPrefix string
	PhoneNationalLen   int
	// Location is the business time zone: the calendar day for the past-date
	// check and the future-reservation guard. Nil means UTC, which is what
	// tests get from New.
	Location *time.Location
}

// New returns an API bound to the given database pool and auth provider.
func New(db *sql.DB, a *auth.Auth) *API {
	return &API{DB: db, Auth: a, PhoneCountryPrefix: "505", PhoneNationalLen: 8}
}

// isAdminEmail reports whether the address is on the administrator
// allow-list. Accounts store their email lower-cased, so the comparison is too.
func (a *API) isAdminEmail(email string) bool {
	return a.AdminEmails[strings.ToLower(strings.TrimSpace(email))]
}
