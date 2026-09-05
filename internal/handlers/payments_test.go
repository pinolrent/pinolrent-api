package handlers

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/pinolrent/pinolrent-api/internal/models"
)

func seedReservation(t *testing.T, a *API) (buyerToken, sellerToken string, v reservationView) {
	t.Helper()
	sellerToken = newSeller(t, a)
	car := createCar(t, a, sellerToken, map[string]any{"name": "Toyota Yaris", "price_per_day": 45000})
	buyerToken = registerBuyer(t, a, "user@example.com", "secret123")
	v = createReservation(t, a, buyerToken, map[string]any{
		"car_id": car.ID, "start_date": futureDate(10), "end_date": futureDate(12),
	})
	return buyerToken, sellerToken, v
}

func TestRecordPayment(t *testing.T) {
	a := newTestAPI(t)
	token, _, v := seedReservation(t, a)

	rec := doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", token, map[string]any{
		"method": "cash", "proof_url": "https://example.com/proof.jpg",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var p models.Payment
	decodeJSON(t, rec, &p)
	if p.Method != "cash" || p.Status != "pending" || p.ReservationID != v.ID {
		t.Fatalf("unexpected payment: %+v", p)
	}

	rec = doJSON(t, a, "GET", "/reservations/"+itoa(v.ID), token, nil)
	var view reservationView
	decodeJSON(t, rec, &view)
	if view.Payment == nil || view.Payment.Status != "pending" {
		t.Fatalf("payment not attached to reservation: %+v", view)
	}
}

func TestRecordPaymentValidates(t *testing.T) {
	a := newTestAPI(t)
	token, _, v := seedReservation(t, a)

	if rec := doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", "", map[string]any{
		"method": "cash",
	}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: status = %d, want 401", rec.Code)
	}

	cases := []struct {
		name string
		body map[string]any
	}{
		{"bad method", map[string]any{"method": "card"}},
		{"missing method", map[string]any{}},
		{"bad url", map[string]any{"method": "cash", "proof_url": "::not-url::"}},
		{"url too long", map[string]any{"method": "cash", "proof_url": "https://x.com/" + strings.Repeat("a", 3000)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", token, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}

	if rec := doJSON(t, a, "POST", "/reservations/garbage/payment", token, map[string]any{
		"method": "cash",
	}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: status = %d, want 400", rec.Code)
	}
	if rec := doJSON(t, a, "POST", "/reservations/99999/payment", token, map[string]any{
		"method": "cash",
	}); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown: status = %d, want 404", rec.Code)
	}
}

func TestRecordPaymentOwnership(t *testing.T) {
	a := newTestAPI(t)
	_, _, v := seedReservation(t, a)
	other := registerBuyer(t, a, "other@example.com", "secret123")

	rec := doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", other, map[string]any{
		"method": "pos",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("other client: status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestRecordPaymentDuplicate(t *testing.T) {
	a := newTestAPI(t)
	token, _, v := seedReservation(t, a)

	doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", token, map[string]any{"method": "cash"})
	rec := doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", token, map[string]any{"method": "pos"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate: status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestRecordPaymentCancelled(t *testing.T) {
	a := newTestAPI(t)
	car := seedCar(t, a)
	token := registerBuyer(t, a, "user@example.com", "secret123")
	v := createReservation(t, a, token, map[string]any{
		"car_id": car.ID, "start_date": futureDate(10), "end_date": futureDate(12),
	})

	if _, err := a.DB.ExecContext(context.Background(), `UPDATE reservations SET status = 'cancelled' WHERE id = ?`, v.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	rec := doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", token, map[string]any{"method": "cash"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("cancelled: status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestConfirmReservation(t *testing.T) {
	a := newTestAPI(t)
	token, seller, v := seedReservation(t, a)
	doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", token, map[string]any{
		"method": "cash", "proof_url": "https://example.com/proof.jpg",
	})

	rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/confirm", seller, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var view reservationView
	decodeJSON(t, rec, &view)
	if view.Status != "confirmed" || view.Payment == nil || view.Payment.Status != "approved" {
		t.Fatalf("unexpected confirmed view: %+v", view)
	}

	// now confirmed -> 409
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/confirm", seller, nil); rec.Code != http.StatusConflict {
		t.Fatalf("re-confirm: status = %d, want 409", rec.Code)
	}

	// confirmed reservation blocks overlapping booking via cars endpoint
	rec = doJSON(t, a, "GET", "/cars?start_date="+futureDate(11)+"&end_date="+futureDate(11), "", nil)
	var cars []models.Car
	decodeJSON(t, rec, &cars)
	if len(cars) != 0 {
		t.Fatalf("confirmed car still available: %+v", cars)
	}
}

func TestConfirmReservationRequires(t *testing.T) {
	a := newTestAPI(t)
	token, seller, v := seedReservation(t, a)

	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/confirm", token, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("buyer: status = %d, want 403", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/confirm", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: status = %d, want 401", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/confirm", newSeller(t, a), nil); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign seller: status = %d, want 404", rec.Code)
	}

	// no payment yet
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/confirm", seller, nil); rec.Code != http.StatusConflict {
		t.Fatalf("no payment: status = %d, want 409", rec.Code)
	}

	if rec := doJSON(t, a, "PATCH", "/seller/reservations/99999/confirm", seller, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown: status = %d, want 404", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/garbage/confirm", seller, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: status = %d, want 400", rec.Code)
	}
}
