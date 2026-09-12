package handlers

import "net/http"

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
		{http.MethodPost, "/auth/register/seller", a.RegisterSeller, limitStrict},
		{http.MethodPost, "/auth/login", a.Login, limitStrict},
		{http.MethodPost, "/auth/refresh", a.Refresh, limitStrict},
		{http.MethodPost, "/auth/logout", a.Auth.RequireAuth(a.Logout), limitStrict},
		{http.MethodGet, "/auth/me", a.Auth.RequireAuth(a.Me), limitStrict},
		{http.MethodPatch, "/auth/me", a.Auth.RequireAuth(a.UpdateMe), limitStrict},
		{http.MethodGet, "/cars", a.ListCars, limitNone},
		{http.MethodGet, "/cars/{id}", a.GetCar, limitNone},
		// Contact exposes the seller's phone, so it gets the strict bucket even
		// though it is a read.
		{http.MethodGet, "/cars/{id}/contact", a.Auth.RequireAuth(a.GetCarContact), limitStrict},
		{http.MethodGet, "/seller/cars", a.Auth.RequireRole("seller", a.ListMyCars), limitNone},
		{http.MethodPost, "/seller/cars", a.Auth.RequireRole("seller", a.CreateCar), limitStandard},
		{http.MethodPatch, "/seller/cars/{id}", a.Auth.RequireRole("seller", a.PatchCar), limitStandard},
		{http.MethodPost, "/reservations", a.Auth.RequireAuth(a.CreateReservation), limitStandard},
		{http.MethodGet, "/reservations", a.Auth.RequireAuth(a.ListReservations), limitNone},
		{http.MethodGet, "/reservations/{id}", a.Auth.RequireAuth(a.GetReservation), limitNone},
		{http.MethodPatch, "/reservations/{id}/cancel", a.Auth.RequireAuth(a.CancelReservation), limitStandard},
		{http.MethodPost, "/reservations/{id}/payment", a.Auth.RequireAuth(a.RecordPayment), limitStandard},
		{http.MethodPost, "/uploads", a.Auth.RequireAuth(a.UploadFile), limitStandard},
		{http.MethodGet, "/uploads/", a.serveUpload, limitNone},
		{http.MethodGet, "/seller/reservations", a.Auth.RequireRole("seller", a.ListSellerReservations), limitNone},
		{http.MethodPatch, "/seller/reservations/{id}/confirm", a.Auth.RequireRole("seller", a.ConfirmReservation), limitStandard},
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
