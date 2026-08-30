package handlers

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/pinolrent/pinolrent-api/internal/models"
)

func createCar(t *testing.T, a *API, token string, body map[string]any) models.Car {
	t.Helper()
	rec := doJSON(t, a, "POST", "/admin/cars", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create car: status %d body %s", rec.Code, rec.Body.String())
	}
	var car models.Car
	decodeJSON(t, rec, &car)
	return car
}

func TestCreateCar(t *testing.T) {
	a := newTestAPI(t)
	token := loginAdmin(t, a)

	car := createCar(t, a, token, map[string]any{
		"name": "Toyota Yaris", "photo_url": "https://example.com/yaris.jpg", "price_per_day": 45000,
	})
	if car.Name != "Toyota Yaris" || car.PhotoURL == "" || car.PricePerDay != 45000 || !car.Active {
		t.Fatalf("unexpected car: %+v", car)
	}
}

func TestCreateCarValidates(t *testing.T) {
	a := newTestAPI(t)
	token := loginAdmin(t, a)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing name", map[string]any{"price_per_day": 45000}},
		{"empty name", map[string]any{"name": "  ", "price_per_day": 45000}},
		{"negative price", map[string]any{"name": "Toyota", "price_per_day": -1}},
		{"bad photo url", map[string]any{"name": "Toyota", "photo_url": "::not-a-url::", "price_per_day": 1}},
		{"malformed", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, a, "POST", "/admin/cars", token, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCreateCarRequiresAdmin(t *testing.T) {
	a := newTestAPI(t)

	if rec := doJSON(t, a, "POST", "/admin/cars", "", map[string]any{"name": "X", "price_per_day": 1}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401", rec.Code)
	}

	clientToken := registerClient(t, a, "cli@example.com", "secret123")
	if rec := doJSON(t, a, "POST", "/admin/cars", clientToken, map[string]any{"name": "X", "price_per_day": 1}); rec.Code != http.StatusForbidden {
		t.Fatalf("client: status = %d, want 403", rec.Code)
	}
}

func TestPatchCar(t *testing.T) {
	a := newTestAPI(t)
	token := loginAdmin(t, a)
	car := createCar(t, a, token, map[string]any{"name": "Toyota Yaris", "price_per_day": 45000})

	rec := doJSON(t, a, "PATCH", "/admin/cars/"+itoa(car.ID), token, map[string]any{"active": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var out models.Car
	decodeJSON(t, rec, &out)
	if out.Active {
		t.Fatalf("car still active: %+v", out)
	}

	rec = doJSON(t, a, "GET", "/cars", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var cars []models.Car
	decodeJSON(t, rec, &cars)
	if len(cars) != 0 {
		t.Fatalf("inactive car shown in list: %+v", cars)
	}
}

func TestPatchCarValidates(t *testing.T) {
	a := newTestAPI(t)
	token := loginAdmin(t, a)
	car := createCar(t, a, token, map[string]any{"name": "Toyota Yaris", "price_per_day": 45000})

	if rec := doJSON(t, a, "PATCH", "/admin/cars/garbage", token, map[string]any{"active": true}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: status = %d, want 400", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/admin/cars/999999", token, map[string]any{"active": true}); rec.Code != http.StatusNotFound {
		t.Fatalf("missing car: status = %d, want 404", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/admin/cars/"+itoa(car.ID), token, map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing active: status = %d, want 400", rec.Code)
	}
}

func TestListCarsByDates(t *testing.T) {
	a := newTestAPI(t)
	token := loginAdmin(t, a)

	free := createCar(t, a, token, map[string]any{"name": "Free", "price_per_day": 100})
	booked := createCar(t, a, token, map[string]any{"name": "Booked", "price_per_day": 200})

	clientToken := registerClient(t, a, "cli@example.com", "secret123")
	_ = clientToken
	if _, err := a.DB.Exec(
		`INSERT INTO reservations (user_id, car_id, start_date, end_date, status) VALUES (?, ?, '2026-09-05', '2026-09-10', 'pending')`,
		1, booked.ID,
	); err != nil {
		t.Fatalf("seed reservation: %v", err)
	}

	rec := doJSON(t, a, "GET", "/cars?start_date=2026-09-07&end_date=2026-09-08", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var cars []models.Car
	decodeJSON(t, rec, &cars)
	if len(cars) != 1 || cars[0].ID != free.ID {
		t.Fatalf("expected only free car, got %+v", cars)
	}

	rec = doJSON(t, a, "GET", "/cars?start_date=2026-09-01&end_date=2026-09-02", "", nil)
	decodeJSON(t, rec, &cars)
	if len(cars) != 2 {
		t.Fatalf("expected both cars outside range, got %+v", cars)
	}

	rec = doJSON(t, a, "GET", "/cars?start_date=2026-09-10&end_date=2026-09-12", "", nil)
	decodeJSON(t, rec, &cars)
	if len(cars) != 1 || cars[0].ID != free.ID {
		t.Fatalf("boundary end overlap: expected only free car, got %+v", cars)
	}
}

func TestListCarsDateValidation(t *testing.T) {
	a := newTestAPI(t)

	cases := []struct{ name, q string }{
		{"only start", "/cars?start_date=2026-09-01"},
		{"bad start", "/cars?start_date=2026-13-99&end_date=2026-09-10"},
		{"bad end", "/cars?start_date=2026-09-01&end_date=nope"},
		{"reversed", "/cars?start_date=2026-09-10&end_date=2026-09-01"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, a, "GET", tc.q, "", nil)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestListCarsEmpty(t *testing.T) {
	a := newTestAPI(t)
	rec := doJSON(t, a, "GET", "/cars", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var cars []models.Car
	decodeJSON(t, rec, &cars)
	if cars == nil || len(cars) != 0 {
		t.Fatalf("expected empty non-nil list, got %#v", cars)
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}