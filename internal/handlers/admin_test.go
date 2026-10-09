package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

var adminSeq atomic.Int64

// newAdmin registers a buyer account and grants it the admin role directly in
// the database, the way the ADMIN_EMAILS allow-list would. The endpoint tests
// need an administrator that does not depend on the allow-list wiring, which
// TestAdminAllowList* covers separately.
//
// The token is issued before the role is granted, so every test that uses this
// helper also proves the role is picked up from the database per request
// instead of being frozen in the token at login.
func newAdmin(t *testing.T, a *API) (token string, id int64) {
	t.Helper()
	adminSeq.Add(1)
	token = registerBuyer(t, a, fmt.Sprintf("admin-%d@example.com", adminSeq.Load()), "secret123")

	rec := doJSON(t, a, "GET", "/auth/me", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: status %d body %s", rec.Code, rec.Body.String())
	}
	var me struct {
		ID int64 `json:"id"`
	}
	decodeJSON(t, rec, &me)

	if _, err := a.DB.ExecContext(context.Background(),
		`INSERT OR IGNORE INTO user_roles (user_id, role) VALUES (?, 'admin')`, me.ID); err != nil {
		t.Fatalf("grant admin role: %v", err)
	}
	return token, me.ID
}

// userID resolves an account id from its email, for tests that only hold the
// token returned at registration.
func userID(t *testing.T, a *API, email string) int64 {
	t.Helper()
	var id int64
	if err := a.DB.QueryRowContext(context.Background(),
		`SELECT id FROM users WHERE email = ?`, email).Scan(&id); err != nil {
		t.Fatalf("find %s: %v", email, err)
	}
	return id
}

func userRoles(t *testing.T, a *API, id int64) []string {
	t.Helper()
	rows, err := a.DB.QueryContext(context.Background(),
		`SELECT role FROM user_roles WHERE user_id = ? ORDER BY role`, id)
	if err != nil {
		t.Fatalf("roles of %d: %v", id, err)
	}
	defer func() { _ = rows.Close() }()
	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			t.Fatalf("scan role: %v", err)
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return roles
}

// adminRoutes is every /admin endpoint with a request that has to be rejected
// before it reaches the handler.
var adminRoutes = []struct {
	method string
	path   string
	body   any
}{
	{"GET", "/admin/users", nil},
	{"GET", "/admin/users/1", nil},
	{"PATCH", "/admin/users/1", map[string]any{"suspended": true}},
	{"PATCH", "/admin/users/1/roles", map[string]any{"seller": true}},
	{"GET", "/admin/cars", nil},
	{"PATCH", "/admin/cars/1", map[string]any{"active": false}},
	{"DELETE", "/admin/cars/1", nil},
	{"GET", "/admin/reservations", nil},
	{"GET", "/admin/payments", nil},
	{"GET", "/admin/stats", nil},
	{"GET", "/admin/audit", nil},
}

// TestAdminRoutesRejectNonAdmins walks the whole admin surface and pins the
// authorization contract: no token is 401, and a valid token without the role
// is 403 whether the account is a plain buyer or also a seller.
func TestAdminRoutesRejectNonAdmins(t *testing.T) {
	a := newTestAPI(t)
	buyer := registerBuyer(t, a, "buyer@example.com", "secret123")
	seller := newSeller(t, a)

	for _, r := range adminRoutes {
		rec := doJSON(t, a, r.method, r.path, "", r.body)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token: status = %d, want 401", r.method, r.path, rec.Code)
		}
		for name, token := range map[string]string{"buyer": buyer, "seller": seller} {
			rec := doJSON(t, a, r.method, r.path, token, r.body)
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s %s as %s: status = %d, want 403", r.method, r.path, name, rec.Code)
			}
		}
	}
}

// TestAdminCannotSuspendSelf pins the rule that keeps the platform
// recoverable: the one account that can lift a suspension cannot be the one
// suspended.
func TestAdminCannotSuspendSelf(t *testing.T) {
	a := newTestAPI(t)
	admin, adminID := newAdmin(t, a)

	rec := doJSON(t, a, "PATCH", fmt.Sprintf("/admin/users/%d", adminID), admin, map[string]any{"suspended": true})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "cannot suspend yourself") {
		t.Fatalf("body = %s", rec.Body.String())
	}

	// Still usable, which is the point.
	if rec := doJSON(t, a, "GET", "/admin/users", admin, nil); rec.Code != http.StatusOK {
		t.Fatalf("admin unusable after self-suspend attempt: %d", rec.Code)
	}
}

