package handlers

import (
	"context"
	"net/http"
	"testing"

	"github.com/pinolrent/pinolrent-api/internal/models"
)

// acceptReservation drives a reservation to the accepted state through the
// seller endpoint, the precondition every admin review test shares.
func acceptReservation(t *testing.T, a *API, buyer, seller string, v reservationView) {
	t.Helper()
	doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", buyer, map[string]any{"method": "cash"})
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/accept", seller, nil); rec.Code != http.StatusOK {
		t.Fatalf("accept: status = %d body %s", rec.Code, rec.Body.String())
	}
}

func TestAdminConfirmReservation(t *testing.T) {
	a := newTestAPI(t)
	buyer, seller, v := seedReservation(t, a)
	admin, adminID := newAdmin(t, a)
	acceptReservation(t, a, buyer, seller, v)

	rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/confirm", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var view reservationView
	decodeJSON(t, rec, &view)
	if view.Status != "confirmed" || view.Payment == nil || view.Payment.Status != "approved" {
		t.Fatalf("unexpected confirmed view: %+v", view)
	}

	// confirming twice is a 409: only accepted moves
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/confirm", admin, nil); rec.Code != http.StatusConflict {
		t.Fatalf("re-confirm: status = %d, want 409", rec.Code)
	}

	// the confirmation leaves an audit record by the acting administrator
	var n int
	if err := a.DB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM admin_audit_log WHERE action = 'reservation.confirm' AND target_id = ? AND actor_id = ?`, v.ID, adminID).Scan(&n); err != nil {
		t.Fatalf("audit: %v", err)
	}
	if n != 1 {
		t.Fatalf("audit rows = %d, want 1", n)
	}

	// a confirmed reservation blocks overlapping bookings via cars endpoint
	rec = doJSON(t, a, "GET", "/cars?start_date="+futureDate(11)+"&end_date="+futureDate(11), "", nil)
	var cars []models.Car
	decodeJSON(t, rec, &cars)
	if len(cars) != 0 {
		t.Fatalf("confirmed car still available: %+v", cars)
	}
}

func TestAdminConfirmReservationRequires(t *testing.T) {
	a := newTestAPI(t)
	buyer, seller, v := seedReservation(t, a)
	admin, _ := newAdmin(t, a)

	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/confirm", buyer, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("buyer: status = %d, want 403", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/confirm", seller, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("seller: status = %d, want 403", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/confirm", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: status = %d, want 401", rec.Code)
	}

	// pending is not accepted: the owner goes first
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/confirm", admin, nil); rec.Code != http.StatusConflict {
		t.Fatalf("pending: status = %d, want 409", rec.Code)
	}

	// accepted without payment: the buyer still has to attach the proof
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/accept", seller, nil); rec.Code != http.StatusOK {
		t.Fatalf("accept: status = %d", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/confirm", admin, nil); rec.Code != http.StatusConflict {
		t.Fatalf("no payment: status = %d, want 409", rec.Code)
	}

	if rec := doJSON(t, a, "PATCH", "/admin/reservations/99999/confirm", admin, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown: status = %d, want 404", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/garbage/confirm", admin, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: status = %d, want 400", rec.Code)
	}
}

func TestAdminRequestCorrection(t *testing.T) {
	a := newTestAPI(t)
	buyer, seller, v := seedReservation(t, a)
	admin, adminID := newAdmin(t, a)
	acceptReservation(t, a, buyer, seller, v)

	rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/request-correction", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var view reservationView
	decodeJSON(t, rec, &view)
	// the payment goes back to the buyer but the reservation stays accepted:
	// the dates keep booked and no new reservation is needed
	if view.Status != "accepted" || view.Payment == nil || view.Payment.Status != "rejected" {
		t.Fatalf("unexpected corrected view: %+v", view)
	}

	var n int
	if err := a.DB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM admin_audit_log WHERE action = 'reservation.request_correction' AND target_id = ? AND actor_id = ?`, v.ID, adminID).Scan(&n); err != nil {
		t.Fatalf("audit: %v", err)
	}
	if n != 1 {
		t.Fatalf("audit rows = %d, want 1", n)
	}
}

func TestAdminRequestCorrectionRequires(t *testing.T) {
	a := newTestAPI(t)
	buyer, seller, v := seedReservation(t, a)
	admin, _ := newAdmin(t, a)

	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/request-correction", buyer, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("buyer: status = %d, want 403", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/request-correction", seller, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("seller: status = %d, want 403", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/request-correction", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: status = %d, want 401", rec.Code)
	}

	// pending is not accepted
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/request-correction", admin, nil); rec.Code != http.StatusConflict {
		t.Fatalf("pending: status = %d, want 409", rec.Code)
	}

	if rec := doJSON(t, a, "PATCH", "/admin/reservations/99999/request-correction", admin, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown: status = %d, want 404", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/garbage/request-correction", admin, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: status = %d, want 400", rec.Code)
	}
}

func TestPaymentCorrectionLoop(t *testing.T) {
	a := newTestAPI(t)
	buyer, seller, v := seedReservation(t, a)
	admin, _ := newAdmin(t, a)
	acceptReservation(t, a, buyer, seller, v)

	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/request-correction", admin, nil); rec.Code != http.StatusOK {
		t.Fatalf("correction: status = %d", rec.Code)
	}

	// the buyer attaches a corrected proof: same payment row, back to pending
	rec := doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", buyer, map[string]any{
		"method": "pos", "proof_url": "https://example.com/proof2.jpg",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("re-pay: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var p models.Payment
	decodeJSON(t, rec, &p)
	if p.Method != "pos" || p.Status != "pending" || p.ProofURL != "https://example.com/proof2.jpg" {
		t.Fatalf("unexpected corrected payment: %+v", p)
	}

	// and the loop closes: the administrator confirms the corrected payment
	conf := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/confirm", admin, nil)
	if conf.Code != http.StatusOK {
		t.Fatalf("confirm: status = %d body %s", conf.Code, conf.Body.String())
	}
	var view reservationView
	decodeJSON(t, conf, &view)
	if view.Status != "confirmed" || view.Payment == nil || view.Payment.Status != "approved" {
		t.Fatalf("unexpected confirmed view: %+v", view)
	}
}
