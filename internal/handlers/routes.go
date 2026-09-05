package handlers

import "net/http"

// Routes returns the HTTP mux with all endpoints registered.
func Routes(a *API) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.Health)
	mux.HandleFunc("POST /auth/register", a.Register)
	mux.HandleFunc("POST /auth/register/seller", a.RegisterSeller)
	mux.HandleFunc("POST /auth/login", a.Login)
	mux.HandleFunc("POST /auth/refresh", a.Refresh)
	mux.Handle("POST /auth/logout", a.Auth.RequireAuth(a.Logout))
	mux.Handle("GET /auth/me", a.Auth.RequireAuth(a.Me))
	mux.HandleFunc("GET /cars", a.ListCars)
	mux.HandleFunc("GET /cars/{id}", a.GetCar)
	mux.Handle("GET /seller/cars", a.Auth.RequireRole("seller", a.ListMyCars))
	mux.Handle("POST /seller/cars", a.Auth.RequireRole("seller", a.CreateCar))
	mux.Handle("PATCH /seller/cars/{id}", a.Auth.RequireRole("seller", a.PatchCar))
	mux.Handle("POST /reservations", a.Auth.RequireAuth(a.CreateReservation))
	mux.Handle("GET /reservations", a.Auth.RequireAuth(a.ListReservations))
	mux.Handle("GET /reservations/{id}", a.Auth.RequireAuth(a.GetReservation))
	mux.Handle("PATCH /reservations/{id}/cancel", a.Auth.RequireAuth(a.CancelReservation))
	mux.Handle("POST /reservations/{id}/payment", a.Auth.RequireAuth(a.RecordPayment))
	mux.Handle("GET /seller/reservations", a.Auth.RequireRole("seller", a.ListSellerReservations))
	mux.Handle("PATCH /seller/reservations/{id}/confirm", a.Auth.RequireRole("seller", a.ConfirmReservation))
	return mux
}
