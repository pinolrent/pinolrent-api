package handlers

import (
	"context"
	"net/http"
	"testing"

	"github.com/pinolrent/pinolrent-api/internal/models"
)

func seedCar(t *testing.T, a *API) models.Car {
	t.Helper()
	return createCar(t, a, newSeller(t, a), map[string]any{
		"name": "Toyota Yaris", "price_per_day": 45000,
	})
}

func createReservation(t *testing.T, a *API, token string, body map[string]any) reservationView {
	t.Helper()
	rec := doJSON(t, a, "POST", "/reservations", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create reservation: status %d body %s", rec.Code, rec.Body.String())
	}
	var v reservationView
	decodeJSON(t, rec, &v)
	return v
}

func TestCreateReservation(t *testing.T) {
	a := newTestAPI(t)
	car := seedCar(t, a)
	token := registerBuyer(t, a, "user@example.com", "secret123")

	v := createReservation(t, a, token, map[string]any{
		"car_id": car.ID, "start_date": "2026-09-10", "end_date": "2026-09-12",
	})
	if v.Status != "pending" || v.CarID != car.ID || v.Car == nil || v.Car.Name != "Toyota Yaris" {
		t.Fatalf("unexpected reservation: %+v", v)
	}
}

func TestCreateReservationValidates(t *testing.T) {
	a := newTestAPI(t)
	car := seedCar(t, a)
	token := registerBuyer(t, a, "user@example.com", "secret123")

	if rec := doJSON(t, a, "POST", "/reservations", "", map[string]any{
		"car_id": car.ID, "start_date": "2026-09-10", "end_date": "2026-09-12",
	}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: status = %d, want 401", rec.Code)
	}

	cases := []struct {
		name string
		body map[string]any
	}{
		{"bad start", map[string]any{"car_id": car.ID, "start_date": "garbage", "end_date": "2026-09-12"}},
		{"reversed", map[string]any{"car_id": car.ID, "start_date": "2026-09-12", "end_date": "2026-09-10"}},
		{"missing car", map[string]any{"start_date": "2026-09-10", "end_date": "2026-09-12"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, a, "POST", "/reservations", token, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}

	if rec := doJSON(t, a, "POST", "/reservations", token, map[string]any{
		"car_id": 99999, "start_date": "2026-09-10", "end_date": "2026-09-12",
	}); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown car: status = %d, want 404", rec.Code)
	}

	inactiveOwner := newSeller(t, a)
	inactive := createCar(t, a, inactiveOwner, map[string]any{"name": "Off", "price_per_day": 100})
	doJSON(t, a, "PATCH", "/seller/cars/"+itoa(inactive.ID), inactiveOwner, map[string]any{"active": false})
	if rec := doJSON(t, a, "POST", "/reservations", token, map[string]any{
		"car_id": inactive.ID, "start_date": "2026-09-10", "end_date": "2026-09-12",
	}); rec.Code != http.StatusConflict {
		t.Fatalf("inactive car: status = %d, want 409", rec.Code)
	}
}

func TestCreateReservationOverlap(t *testing.T) {
	a := newTestAPI(t)
	car := seedCar(t, a)
	token := registerBuyer(t, a, "user@example.com", "secret123")

	createReservation(t, a, token, map[string]any{
		"car_id": car.ID, "start_date": "2026-09-10", "end_date": "2026-09-12",
	})

	cases := []struct {
		name      string
		startDate string
		endDate   string
	}{
		{"inside", "2026-09-11", "2026-09-11"},
		{"straddle start", "2026-09-08", "2026-09-11"},
		{"straddle end", "2026-09-12", "2026-09-15"},
		{"envelope", "2026-09-01", "2026-09-30"},
		{"equal", "2026-09-10", "2026-09-12"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, a, "POST", "/reservations", token, map[string]any{
				"car_id": car.ID, "start_date": tc.startDate, "end_date": tc.endDate,
			})
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}

	rec := doJSON(t, a, "POST", "/reservations", token, map[string]any{
		"car_id": car.ID, "start_date": "2026-09-13", "end_date": "2026-09-15",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("adjacent after: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestCreateReservationAllowsCancelled(t *testing.T) {
	a := newTestAPI(t)
	car := seedCar(t, a)
	token := registerBuyer(t, a, "user@example.com", "secret123")

	if _, err := a.DB.ExecContext(context.Background(),
		`INSERT INTO reservations (user_id, car_id, start_date, end_date, status) VALUES (1, ?, '2026-09-10', '2026-09-12', 'cancelled')`, car.ID,
	); err != nil {
		t.Fatalf("seed cancelled: %v", err)
	}

	rec := doJSON(t, a, "POST", "/reservations", token, map[string]any{
		"car_id": car.ID, "start_date": "2026-09-10", "end_date": "2026-09-12",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestListReservationsOwn(t *testing.T) {
	a := newTestAPI(t)
	car := seedCar(t, a)

	tokenA := registerBuyer(t, a, "a@example.com", "secret123")
	tokenB := registerBuyer(t, a, "b@example.com", "secret123")

	createReservation(t, a, tokenA, map[string]any{
		"car_id": car.ID, "start_date": "2026-09-10", "end_date": "2026-09-11",
	})
	createReservation(t, a, tokenB, map[string]any{
		"car_id": car.ID, "start_date": "2026-09-20", "end_date": "2026-09-21",
	})

	rec := doJSON(t, a, "GET", "/reservations", tokenA, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var views []reservationView
	decodeJSON(t, rec, &views)
	if len(views) != 1 {
		t.Fatalf("expected 1 reservation for user A, got %+v", views)
	}
	if rec := doJSON(t, a, "GET", "/reservations", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: status = %d, want 401", rec.Code)
	}
}

func TestGetReservationAccess(t *testing.T) {
	a := newTestAPI(t)
	seller := newSeller(t, a)
	car := createCar(t, a, seller, map[string]any{"name": "Toyota Yaris", "price_per_day": 45000})

	tokenA := registerBuyer(t, a, "a@example.com", "secret123")
	tokenB := registerBuyer(t, a, "b@example.com", "secret123")
	v := createReservation(t, a, tokenA, map[string]any{
		"car_id": car.ID, "start_date": "2026-09-10", "end_date": "2026-09-11",
	})

	// owner can read
	rec := doJSON(t, a, "GET", "/reservations/"+itoa(v.ID), tokenA, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner: status = %d body %s", rec.Code, rec.Body.String())
	}

	// other buyer cannot (404, no leak)
	if rec := doJSON(t, a, "GET", "/reservations/"+itoa(v.ID), tokenB, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("other buyer: status = %d, want 404", rec.Code)
	}

	// the seller that owns the car can read the reservation
	if rec := doJSON(t, a, "GET", "/reservations/"+itoa(v.ID), seller, nil); rec.Code != http.StatusOK {
		t.Fatalf("car owner: status = %d body %s", rec.Code, rec.Body.String())
	}

	// a seller that does not own the car cannot (404, no leak)
	if rec := doJSON(t, a, "GET", "/reservations/"+itoa(v.ID), newSeller(t, a), nil); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign seller: status = %d, want 404", rec.Code)
	}

	if rec := doJSON(t, a, "GET", "/reservations/garbage", tokenA, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: status = %d, want 400", rec.Code)
	}
	if rec := doJSON(t, a, "GET", "/reservations/99999", tokenA, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown: status = %d, want 404", rec.Code)
	}
}

func TestListSellerReservations(t *testing.T) {
	a := newTestAPI(t)
	seller := newSeller(t, a)
	car := createCar(t, a, seller, map[string]any{"name": "Toyota Yaris", "price_per_day": 45000})

	tokenA := registerBuyer(t, a, "a@example.com", "secret123")
	tokenB := registerBuyer(t, a, "b@example.com", "secret123")
	createReservation(t, a, tokenA, map[string]any{
		"car_id": car.ID, "start_date": "2026-09-10", "end_date": "2026-09-11",
	})
	createReservation(t, a, tokenB, map[string]any{
		"car_id": car.ID, "start_date": "2026-09-20", "end_date": "2026-09-21",
	})

	rec := doJSON(t, a, "GET", "/seller/reservations", seller, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var views []reservationView
	decodeJSON(t, rec, &views)
	if len(views) != 2 {
		t.Fatalf("seller reservations = %d, want 2 (%+v)", len(views), views)
	}

	if rec := doJSON(t, a, "GET", "/seller/reservations", newSeller(t, a), nil); rec.Code != http.StatusOK {
		t.Fatalf("seller without cars: status = %d body %s", rec.Code, rec.Body.String())
	}
}
