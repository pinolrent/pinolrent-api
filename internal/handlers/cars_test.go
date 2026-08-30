package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/pinolrent/pinolrent-api/internal/models"
)

func createCar(t *testing.T, a *API, token string, body map[string]any) models.Car {
	t.Helper()
	rec := doJSON(t, a, "POST", "/seller/cars", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create car: status %d body %s", rec.Code, rec.Body.String())
	}
	var car models.Car
	decodeJSON(t, rec, &car)
	return car
}

func TestCreateCar(t *testing.T) {
	a := newTestAPI(t)
	token := newSeller(t, a)

	car := createCar(t, a, token, map[string]any{
		"name": "Toyota Yaris", "photo_url": "https://example.com/yaris.jpg", "price_per_day": 45000,
	})
	if car.Name != "Toyota Yaris" || car.PhotoURL == "" || car.PricePerDay != 45000 || !car.Active {
		t.Fatalf("unexpected car: %+v", car)
	}
	if car.OwnerID == 0 {
		t.Fatalf("car has no owner_id: %+v", car)
	}
}

func TestCreateCarValidates(t *testing.T) {
	a := newTestAPI(t)
	token := newSeller(t, a)

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
			rec := doJSON(t, a, "POST", "/seller/cars", token, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCreateCarRequiresSeller(t *testing.T) {
	a := newTestAPI(t)

	if rec := doJSON(t, a, "POST", "/seller/cars", "", map[string]any{"name": "X", "price_per_day": 1}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401", rec.Code)
	}

	buyerToken := registerBuyer(t, a, "cli@example.com", "secret123")
	if rec := doJSON(t, a, "POST", "/seller/cars", buyerToken, map[string]any{"name": "X", "price_per_day": 1}); rec.Code != http.StatusForbidden {
		t.Fatalf("buyer: status = %d, want 403", rec.Code)
	}
}

func TestGetCar(t *testing.T) {
	a := newTestAPI(t)
	token := newSeller(t, a)
	car := createCar(t, a, token, map[string]any{"name": "Toyota Yaris", "photo_url": "https://example.com/yaris.jpg", "price_per_day": 45000})

	rec := doJSON(t, a, "GET", "/cars/"+itoa(car.ID), "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var out models.Car
	decodeJSON(t, rec, &out)
	if out.ID != car.ID || out.Name != "Toyota Yaris" || out.PricePerDay != 45000 || !out.Active {
		t.Fatalf("unexpected car: %+v", out)
	}

	if rec := doJSON(t, a, "GET", "/cars/999999", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing: status = %d, want 404", rec.Code)
	}
	if rec := doJSON(t, a, "GET", "/cars/garbage", "", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: status = %d, want 400", rec.Code)
	}

	doJSON(t, a, "PATCH", "/seller/cars/"+itoa(car.ID), token, map[string]any{"active": false})
	if rec := doJSON(t, a, "GET", "/cars/"+itoa(car.ID), "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("inactive: status = %d, want 404", rec.Code)
	}
}

func TestPatchCar(t *testing.T) {
	a := newTestAPI(t)
	token := newSeller(t, a)
	car := createCar(t, a, token, map[string]any{"name": "Toyota Yaris", "price_per_day": 45000})

	rec := doJSON(t, a, "PATCH", "/seller/cars/"+itoa(car.ID), token, map[string]any{"active": false})
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

func TestPatchCarOwnership(t *testing.T) {
	a := newTestAPI(t)
	owner := newSeller(t, a)
	car := createCar(t, a, owner, map[string]any{"name": "Toyota Yaris", "price_per_day": 45000})

	other := newSeller(t, a)
	rec := doJSON(t, a, "PATCH", "/seller/cars/"+itoa(car.ID), other, map[string]any{"active": false})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("other seller: status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, a, "GET", "/seller/cars", other, nil)
	var mine []models.Car
	decodeJSON(t, rec, &mine)
	if len(mine) != 0 {
		t.Fatalf("other seller sees foreign car: %+v", mine)
	}
}

func TestListMyCars(t *testing.T) {
	a := newTestAPI(t)
	seller := newSeller(t, a)
	createCar(t, a, seller, map[string]any{"name": "A", "price_per_day": 100})
	createCar(t, a, seller, map[string]any{"name": "B", "price_per_day": 200})

	rec := doJSON(t, a, "GET", "/seller/cars", seller, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var cars []models.Car
	decodeJSON(t, rec, &cars)
	if len(cars) != 2 {
		t.Fatalf("my cars = %d, want 2 (%+v)", len(cars), cars)
	}
}

func TestPatchCarValidates(t *testing.T) {
	a := newTestAPI(t)
	token := newSeller(t, a)
	car := createCar(t, a, token, map[string]any{"name": "Toyota Yaris", "price_per_day": 45000})

	if rec := doJSON(t, a, "PATCH", "/seller/cars/garbage", token, map[string]any{"active": true}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: status = %d, want 400", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/seller/cars/999999", token, map[string]any{"active": true}); rec.Code != http.StatusNotFound {
		t.Fatalf("missing car: status = %d, want 404", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/seller/cars/"+itoa(car.ID), token, map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing active: status = %d, want 400", rec.Code)
	}
}

func TestListCarsByDates(t *testing.T) {
	a := newTestAPI(t)
	token := newSeller(t, a)

	free := createCar(t, a, token, map[string]any{"name": "Free", "price_per_day": 100})
	booked := createCar(t, a, token, map[string]any{"name": "Booked", "price_per_day": 200})

	buyerToken := registerBuyer(t, a, "cli@example.com", "secret123")
	_ = buyerToken
	var buyerID int64
	if err := a.DB.QueryRowContext(context.Background(),
		`SELECT id FROM users WHERE email = 'cli@example.com'`).Scan(&buyerID); err != nil {
		t.Fatalf("buyer id: %v", err)
	}
	if _, err := a.DB.ExecContext(context.Background(),
		`INSERT INTO reservations (user_id, car_id, start_date, end_date, status) VALUES (?, ?, '2026-09-05', '2026-09-10', 'pending')`,
		buyerID, booked.ID,
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

func TestListCarsPagination(t *testing.T) {
	a := newTestAPI(t)
	seller := newSeller(t, a)
	for i := 0; i < 5; i++ {
		createCar(t, a, seller, map[string]any{
			"name": fmt.Sprintf("C%d", i), "price_per_day": i * 100,
		})
	}

	get := func(q string) []models.Car {
		rec := doJSON(t, a, "GET", "/cars"+q, "", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
		}
		var cars []models.Car
		decodeJSON(t, rec, &cars)
		return cars
	}

	if cars := get("?limit=2"); len(cars) != 2 || cars[0].ID != 1 || cars[1].ID != 2 {
		t.Fatalf("limit=2: got %+v", cars)
	}
	if cars := get("?limit=2&offset=2"); len(cars) != 2 || cars[0].ID != 3 || cars[1].ID != 4 {
		t.Fatalf("offset=2: got %+v", cars)
	}
	if cars := get("?offset=4"); len(cars) != 1 || cars[0].ID != 5 {
		t.Fatalf("offset=4: got %+v", cars)
	}
}

func TestListCarsPaginationValidation(t *testing.T) {
	a := newTestAPI(t)
	cases := []struct{ name, q string }{
		{"bad limit", "/cars?limit=abc"},
		{"zero limit", "/cars?limit=0"},
		{"negative limit", "/cars?limit=-1"},
		{"over max", "/cars?limit=201"},
		{"bad offset", "/cars?offset=-2"},
		{"non-numeric offset", "/cars?offset=x"},
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

func TestListCarsByOwner(t *testing.T) {
	a := newTestAPI(t)
	sellerA := newSeller(t, a)
	sellerB := newSeller(t, a)
	ca := createCar(t, a, sellerA, map[string]any{"name": "A1", "price_per_day": 100})
	createCar(t, a, sellerA, map[string]any{"name": "A2", "price_per_day": 200})
	cb := createCar(t, a, sellerB, map[string]any{"name": "B1", "price_per_day": 300})

	get := func(q string) []models.Car {
		rec := doJSON(t, a, "GET", "/cars"+q, "", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
		}
		var cars []models.Car
		decodeJSON(t, rec, &cars)
		return cars
	}

	if cars := get("?owner_id=" + itoa(ca.OwnerID)); len(cars) != 2 || cars[0].OwnerID != ca.OwnerID || cars[1].OwnerID != ca.OwnerID {
		t.Fatalf("owner filter: got %+v", cars)
	}
	if cars := get("?owner_id=999"); len(cars) != 0 {
		t.Fatalf("unknown owner should be empty, got %+v", cars)
	}

	// combined with the date filter: bookings still exclude the car
	buyerToken := registerBuyer(t, a, "cli@example.com", "secret123")
	createReservation(t, a, buyerToken, map[string]any{
		"car_id": cb.ID, "start_date": "2026-09-05", "end_date": "2026-09-10",
	})
	if cars := get("?owner_id=" + itoa(cb.OwnerID) + "&start_date=2026-09-07&end_date=2026-09-08"); len(cars) != 0 {
		t.Fatalf("owner+dates should exclude booked car, got %+v", cars)
	}

	if rec := doJSON(t, a, "GET", "/cars?owner_id=abc", "", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad owner_id: status = %d, want 400", rec.Code)
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
