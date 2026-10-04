package handlers

import (
	"net/http"

	"github.com/pinolrent/pinolrent-api/internal/db"
)

// limiterKind selects which rate limiter protects a route. The kinds name the
// bucket, not the HTTP method: strict and standard are per-IP budgets.
type limiterKind uint8

const (
	limitNone limiterKind = iota
	limitStrict
	limitStandard
)

// authNamespace is the prefix the strict limiter protects as a whole namespace,
// so requests to unregistered paths under /auth/ stay metered too.
const authNamespace = "/auth/"

// route is one entry of the API surface: the mux pattern, the handler with its
// auth middleware already applied, and the rate limiter it belongs to.
type route struct {
	method  string
	pattern string
	handler http.HandlerFunc
	limit   limiterKind
}

// routes returns the API surface. This table is the single source of truth for
// which endpoints exist, who may call them and how they are rate limited;
// Routes, NewRouter, CORS and the tests all derive from it.
func (a *API) routes() []route {
	return []route{
		{http.MethodGet, "/health", a.Health, limitNone},
		{http.MethodPost, "/auth/register", a.Register, limitStrict},
		{http.MethodPost, "/auth/become-seller", a.Auth.RequireAuth(a.BecomeSeller), limitStrict},
		{http.MethodPost, "/auth/login", a.Login, limitStrict},
		{http.MethodPost, "/auth/refresh", a.Refresh, limitStrict},
		{http.MethodPost, "/auth/logout", a.Logout, limitStrict},
		{http.MethodGet, "/auth/me", a.Auth.RequireAuth(a.Me), limitStrict},
		{http.MethodPatch, "/auth/me", a.Auth.RequireAuth(a.UpdateMe), limitStrict},
		{http.MethodPatch, "/auth/password", a.Auth.RequireAuth(a.UpdatePassword), limitStrict},
		{http.MethodGet, "/cars", a.ListCars, limitNone},
		{http.MethodGet, "/cars/{id}", a.GetCar, limitNone},
		// Contact exposes the seller's phone, so it gets the strict bucket even
		// though it is a read.
		{http.MethodGet, "/cars/{id}/contact", a.Auth.RequireAuth(a.GetCarContact), limitStrict},
		{http.MethodGet, "/seller/cars", a.Auth.RequireRole(db.RoleSeller, a.ListMyCars), limitNone},
		{http.MethodPost, "/seller/cars", a.Auth.RequireRole(db.RoleSeller, a.CreateCar), limitStandard},
		{http.MethodPatch, "/seller/cars/{id}", a.Auth.RequireRole(db.RoleSeller, a.PatchCar), limitStandard},
		{http.MethodDelete, "/seller/cars/{id}", a.Auth.RequireRole(db.RoleSeller, a.DeleteCar), limitStandard},
		{http.MethodPost, "/reservations", a.Auth.RequireAuth(a.CreateReservation), limitStandard},
		{http.MethodGet, "/reservations", a.Auth.RequireAuth(a.ListReservations), limitNone},
		{http.MethodGet, "/reservations/{id}", a.Auth.RequireAuth(a.GetReservation), limitNone},
		{http.MethodPatch, "/reservations/{id}/cancel", a.Auth.RequireAuth(a.CancelReservation), limitStandard},
		{http.MethodPost, "/reservations/{id}/payment", a.Auth.RequireAuth(a.RecordPayment), limitStandard},
		{http.MethodGet, "/notifications", a.Auth.RequireAuth(a.ListNotifications), limitNone},
		{http.MethodPatch, "/notifications/{id}/read", a.Auth.RequireAuth(a.MarkNotificationRead), limitStandard},
		{http.MethodPost, "/uploads", a.Auth.RequireAuth(a.UploadFile), limitStandard},
		{http.MethodGet, "/uploads/", a.serveUpload, limitNone},
		{http.MethodGet, "/seller/reservations", a.Auth.RequireRole(db.RoleSeller, a.ListSellerReservations), limitNone},
		{http.MethodPatch, "/seller/reservations/{id}/accept", a.Auth.RequireRole(db.RoleSeller, a.AcceptReservation), limitStandard},
		{http.MethodPatch, "/seller/reservations/{id}/reject", a.Auth.RequireRole(db.RoleSeller, a.RejectReservation), limitStandard},
		{http.MethodGet, "/admin/users", a.Auth.RequireRole(db.RoleAdmin, a.AdminListUsers), limitNone},
		{http.MethodGet, "/admin/users/{id}", a.Auth.RequireRole(db.RoleAdmin, a.AdminGetUser), limitNone},
		{http.MethodPatch, "/admin/users/{id}", a.Auth.RequireRole(db.RoleAdmin, a.AdminPatchUser), limitStandard},
		{http.MethodPatch, "/admin/users/{id}/roles", a.Auth.RequireRole(db.RoleAdmin, a.AdminPatchUserRoles), limitStandard},
		{http.MethodGet, "/admin/cars", a.Auth.RequireRole(db.RoleAdmin, a.AdminListCars), limitNone},
		{http.MethodPatch, "/admin/cars/{id}", a.Auth.RequireRole(db.RoleAdmin, a.AdminPatchCar), limitStandard},
		{http.MethodDelete, "/admin/cars/{id}", a.Auth.RequireRole(db.RoleAdmin, a.AdminDeleteCar), limitStandard},
		{http.MethodGet, "/admin/reservations", a.Auth.RequireRole(db.RoleAdmin, a.AdminListReservations), limitNone},
		{http.MethodPatch, "/admin/reservations/{id}/confirm", a.Auth.RequireRole(db.RoleAdmin, a.AdminConfirmReservation), limitStandard},
		{http.MethodPatch, "/admin/reservations/{id}/request-correction", a.Auth.RequireRole(db.RoleAdmin, a.AdminRequestCorrection), limitStandard},
		{http.MethodGet, "/admin/payments", a.Auth.RequireRole(db.RoleAdmin, a.AdminListPayments), limitNone},
		{http.MethodGet, "/admin/notifications", a.Auth.RequireRole(db.RoleAdmin, a.AdminListNotifications), limitNone},
		{http.MethodGet, "/admin/stats", a.Auth.RequireRole(db.RoleAdmin, a.AdminStats), limitNone},
		{http.MethodGet, "/admin/audit", a.Auth.RequireRole(db.RoleAdmin, a.AdminListAudit), limitNone},
	}
}

// Routes returns the HTTP mux with all endpoints registered and no rate
// limiting. NewRouter builds the production mux, where every route carries the
// limiter its table entry declares.
func Routes(a *API) *http.ServeMux {
	mux := http.NewServeMux()
	for _, r := range a.routes() {
		mux.Handle(r.method+" "+r.pattern, r.handler)
	}
	return mux
}
