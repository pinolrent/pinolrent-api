package handlers

import (
	"context"
	"database/sql"
	"strings"

	"github.com/pinolrent/pinolrent-api/internal/models"
)

// accountView is the shared account shape of the profile endpoints and the
// admin user endpoints. One struct keeps the six responses from drifting as
// fields are added. Roles is never null and SuspendedAt stays out while the
// account is in good standing.
type accountView struct {
	ID          int64    `json:"id"`
	Email       string   `json:"email"`
	Phone       string   `json:"phone"`
	Roles       []string `json:"roles"`
	SuspendedAt int64    `json:"suspended_at,omitempty"`
}

// accountViewFrom builds the view from a user row. suspendedAt is the raw
// suspended_at column: 0 means the account is not suspended.
func accountViewFrom(u *models.User, suspendedAt int64) accountView {
	roles := u.Roles
	if roles == nil {
		roles = []string{}
	}
	return accountView{ID: u.ID, Email: u.Email, Phone: u.Phone, Roles: roles, SuspendedAt: suspendedAt}
}

// adminUserView loads one account in the shape the admin endpoints return.
func (a *API) adminUserView(ctx context.Context, id int64) (accountView, error) {
	var email, phone string
	var suspended sql.NullInt64
	var rolesStr sql.NullString
	err := a.DB.QueryRowContext(ctx,
		`SELECT u.email, u.phone, u.suspended_at, GROUP_CONCAT(ur.role)
		 FROM users u
		 LEFT JOIN user_roles ur ON ur.user_id = u.id
		 WHERE u.id = ?
		 GROUP BY u.id, u.email, u.phone, u.suspended_at`, id).Scan(&email, &phone, &suspended, &rolesStr)
	if err != nil {
		return accountView{}, err
	}
	var roles []string
	if rolesStr.Valid && rolesStr.String != "" {
		roles = strings.Split(rolesStr.String, ",")
	}
	u := models.User{ID: id, Email: email, Phone: phone, Roles: roles}
	return accountViewFrom(&u, suspended.Int64), nil
}
