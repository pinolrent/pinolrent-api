package handlers

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pinolrent/pinolrent-api/internal/models"
)

func TestListAdminReservations(t *testing.T) {
	a := newTestAPI(t)
	admin := newAdmin(t, a)
	seller := newSeller(t, a)
	car := createCar(t, a, seller, map[string]any{"name": "Toyota Yaris", "price_per_day": 45000})

	tokenA := registerBuyer(t, a, "a@example.com", "secret123")
	tokenB := registerBuyer(t, a, "b@example.com", "secret123")
	vA := createReservation(t, a, tokenA, map[string]any{
		"car_id": car.ID, "start_date": futureDate(10), "end_date": futureDate(11),
	})
	vB := createReservation(t, a, tokenB, map[string]any{
		"car_id": car.ID, "start_date": futureDate(20), "end_date": futureDate(21),
	})

	// the global history shows both buyers' reservations, newest first
	rec := doJSON(t, a, "GET", "/admin/reservations", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var views []reservationView
	decodeJSON(t, rec, &views)
	if len(views) != 2 || views[0].ID != vB.ID || views[1].ID != vA.ID {
		t.Fatalf("admin history = %+v, want [%d %d]", views, vB.ID, vA.ID)
	}

	// status filter
	rec = doJSON(t, a, "GET", "/admin/reservations?status=pending", admin, nil)
	decodeJSON(t, rec, &views)
	if len(views) != 2 {
		t.Fatalf("pending filter = %d, want 2", len(views))
	}
	rec = doJSON(t, a, "GET", "/admin/reservations?status=confirmed", admin, nil)
	decodeJSON(t, rec, &views)
	if len(views) != 0 {
		t.Fatalf("confirmed filter = %d, want 0", len(views))
	}
	if rec := doJSON(t, a, "GET", "/admin/reservations?status=bogus", admin, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad status: status = %d, want 400", rec.Code)
	}

	// pagination
	rec = doJSON(t, a, "GET", "/admin/reservations?limit=1", admin, nil)
	decodeJSON(t, rec, &views)
	if len(views) != 1 || views[0].ID != vB.ID {
		t.Fatalf("limit=1 = %+v", views)
	}

	// only admins can read the global history
	if rec := doJSON(t, a, "GET", "/admin/reservations", tokenA, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("buyer: status = %d, want 403", rec.Code)
	}
	if rec := doJSON(t, a, "GET", "/admin/reservations", seller, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("seller: status = %d, want 403", rec.Code)
	}
	if rec := doJSON(t, a, "GET", "/admin/reservations", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: status = %d, want 401", rec.Code)
	}
}

func TestApproveReservation(t *testing.T) {
	a := newTestAPI(t)
	admin := newAdmin(t, a)
	buyer, seller, v := seedReservation(t, a)
	doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", buyer, map[string]any{"method": "cash"})

	// before the seller accepts there is nothing to approve
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/approve", admin, nil); rec.Code != http.StatusConflict {
		t.Fatalf("approve pending: status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}

	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/confirm", seller, nil); rec.Code != http.StatusOK {
		t.Fatalf("seller accept: status = %d body %s", rec.Code, rec.Body.String())
	}

	rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/approve", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: status = %d body %s", rec.Code, rec.Body.String())
	}
	var view reservationView
	decodeJSON(t, rec, &view)
	if view.Status != "confirmed" || view.Payment == nil || view.Payment.Status != "approved" {
		t.Fatalf("unexpected approved view: %+v", view)
	}

	// terminal: a second approval is a 409
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/approve", admin, nil); rec.Code != http.StatusConflict {
		t.Fatalf("re-approve: status = %d, want 409", rec.Code)
	}

	// the confirmed reservation still blocks the dates
	rec = doJSON(t, a, "GET", "/cars?start_date="+futureDate(11)+"&end_date="+futureDate(11), "", nil)
	var cars []models.Car
	decodeJSON(t, rec, &cars)
	if len(cars) != 0 {
		t.Fatalf("confirmed car still available: %+v", cars)
	}

	// only the admin role can approve
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/approve", buyer, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("buyer: status = %d, want 403", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/approve", seller, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("seller: status = %d, want 403", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/approve", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: status = %d, want 401", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/99999/approve", admin, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown: status = %d, want 404", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/garbage/approve", admin, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: status = %d, want 400", rec.Code)
	}
}

