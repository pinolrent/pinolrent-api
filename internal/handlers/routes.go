package handlers

import "net/http"

// limiterKind selects which rate limiter protects a route.
type limiterKind uint8

const (
	limitNone limiterKind = iota
	limitAuth
	limitWrite
)

// authNamespace is the prefix the auth limiter protects as a whole namespace,
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
		{http.MethodPost, "/auth/register", a.Register, limitAuth},
		{http.MethodPost, "/auth/register/seller", a.RegisterSeller, limitAuth},
		{http.MethodPost, "/auth/login", a.Login, limitAuth},
		{http.MethodPost, "/auth/refresh", a.Refresh, limitAuth},
		{http.MethodPost, "/auth/logout", a.Auth.RequireAuth(a.Logout), limitAuth},
		{http.MethodGet, "/auth/me", a.Auth.RequireAuth(a.Me), limitAuth},
		{http.MethodGet, "/cars", a.ListCars, limitNone},
		{http.MethodGet, "/cars/{id}", a.GetCar, limitNone},
		{http.MethodGet, "/seller/cars", a.Auth.RequireRole("seller", a.ListMyCars), limitNone},
		{http.MethodPost, "/seller/cars", a.Auth.RequireRole("seller", a.CreateCar), limitWrite},
		{http.MethodPatch, "/seller/cars/{id}", a.Auth.RequireRole("seller", a.PatchCar), limitNone},
		{http.MethodPost, "/reservations", a.Auth.RequireAuth(a.CreateReservation), limitWrite},
		{http.MethodGet, "/reservations", a.Auth.RequireAuth(a.ListReservations), limitNone},
		{http.MethodGet, "/reservations/{id}", a.Auth.RequireAuth(a.GetReservation), limitNone},
		{http.MethodPatch, "/reservations/{id}/cancel", a.Auth.RequireAuth(a.CancelReservation), limitNone},
		{http.MethodPost, "/reservations/{id}/payment", a.Auth.RequireAuth(a.RecordPayment), limitWrite},
		{http.MethodPost, "/uploads", a.Auth.RequireAuth(a.UploadFile), limitWrite},
		{http.MethodGet, "/uploads/", a.serveUpload, limitNone},
		{http.MethodGet, "/seller/reservations", a.Auth.RequireRole("seller", a.ListSellerReservations), limitNone},
		{http.MethodPatch, "/seller/reservations/{id}/confirm", a.Auth.RequireRole("seller", a.ConfirmReservation), limitNone},
	}
}

// Routes returns the HTTP mux with all endpoints registered.
func Routes(a *API) *http.ServeMux {
	mux := http.NewServeMux()
	for _, r := range a.routes() {
		mux.Handle(r.method+" "+r.pattern, r.handler)
	}
	return mux
}

// limiterPatterns returns the prefixes the given limiter must enforce, derived
// from the route table so a new endpoint inherits its limit as soon as it is
// declared. The auth limiter covers its whole namespace instead, so even
// unregistered paths under /auth/ stay metered.
func (a *API) limiterPatterns(kind limiterKind) []string {
	if kind == limitAuth {
		return []string{authNamespace}
	}
	var patterns []string
	for _, r := range a.routes() {
		if r.limit == kind {
			patterns = append(patterns, r.method+" "+r.pattern)
		}
	}
	return patterns
}