// TestAdminSuspensionTakesEffectAtOnce is the property that distinguishes a
// suspension from a token revocation: the buyer's still-valid token stops
// working on the very next request, and works again without logging in once
// the flag is lifted.
func TestAdminSuspensionTakesEffectAtOnce(t *testing.T) {
	a := newTestAPI(t)
	admin, _ := newAdmin(t, a)
	buyerToken := registerBuyer(t, a, "buyer@example.com", "secret123")
	buyerID := userID(t, a, "buyer@example.com")

	rec := doJSON(t, a, "GET", "/auth/me", buyerToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("buyer before suspension: status = %d", rec.Code)
	}

	rec = doJSON(t, a, "PATCH", fmt.Sprintf("/admin/users/%d", buyerID), admin, map[string]any{"suspended": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("suspend: status = %d body %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, a, "GET", "/auth/me", buyerToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("suspended buyer: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "account suspended") {
		t.Fatalf("body = %s, want account suspended", rec.Body.String())
	}
	// Also refused on the read endpoints that do not need a role.
	if rec := doJSON(t, a, "GET", "/reservations", buyerToken, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("suspended buyer on /reservations: status = %d, want 403", rec.Code)
	}

	rec = doJSON(t, a, "PATCH", fmt.Sprintf("/admin/users/%d", buyerID), admin, map[string]any{"suspended": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("unsuspend: status = %d body %s", rec.Code, rec.Body.String())
	}

	// The same token, no re-login.
	if rec := doJSON(t, a, "GET", "/auth/me", buyerToken, nil); rec.Code != http.StatusOK {
		t.Fatalf("buyer after unsuspend: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestAdminSuspendIsIdempotentAndAuditedOnce covers the reason the handler
// reads the current state first: repeating the call must not pile up audit
// rows for a change that did not happen.
func TestAdminSuspendIsIdempotentAndAuditedOnce(t *testing.T) {
	a := newTestAPI(t)
	admin, adminID := newAdmin(t, a)
	registerBuyer(t, a, "buyer@example.com", "secret123")
	buyerID := userID(t, a, "buyer@example.com")

	path := fmt.Sprintf("/admin/users/%d", buyerID)
	for i := range 2 {
		rec := doJSON(t, a, "PATCH", path, admin, map[string]any{"suspended": true})
		if rec.Code != http.StatusOK {
			t.Fatalf("suspend #%d: status = %d body %s", i+1, rec.Code, rec.Body.String())
		}
	}

	var n int
	if err := a.DB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM admin_audit_log WHERE action = 'user.suspend' AND target_id = ?`, buyerID).Scan(&n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if n != 1 {
		t.Fatalf("audit rows for user.suspend = %d, want 1", n)
	}

	// Unsuspending twice is idempotent too.
	for i := range 2 {
		rec := doJSON(t, a, "PATCH", path, admin, map[string]any{"suspended": false})
		if rec.Code != http.StatusOK {
			t.Fatalf("unsuspend #%d: status = %d body %s", i+1, rec.Code, rec.Body.String())
		}
	}
	if err := a.DB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM admin_audit_log WHERE action = 'user.unsuspend' AND target_id = ?`, buyerID).Scan(&n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if n != 1 {
		t.Fatalf("audit rows for user.unsuspend = %d, want 1", n)
	}

	// The actor is the administrator that made the change, not the target.
	var actor int64
	if err := a.DB.QueryRowContext(context.Background(),
		`SELECT actor_id FROM admin_audit_log WHERE target_id = ?`, buyerID).Scan(&actor); err != nil {
		t.Fatalf("read actor: %v", err)
	}
	if actor != adminID {
		t.Fatalf("actor_id = %d, want %d", actor, adminID)
	}
}

// TestAdminGrantsAndRevokesSellerRole checks the only role the API is allowed
// to move, and that doing it never disturbs the admin membership.
func TestAdminGrantsAndRevokesSellerRole(t *testing.T) {
	a := newTestAPI(t)
	admin, adminID := newAdmin(t, a)
	buyer := registerBuyer(t, a, "buyer@example.com", "secret123")
	buyerID := userID(t, a, "buyer@example.com")
	// Granting seller promises buyers a contact phone, so both targets get
	// one first (phone is not unique, sharing it here is fine).
	for _, tok := range []string{admin, buyer} {
		if rec := doJSON(t, a, "PATCH", "/auth/me", tok,
			map[string]any{"phone": "+56912345678"}); rec.Code != http.StatusOK {
			t.Fatalf("set phone: %d body %s", rec.Code, rec.Body.String())
		}
	}
	// The administrator is also a seller here, to prove the call leaves
	// unrelated memberships alone.
	if rec := doJSON(t, a, "PATCH", fmt.Sprintf("/admin/users/%d/roles", adminID), admin,
		map[string]any{"seller": true}); rec.Code != http.StatusOK {
		t.Fatalf("make admin a seller: %d body %s", rec.Code, rec.Body.String())
	}

	path := fmt.Sprintf("/admin/users/%d/roles", buyerID)
	rec := doJSON(t, a, "PATCH", path, admin, map[string]any{"seller": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("grant seller: status = %d body %s", rec.Code, rec.Body.String())
	}
	if roles := userRoles(t, a, buyerID); len(roles) != 2 || roles[0] != "buyer" || roles[1] != "seller" {
		t.Fatalf("roles after grant = %v, want [buyer seller]", roles)
	}
	// Granting twice writes nothing new.
	if rec := doJSON(t, a, "PATCH", path, admin, map[string]any{"seller": true}); rec.Code != http.StatusOK {
		t.Fatalf("grant seller again: status = %d", rec.Code)
	}
	var n int
	if err := a.DB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM admin_audit_log
		 WHERE action = 'user.role_grant_seller' AND target_id = ?`, buyerID).Scan(&n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if n != 1 {
		t.Fatalf("grant audit rows = %d, want 1", n)
	}

	rec = doJSON(t, a, "PATCH", path, admin, map[string]any{"seller": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke seller: status = %d body %s", rec.Code, rec.Body.String())
	}
	if roles := userRoles(t, a, buyerID); len(roles) != 1 || roles[0] != "buyer" {
		t.Fatalf("roles after revoke = %v, want [buyer]", roles)
	}
	if roles := userRoles(t, a, adminID); len(roles) != 3 {
		t.Fatalf("admin roles changed: %v, want [admin buyer seller]", roles)
	}
}

// TestAdminCannotGrantSellerWithoutPhone pins the seller-phone invariant on
// the admin path too: granting seller to a phoneless account is a 409 and
// changes nothing.
func TestAdminCannotGrantSellerWithoutPhone(t *testing.T) {
	a := newTestAPI(t)
	admin, _ := newAdmin(t, a)
	registerBuyer(t, a, "buyer@example.com", "secret123")
	target := userID(t, a, "buyer@example.com")

	rec := doJSON(t, a, "PATCH", fmt.Sprintf("/admin/users/%d/roles", target), admin,
		map[string]any{"seller": true})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	if roles := userRoles(t, a, target); len(roles) != 1 || roles[0] != "buyer" {
		t.Fatalf("roles changed by rejected call: %v", roles)
	}
}

// TestAdminCannotGrantAdminRole pins the anti-escalation rule at the HTTP
// boundary. The allow-list is the only source of the admin role, so an attempt
// to pass it in the body is rejected by the strict decoder like any other
// unknown field.
func TestAdminCannotGrantAdminRole(t *testing.T) {
	a := newTestAPI(t)
	admin, _ := newAdmin(t, a)
	registerBuyer(t, a, "buyer@example.com", "secret123")
	target := userID(t, a, "buyer@example.com")

	for _, body := range []map[string]any{
		{"admin": true},
		{"seller": true, "admin": true},
		{"roles": []string{"admin"}},
	} {
		rec := doJSON(t, a, "PATCH", fmt.Sprintf("/admin/users/%d/roles", target), admin, body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %v: status = %d, want 400 (body %s)", body, rec.Code, rec.Body.String())
		}
		if roles := userRoles(t, a, target); len(roles) != 1 || roles[0] != "buyer" {
			t.Fatalf("roles changed by rejected call: %v", roles)
		}
	}
}

// TestAdminListsEveryCarIncludingInactive shows the admin view is not the
// public one: /cars hides deactivated cars, /admin/cars does not.
func TestAdminListsEveryCarIncludingInactive(t *testing.T) {
	a := newTestAPI(t)
	admin, _ := newAdmin(t, a)
	seller := newSeller(t, a)
	car := createCar(t, a, seller, map[string]any{"name": "Toyota Yaris", "price_per_day": 45000})

	if rec := doJSON(t, a, "PATCH", fmt.Sprintf("/seller/cars/%d", car.ID), seller,
		map[string]any{"active": false}); rec.Code != http.StatusOK {
		t.Fatalf("seller deactivates: %d body %s", rec.Code, rec.Body.String())
	}

	// Gone from the public catalog...
	rec := doJSON(t, a, "GET", "/cars", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("public list: status = %d", rec.Code)
	}
	var publicList []struct {
		ID int64 `json:"id"`
	}
	decodeJSON(t, rec, &publicList)
	for _, c := range publicList {
		if c.ID == car.ID {
			t.Fatal("deactivated car still listed by the public catalog")
		}
	}

	// ...but present for an administrator, with the owner resolved.
	rec = doJSON(t, a, "GET", "/admin/cars?active=false", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list: status = %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			Active     bool   `json:"active"`
			OwnerEmail string `json:"owner_email"`
		} `json:"items"`
		Total int `json:"total"`
	}
	decodeJSON(t, rec, &out)
	if len(out.Items) != 1 || out.Items[0].ID != car.ID {
		t.Fatalf("items = %+v, want the deactivated car %d", out.Items, car.ID)
	}
	if out.Items[0].Active {
		t.Fatal("car reported as active under active=false filter")
	}
	if out.Items[0].OwnerEmail == "" {
		t.Fatal("owner_email not resolved")
	}
	if out.Total != 1 {
		t.Fatalf("total = %d, want 1", out.Total)
	}
}

// TestAdminDeactivatesCarWithFutureReservations is the moderation override:
// the seller route refuses to deactivate a car that has future bookings
// (409), the admin route does not, because unpublishing a problematic listing
// is exactly the job.
func TestAdminDeactivatesCarWithFutureReservations(t *testing.T) {
	a := newTestAPI(t)
	admin, _ := newAdmin(t, a)
	seller := newSeller(t, a)
	buyer := registerBuyer(t, a, "buyer@example.com", "secret123")
	car := createCar(t, a, seller, map[string]any{"name": "Toyota Yaris", "price_per_day": 45000})

	rec := doJSON(t, a, "POST", "/reservations", buyer, map[string]any{
		"car_id": car.ID, "start_date": futureDate(5), "end_date": futureDate(7),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("reserve: status = %d body %s", rec.Code, rec.Body.String())
	}

	if rec := doJSON(t, a, "PATCH", fmt.Sprintf("/seller/cars/%d", car.ID), seller,
		map[string]any{"active": false}); rec.Code != http.StatusConflict {
		t.Fatalf("seller deactivation: status = %d, want 409", rec.Code)
	}

	if rec := doJSON(t, a, "PATCH", fmt.Sprintf("/admin/cars/%d", car.ID), admin,
		map[string]any{"active": false}); rec.Code != http.StatusOK {
		t.Fatalf("admin deactivation: status = %d body %s", rec.Code, rec.Body.String())
	}

	if err := a.DB.QueryRowContext(context.Background(),
		`SELECT active FROM cars WHERE id = ?`, car.ID).Scan(new(int)); err != nil {
		t.Fatalf("read car: %v", err)
	}
	var n int
	if err := a.DB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM admin_audit_log WHERE action = 'car.update' AND target_id = ?`, car.ID).Scan(&n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if n != 1 {
		t.Fatalf("car.update audit rows = %d, want 1", n)
	}
}

// TestAdminDeleteCarKeepsReservationGuard pins that the delete override is not
// unlimited: history still blocks a hard delete, with the same 409 the seller
// route answers, so the foreign keys keep pointing at real rows.
func TestAdminDeleteCarKeepsReservationGuard(t *testing.T) {
	a := newTestAPI(t)
	admin, adminID := newAdmin(t, a)
	seller := newSeller(t, a)
	buyer := registerBuyer(t, a, "buyer@example.com", "secret123")
	free := createCar(t, a, seller, map[string]any{"name": "Libre", "price_per_day": 1000})
	booked := createCar(t, a, seller, map[string]any{"name": "Reservado", "price_per_day": 1000})

	if rec := doJSON(t, a, "POST", "/reservations", buyer, map[string]any{
		"car_id": booked.ID, "start_date": futureDate(5), "end_date": futureDate(7),
	}); rec.Code != http.StatusCreated {
		t.Fatalf("reserve: status = %d body %s", rec.Code, rec.Body.String())
	}

	if rec := doJSON(t, a, "DELETE", fmt.Sprintf("/admin/cars/%d", booked.ID), admin, nil); rec.Code != http.StatusConflict {
		t.Fatalf("delete with history: status = %d, want 409", rec.Code)
	}

	// A car nobody booked can go, and it is not the seller's to keep anyway.
	if rec := doJSON(t, a, "DELETE", fmt.Sprintf("/admin/cars/%d", free.ID), admin, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete free car: status = %d body %s", rec.Code, rec.Body.String())
	}
	var n int
	if err := a.DB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM cars WHERE id = ?`, free.ID).Scan(&n); err != nil {
		t.Fatalf("count cars: %v", err)
	}
	if n != 0 {
		t.Fatalf("car still present after delete")
	}
	if err := a.DB.QueryRowContext(context.Background(),
		`SELECT actor_id FROM admin_audit_log WHERE action = 'car.delete' AND target_id = ?`, free.ID).Scan(&n); err != nil {
		t.Fatalf("read delete actor: %v", err)
	}
	if int64(n) != adminID {
		t.Fatalf("car.delete actor = %d, want %d", n, adminID)
	}
}

// TestAdminListsReservationsAndPayments checks the read endpoints join the
// context an administrator needs to triage, and that they see rows belonging
// to other people.
func TestAdminListsReservationsAndPayments(t *testing.T) {
	a := newTestAPI(t)
	admin, _ := newAdmin(t, a)
	seller := newSeller(t, a)
	buyer := registerBuyer(t, a, "buyer@example.com", "secret123")
	car := createCar(t, a, seller, map[string]any{"name": "Toyota Yaris", "price_per_day": 45000})

	rec := doJSON(t, a, "POST", "/reservations", buyer, map[string]any{
		"car_id": car.ID, "start_date": futureDate(5), "end_date": futureDate(7),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("reserve: status = %d body %s", rec.Code, rec.Body.String())
	}
	var res struct {
		ID int64 `json:"id"`
	}
	decodeJSON(t, rec, &res)

	rec = doJSON(t, a, "POST", fmt.Sprintf("/reservations/%d/payment", res.ID), buyer, map[string]any{"method": "pos"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("pay: status = %d body %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, a, "GET", "/admin/reservations", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("reservations: status = %d body %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []struct {
			ID         int64  `json:"id"`
			Status     string `json:"status"`
			BuyerEmail string `json:"buyer_email"`
			CarName    string `json:"car_name"`
		} `json:"items"`
		Total int `json:"total"`
	}
	decodeJSON(t, rec, &list)
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("reservations = %+v, want the buyer's single reservation", list)
	}
	if list.Items[0].BuyerEmail != "buyer@example.com" || list.Items[0].CarName != "Toyota Yaris" {
		t.Fatalf("reservation context not resolved: %+v", list.Items[0])
	}

	rec = doJSON(t, a, "GET", "/admin/payments", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("payments: status = %d body %s", rec.Code, rec.Body.String())
	}
	var pays struct {
		Items []struct {
			ID            int64  `json:"id"`
			ReservationID int64  `json:"reservation_id"`
			Status        string `json:"status"`
			BuyerEmail    string `json:"buyer_email"`
		} `json:"items"`
		Total int `json:"total"`
	}
	decodeJSON(t, rec, &pays)
	if pays.Total != 1 || pays.Items[0].ReservationID != res.ID {
		t.Fatalf("payments = %+v, want the single payment of %d", pays, res.ID)
	}
	if pays.Items[0].BuyerEmail != "buyer@example.com" {
		t.Fatalf("payment buyer not resolved: %+v", pays.Items[0])
	}

	// The status filter accepts the real values and rejects anything else.
	if rec := doJSON(t, a, "GET", "/admin/payments?status=pending", admin, nil); rec.Code != http.StatusOK {
		t.Fatalf("filter pending: %d", rec.Code)
	}
	if rec := doJSON(t, a, "GET", "/admin/payments?status=refunded", admin, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bogus status: %d, want 400", rec.Code)
	}
	// 'paid' is a payment status, not a reservation one. Accepting it as a
	// reservation filter would advertise a value the schema cannot store.
	if rec := doJSON(t, a, "GET", "/admin/reservations?status=pending", admin, nil); rec.Code != http.StatusOK {
		t.Fatalf("reservations status filter: %d", rec.Code)
	}
	if rec := doJSON(t, a, "GET", "/admin/reservations?status=paid", admin, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("reservations status=paid: %d, want 400", rec.Code)
	}
}

// TestAdminStatsCountsPlatform feeds the endpoint a known dataset and checks
// every number it reports.
func TestAdminStatsCountsPlatform(t *testing.T) {
	a := newTestAPI(t)
	admin, _ := newAdmin(t, a)
	seller := newSeller(t, a)
	buyer := registerBuyer(t, a, "buyer@example.com", "secret123")
	buyerID := userID(t, a, "buyer@example.com")
	car := createCar(t, a, seller, map[string]any{"name": "Toyota Yaris", "price_per_day": 45000})

	rec := doJSON(t, a, "POST", "/reservations", buyer, map[string]any{
		"car_id": car.ID, "start_date": futureDate(5), "end_date": futureDate(7),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("reserve: %d body %s", rec.Code, rec.Body.String())
	}
	var res struct {
		ID int64 `json:"id"`
	}
	decodeJSON(t, rec, &res)

	// An approved payment, so approved_total has a row to sum. The date
	// arithmetic runs through julianday and comes back as a float, which the
	// handler has to hand over as an integer.
	rec = doJSON(t, a, "POST", fmt.Sprintf("/reservations/%d/payment", res.ID), buyer, map[string]any{"method": "pos"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("pay: %d body %s", rec.Code, rec.Body.String())
	}
	if _, err := a.DB.ExecContext(context.Background(), `UPDATE payments SET status = 'approved'`); err != nil {
		t.Fatalf("approve payment: %v", err)
	}

	if rec := doJSON(t, a, "PATCH", fmt.Sprintf("/admin/users/%d", buyerID), admin,
		map[string]any{"suspended": true}); rec.Code != http.StatusOK {
		t.Fatalf("suspend buyer: %d", rec.Code)
	}

	rec = doJSON(t, a, "GET", "/admin/stats", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("stats: status = %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Users struct {
			Total     int64 `json:"total"`
			Sellers   int64 `json:"sellers"`
			Admins    int64 `json:"admins"`
			Suspended int64 `json:"suspended"`
		} `json:"users"`
		Cars struct {
			Active int64 `json:"active"`
		} `json:"cars"`
		Reservations struct {
			Pending   int64  `json:"pending"`
			Confirmed int64  `json:"confirmed"`
			Cancelled int64  `json:"cancelled"`
			Paid      *int64 `json:"paid"`
		} `json:"reservations"`
		Payments struct {
			Approved      int64 `json:"approved"`
			ApprovedTotal int64 `json:"approved_total"`
		} `json:"payments"`
	}
	decodeJSON(t, rec, &out)

	if out.Users.Total != 3 {
		t.Fatalf("users.total = %d, want 3", out.Users.Total)
	}
	if out.Users.Sellers != 1 {
		t.Fatalf("users.sellers = %d, want 1", out.Users.Sellers)
	}
	if out.Users.Admins != 1 {
		t.Fatalf("users.admins = %d, want 1", out.Users.Admins)
	}
	if out.Users.Suspended != 1 {
		t.Fatalf("users.suspended = %d, want 1", out.Users.Suspended)
	}
	if out.Cars.Active != 1 {
		t.Fatalf("cars.active = %d, want 1", out.Cars.Active)
	}
	if out.Reservations.Pending != 1 {
		t.Fatalf("reservations.pending = %d, want 1", out.Reservations.Pending)
	}
	// The reservations CHECK has no 'paid' status: a reservation is paid when
	// its payment is approved. Reporting a count that can only ever be zero
	// would be a metric that lies, so the key is absent instead.
	if out.Reservations.Paid != nil {
		t.Fatalf("reservations.paid should not be reported, got %d", *out.Reservations.Paid)
	}
	if out.Payments.Approved != 1 {
		t.Fatalf("payments.approved = %d, want 1", out.Payments.Approved)
	}
	// Three inclusive days (futureDate(5)..futureDate(7)) at 45000, summed
	// as an integer even though julianday yields a float.
	if out.Payments.ApprovedTotal != 135000 {
		t.Fatalf("payments.approved_total = %d, want 135000", out.Payments.ApprovedTotal)
	}
}

// TestAdminStatsBillsSameDayAsOneDay pins the inclusive day count: a
// same-day rental is one day of revenue, not zero. end_date - start_date
// alone reports it as nothing.
func TestAdminStatsBillsSameDayAsOneDay(t *testing.T) {
	a := newTestAPI(t)
	admin, _ := newAdmin(t, a)
	seller := newSeller(t, a)
	buyer := registerBuyer(t, a, "sameday@example.com", "secret123")
	car := createCar(t, a, seller, map[string]any{"name": "Kia Picanto", "price_per_day": 50000})

	rec := doJSON(t, a, "POST", "/reservations", buyer, map[string]any{
		"car_id": car.ID, "start_date": futureDate(3), "end_date": futureDate(3),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("reserve: %d body %s", rec.Code, rec.Body.String())
	}
	var res struct {
		ID int64 `json:"id"`
	}
	decodeJSON(t, rec, &res)

	rec = doJSON(t, a, "POST", fmt.Sprintf("/reservations/%d/payment", res.ID), buyer, map[string]any{"method": "pos"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("pay: %d body %s", rec.Code, rec.Body.String())
	}
	if _, err := a.DB.ExecContext(context.Background(), `UPDATE payments SET status = 'approved'`); err != nil {
		t.Fatalf("approve payment: %v", err)
	}

	rec = doJSON(t, a, "GET", "/admin/stats", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("stats: status = %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Payments struct {
			ApprovedTotal int64 `json:"approved_total"`
		} `json:"payments"`
	}
	decodeJSON(t, rec, &out)

	if out.Payments.ApprovedTotal != 50000 {
		t.Fatalf("payments.approved_total = %d, want 50000", out.Payments.ApprovedTotal)
	}
}

// TestAdminAuditListsNewestFirst reads the trail the mutations above wrote and
// checks the ordering, the actor join and the filter.
func TestAdminAuditListsNewestFirst(t *testing.T) {
	a := newTestAPI(t)
	admin, adminID := newAdmin(t, a)
	registerBuyer(t, a, "first@example.com", "secret123")
	registerBuyer(t, a, "second@example.com", "secret123")
	first := userID(t, a, "first@example.com")
	second := userID(t, a, "second@example.com")

	for _, id := range []int64{first, second} {
		if rec := doJSON(t, a, "PATCH", fmt.Sprintf("/admin/users/%d", id), admin,
			map[string]any{"suspended": true}); rec.Code != http.StatusOK {
			t.Fatalf("suspend %d: %d body %s", id, rec.Code, rec.Body.String())
		}
	}

	rec := doJSON(t, a, "GET", "/admin/audit", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit: status = %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []struct {
			ID         int64  `json:"id"`
			ActorID    int64  `json:"actor_id"`
			ActorEmail string `json:"actor_email"`
			Action     string `json:"action"`
			TargetType string `json:"target_type"`
			TargetID   int64  `json:"target_id"`
			CreatedAt  string `json:"created_at"`
		} `json:"items"`
		Total int `json:"total"`
	}
	decodeJSON(t, rec, &out)

	if out.Total != 2 || len(out.Items) != 2 {
		t.Fatalf("audit = %+v, want 2 entries", out)
	}
	// Newest first, so the second suspension leads.
	if out.Items[0].TargetID != second || out.Items[1].TargetID != first {
		t.Fatalf("audit not newest-first: %+v", out.Items)
	}
	for _, item := range out.Items {
		if item.Action != "user.suspend" || item.TargetType != "users" {
			t.Fatalf("unexpected entry: %+v", item)
		}
		if item.ActorID != adminID {
			t.Fatalf("actor_id = %d, want %d", item.ActorID, adminID)
		}
		if item.ActorEmail == "" {
			t.Fatal("actor_email not resolved")
		}
		if item.CreatedAt == "" {
			t.Fatal("created_at missing")
		}
	}

	// Filtered by action and by actor.
	rec = doJSON(t, a, "GET", "/admin/audit?action=user.unsuspend", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("filter: %d body %s", rec.Code, rec.Body.String())
	}
	var filtered struct {
		Total int `json:"total"`
	}
	decodeJSON(t, rec, &filtered)
	if filtered.Total != 0 {
		t.Fatalf("unsuspend entries = %d, want 0", filtered.Total)
	}

	rec = doJSON(t, a, "GET", fmt.Sprintf("/admin/audit?actor_id=%d", adminID), admin, nil)
	decodeJSON(t, rec, &filtered)
	if filtered.Total != 2 {
		t.Fatalf("actor filter total = %d, want 2", filtered.Total)
	}
}

// TestAdminListsUsers pins the search and the paginated envelope shape, which
// is the object form the admin endpoints use (not the bare array of the public
// lists).
func TestAdminListsUsers(t *testing.T) {
	a := newTestAPI(t)
	admin, adminID := newAdmin(t, a)
	for _, email := range []string{"ana@example.com", "beto@example.com", "carla@example.com"} {
		registerBuyer(t, a, email, "secret123")
	}

	rec := doJSON(t, a, "GET", "/admin/users?limit=2", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status = %d body %s", rec.Code, rec.Body.String())
	}
	var page struct {
		Items []struct {
			ID    int64    `json:"id"`
			Email string   `json:"email"`
			Roles []string `json:"roles"`
		} `json:"items"`
		Total  int `json:"total"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}
	decodeJSON(t, rec, &page)
	if page.Limit != 2 || page.Offset != 0 {
		t.Fatalf("limit/offset echoed wrong: %+v", page)
	}
	if page.Total != 4 {
		t.Fatalf("total = %d, want 4", page.Total)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(page.Items))
	}
	if page.Items[0].ID != 1 || page.Items[1].ID != 2 {
		t.Fatalf("page is not ordered by id: %+v", page.Items)
	}

	// Second page continues where the first stopped.
	rec = doJSON(t, a, "GET", "/admin/users?limit=2&offset=2", admin, nil)
	decodeJSON(t, rec, &page)
	if len(page.Items) != 2 || page.Items[0].ID != 3 {
		t.Fatalf("second page = %+v", page.Items)
	}

	// Free-text search hits the email substring.
	rec = doJSON(t, a, "GET", "/admin/users?q=beto", admin, nil)
	decodeJSON(t, rec, &page)
	if page.Total != 1 || page.Items[0].Email != "beto@example.com" {
		t.Fatalf("search by email = %+v", page)
	}

	// ...and the exact id.
	rec = doJSON(t, a, "GET", fmt.Sprintf("/admin/users?q=%d", adminID), admin, nil)
	decodeJSON(t, rec, &page)
	if page.Total != 1 || page.Items[0].ID != adminID {
		t.Fatalf("search by id = %+v", page)
	}

	// Role filter, including the admins themselves.
	rec = doJSON(t, a, "GET", "/admin/users?role=admin", admin, nil)
	decodeJSON(t, rec, &page)
	if page.Total != 1 || page.Items[0].ID != adminID {
		t.Fatalf("role=admin = %+v", page)
	}
	rec = doJSON(t, a, "GET", "/admin/users?role=seller", admin, nil)
	decodeJSON(t, rec, &page)
	if page.Total != 0 {
		t.Fatalf("role=seller total = %d, want 0", page.Total)
	}

	// The detail view and the 404s.
	rec = doJSON(t, a, "GET", fmt.Sprintf("/admin/users/%d", adminID), admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail: status = %d body %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, a, "GET", "/admin/users/9999", admin, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing user: status = %d, want 404", rec.Code)
	}
	if rec := doJSON(t, a, "GET", "/admin/users/abc", admin, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: status = %d, want 400", rec.Code)
	}
}
