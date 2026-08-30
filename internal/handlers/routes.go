package handlers

import "net/http"

func Routes(a *API) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.Health)
	mux.HandleFunc("POST /auth/register", a.Register)
	mux.HandleFunc("POST /auth/login", a.Login)
	mux.HandleFunc("GET /cars", a.ListCars)
	mux.Handle("POST /admin/cars", a.Auth.RequireRole("admin", a.CreateCar))
	mux.Handle("PATCH /admin/cars/{id}", a.Auth.RequireRole("admin", a.PatchCar))
	mux.Handle("POST /reservations", a.Auth.RequireAuth(a.CreateReservation))
	mux.Handle("GET /reservations", a.Auth.RequireAuth(a.ListReservations))
	mux.Handle("GET /reservations/{id}", a.Auth.RequireAuth(a.GetReservation))
	mux.Handle("POST /reservations/{id}/payment", a.Auth.RequireAuth(a.RecordPayment))
	mux.Handle("PATCH /admin/reservations/{id}/confirm", a.Auth.RequireRole("admin", a.ConfirmReservation))
	return mux
}