func TestAdminRejectReservation(t *testing.T) {
	a := newTestAPI(t)
	admin := newAdmin(t, a)
	buyer, seller, v := seedReservation(t, a)
	doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", buyer, map[string]any{"method": "cash"})
	doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/confirm", seller, nil)

	rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/reject", admin, map[string]any{
		"admin_note": "comprobante ilegible",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("reject: status = %d body %s", rec.Code, rec.Body.String())
	}
	var view reservationView
	decodeJSON(t, rec, &view)
	if view.Status != "pending" || view.AdminNote != "comprobante ilegible" {
		t.Fatalf("unexpected rejected view: %+v", view)
	}
	if view.Payment == nil || view.Payment.Status != "pending" {
		t.Fatalf("payment must stay pending: %+v", view.Payment)
	}

	// the seller corrects and accepts again: back to the queue, note cleared
	rec = doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/confirm", seller, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("re-accept: status = %d body %s", rec.Code, rec.Body.String())
	}
	var reAccepted reservationView
	decodeJSON(t, rec, &reAccepted)
	if reAccepted.Status != "awaiting_admin" || reAccepted.AdminNote != "" {
		t.Fatalf("unexpected re-accepted view: %+v", reAccepted)
	}

	// and the admin approves it this time
	rec = doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/approve", admin, nil)
	var approved reservationView
	decodeJSON(t, rec, &approved)
	if approved.Status != "confirmed" || approved.Payment == nil || approved.Payment.Status != "approved" {
		t.Fatalf("unexpected approved view: %+v", approved)
	}

	// rejecting outside the queue is a 409
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/reject", admin, map[string]any{"admin_note": "x"}); rec.Code != http.StatusConflict {
		t.Fatalf("reject confirmed: status = %d, want 409", rec.Code)
	}
}

func TestAdminRejectReservationValidates(t *testing.T) {
	a := newTestAPI(t)
	admin := newAdmin(t, a)
	buyer, seller, v := seedReservation(t, a)
	doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", buyer, map[string]any{"method": "cash"})
	doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/confirm", seller, nil)
	path := "/admin/reservations/" + itoa(v.ID) + "/reject"

	if rec := doJSON(t, a, "PATCH", path, buyer, map[string]any{"admin_note": "x"}); rec.Code != http.StatusForbidden {
		t.Fatalf("buyer: status = %d, want 403", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", path, "", map[string]any{"admin_note": "x"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: status = %d, want 401", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", path, admin, map[string]any{"reason": "x"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: status = %d, want 400", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", path, admin, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing body: status = %d, want 400", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", path, admin, map[string]any{"admin_note": strings.Repeat("a", maxAdminNoteLen+1)}); rec.Code != http.StatusBadRequest {
		t.Fatalf("long note: status = %d, want 400", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/garbage/reject", admin, map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: status = %d, want 400", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/99999/reject", admin, map[string]any{}); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown: status = %d, want 404", rec.Code)
	}

	// an empty note is allowed; the seller can still reject to cancel entirely
	rec := doJSON(t, a, "PATCH", path, admin, map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("empty note: status = %d body %s", rec.Code, rec.Body.String())
	}
	var view reservationView
	decodeJSON(t, rec, &view)
	if view.Status != "pending" || view.AdminNote != "" {
		t.Fatalf("unexpected view: %+v", view)
	}
	rec = doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/reject", seller, nil)
	decodeJSON(t, rec, &view)
	if view.Status != "cancelled" || view.Payment == nil || view.Payment.Status != "rejected" {
		t.Fatalf("seller escape hatch: %+v", view)
	}
}
