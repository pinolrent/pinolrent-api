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

func TestAcceptReservation(t *testing.T) {
	a := newTestAPI(t)
	token, seller, v := seedReservation(t, a)
	doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", token, map[string]any{
		"method": "cash", "proof_url": "https://example.com/proof.jpg",
	})

	rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/accept", seller, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var view reservationView
	decodeJSON(t, rec, &view)
	if view.Status != "accepted" || view.Payment == nil || view.Payment.Status != "pending" {
		t.Fatalf("unexpected accepted view: %+v", view)
	}

	// accepting twice is a 409: only pending moves
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/accept", seller, nil); rec.Code != http.StatusConflict {
		t.Fatalf("re-accept: status = %d, want 409", rec.Code)
	}

	// the owner alone does not confirm: the dates stay booked but the
	// reservation is not confirmed yet
	if view.Status == "confirmed" {
		t.Fatalf("accept must not confirm: %+v", view)
	}
}

func TestAcceptReservationRequires(t *testing.T) {
	a := newTestAPI(t)
	token, seller, v := seedReservation(t, a)

	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/accept", token, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("buyer: status = %d, want 403", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/accept", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: status = %d, want 401", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/accept", newSeller(t, a), nil); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign seller: status = %d, want 404", rec.Code)
	}

	// accepting needs no payment: the owner decides on the request, the
	// administrator validates the payment later
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/accept", seller, nil); rec.Code != http.StatusOK {
		t.Fatalf("accept without payment: status = %d, want 200", rec.Code)
	}

	if rec := doJSON(t, a, "PATCH", "/seller/reservations/99999/accept", seller, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown: status = %d, want 404", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/garbage/accept", seller, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: status = %d, want 400", rec.Code)
	}
}

func TestRejectReservation(t *testing.T) {
	a := newTestAPI(t)
	token, seller, v := seedReservation(t, a)
	doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", token, map[string]any{
		"method": "cash", "proof_url": "https://example.com/proof.jpg",
	})

	rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/reject", seller, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var view reservationView
	decodeJSON(t, rec, &view)
	if view.Status != "rejected" || view.Payment == nil || view.Payment.Status != "rejected" {
		t.Fatalf("unexpected rejected view: %+v", view)
	}

	// the dates are free again: the car shows up for the same range
	rec = doJSON(t, a, "GET", "/cars?start_date="+futureDate(11)+"&end_date="+futureDate(11), "", nil)
	var cars []models.Car
	decodeJSON(t, rec, &cars)
	if len(cars) != 1 {
		t.Fatalf("car not available after reject: %+v", cars)
	}

	// terminal: rejecting twice is a 409
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/reject", seller, nil); rec.Code != http.StatusConflict {
		t.Fatalf("re-reject: status = %d, want 409", rec.Code)
	}
	// and the buyer cannot pay it again either
	if rec := doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", token, map[string]any{"method": "pos"}); rec.Code != http.StatusConflict {
		t.Fatalf("re-pay: status = %d, want 409", rec.Code)
	}
}

func TestRejectReservationWithoutPayment(t *testing.T) {
	a := newTestAPI(t)
	_, seller, v := seedReservation(t, a)

	// the owner decides on the request itself: no payment needed to reject
	rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/reject", seller, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var view reservationView
	decodeJSON(t, rec, &view)
	if view.Status != "rejected" || view.Payment != nil {
		t.Fatalf("unexpected rejected view: %+v", view)
	}
}

func TestRejectReservationRequires(t *testing.T) {
	a := newTestAPI(t)
	token, seller, v := seedReservation(t, a)

	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/reject", token, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("buyer: status = %d, want 403", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/reject", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: status = %d, want 401", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/reject", newSeller(t, a), nil); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign seller: status = %d, want 404", rec.Code)
	}

	if rec := doJSON(t, a, "PATCH", "/seller/reservations/99999/reject", seller, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown: status = %d, want 404", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/garbage/reject", seller, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: status = %d, want 400", rec.Code)
	}
}

func TestRejectReservationAfterConfirm(t *testing.T) {
	a := newTestAPI(t)
	token, seller, v := seedReservation(t, a)
	admin, _ := newAdmin(t, a)
	doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", token, map[string]any{"method": "cash"})
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/accept", seller, nil); rec.Code != http.StatusOK {
		t.Fatalf("accept: status = %d", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/confirm", admin, nil); rec.Code != http.StatusOK {
		t.Fatalf("confirm: status = %d", rec.Code)
	}

	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/reject", seller, nil); rec.Code != http.StatusConflict {
		t.Fatalf("reject after confirm: status = %d, want 409", rec.Code)
	}
}

// TestSellerDecisionsAreAudited pins the audit side of the owner's decisions:
// accept and reject each write one audit row with the seller as actor.
func TestSellerDecisionsAreAudited(t *testing.T) {
	a := newTestAPI(t)
	_, seller, v := seedReservation(t, a)
	_, seller2, w := seedReservation(t, a)

	sellerID := func(token string) int64 {
		t.Helper()
		rec := doJSON(t, a, "GET", "/auth/me", token, nil)
		var me struct {
			ID int64 `json:"id"`
		}
		decodeJSON(t, rec, &me)
		return me.ID
	}

	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/accept", seller, nil); rec.Code != http.StatusOK {
		t.Fatalf("accept: status = %d", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(w.ID)+"/reject", seller2, nil); rec.Code != http.StatusOK {
		t.Fatalf("reject: status = %d", rec.Code)
	}

	for _, tc := range []struct {
		action string
		target int64
		actor  int64
	}{
		{"reservation.accept", v.ID, sellerID(seller)},
		{"reservation.reject", w.ID, sellerID(seller2)},
	} {
		var actor int64
		var n int
		if err := a.DB.QueryRowContext(context.Background(),
			`SELECT actor_id, COUNT(*) FROM admin_audit_log WHERE action = ? AND target_id = ? GROUP BY actor_id`,
			tc.action, tc.target).Scan(&actor, &n); err != nil {
			t.Fatalf("%s: %v", tc.action, err)
		}
		if n != 1 || actor != tc.actor {
			t.Fatalf("%s: rows = %d actor = %d, want 1 row by %d", tc.action, n, actor, tc.actor)
		}
	}
}
