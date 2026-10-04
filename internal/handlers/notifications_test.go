package handlers

import (
	"net/http"
	"testing"

	"github.com/pinolrent/pinolrent-api/internal/models"
)

func notificationsOf(t *testing.T, a *API, token string) []models.Notification {
	t.Helper()
	rec := doJSON(t, a, "GET", "/notifications", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status = %d body %s", rec.Code, rec.Body.String())
	}
	var out []models.Notification
	decodeJSON(t, rec, &out)
	return out
}

func kinds(ns []models.Notification) map[string]bool {
	m := map[string]bool{}
	for _, n := range ns {
		m[n.Kind] = true
	}
	return m
}

func TestNotificationsFanOut(t *testing.T) {
	a := newTestAPI(t)
	admin, _ := newAdmin(t, a)
	buyer, seller, v := seedReservation(t, a)

	// a new request reaches the owner and the administrator
	if k := kinds(notificationsOf(t, a, seller)); !k["reservation.requested"] {
		t.Fatalf("owner missing requested: %v", k)
	}
	if k := kinds(notificationsOf(t, a, admin)); !k["reservation.requested"] {
		t.Fatalf("admin missing requested: %v", k)
	}
	if k := kinds(notificationsOf(t, a, buyer)); k["reservation.requested"] {
		t.Fatalf("buyer should not see the request note: %v", k)
	}

	// the owner's decision reaches the buyer
	doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", buyer, map[string]any{"method": "cash"})
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/accept", seller, nil); rec.Code != http.StatusOK {
		t.Fatalf("accept: status = %d", rec.Code)
	}
	if k := kinds(notificationsOf(t, a, buyer)); !k["reservation.accepted"] {
		t.Fatalf("buyer missing accepted: %v", k)
	}

	// the administrator's confirmation reaches buyer, owner and admin
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/confirm", admin, nil); rec.Code != http.StatusOK {
		t.Fatalf("confirm: status = %d", rec.Code)
	}
	for name, token := range map[string]string{"buyer": buyer, "seller": seller, "admin": admin} {
		if k := kinds(notificationsOf(t, a, token)); !k["reservation.confirmed"] {
			t.Fatalf("%s missing confirmed: %v", name, k)
		}
	}
}

func TestNotificationsRejectAndCorrection(t *testing.T) {
	a := newTestAPI(t)
	admin, _ := newAdmin(t, a)
	buyer, seller, v := seedReservation(t, a)

	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/reject", seller, nil); rec.Code != http.StatusOK {
		t.Fatalf("reject: status = %d", rec.Code)
	}
	if k := kinds(notificationsOf(t, a, buyer)); !k["reservation.rejected"] {
		t.Fatalf("buyer missing rejected: %v", k)
	}
	if k := kinds(notificationsOf(t, a, admin)); !k["reservation.rejected"] {
		t.Fatalf("admin missing rejected: %v", k)
	}

	_, _, v2 := seedReservation(t, a)
	doJSON(t, a, "POST", "/reservations/"+itoa(v2.ID)+"/payment", buyer, map[string]any{"method": "cash"})
	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v2.ID)+"/request-correction", admin, nil); rec.Code != http.StatusConflict {
		t.Fatalf("correction before accept: status = %d, want 409", rec.Code)
	}
}

func TestNotificationsCorrectionNote(t *testing.T) {
	a := newTestAPI(t)
	buyer, seller, v := seedReservation(t, a)
	admin, _ := newAdmin(t, a)
	acceptReservation(t, a, buyer, seller, v)

	if rec := doJSON(t, a, "PATCH", "/admin/reservations/"+itoa(v.ID)+"/request-correction", admin, nil); rec.Code != http.StatusOK {
		t.Fatalf("correction: status = %d", rec.Code)
	}
	if k := kinds(notificationsOf(t, a, buyer)); !k["reservation.correction_requested"] {
		t.Fatalf("buyer missing correction_requested: %v", k)
	}
}

func TestNotificationsReadFlow(t *testing.T) {
	a := newTestAPI(t)
	buyer, seller, v := seedReservation(t, a)
	other := registerBuyer(t, a, "other@example.com", "secret123")

	// the buyer only hears about decisions: accept seeds one note
	if rec := doJSON(t, a, "PATCH", "/seller/reservations/"+itoa(v.ID)+"/accept", seller, nil); rec.Code != http.StatusOK {
		t.Fatalf("accept: status = %d", rec.Code)
	}

	mine := notificationsOf(t, a, buyer)
	if len(mine) == 0 {
		t.Fatalf("expected seed notifications")
	}
	for _, n := range mine {
		if n.Read {
			t.Fatalf("fresh notification reads as read: %+v", n)
		}
	}

	// the unread filter narrows, and reading clears it
	rec := doJSON(t, a, "GET", "/notifications?unread=true", buyer, nil)
	var unread []models.Notification
	decodeJSON(t, rec, &unread)
	if len(unread) != len(mine) {
		t.Fatalf("unread = %d, want %d", len(unread), len(mine))
	}

	rec = doJSON(t, a, "PATCH", "/notifications/"+itoa(mine[0].ID)+"/read", buyer, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("read: status = %d body %s", rec.Code, rec.Body.String())
	}
	var n models.Notification
	decodeJSON(t, rec, &n)
	if !n.Read {
		t.Fatalf("notification still unread: %+v", n)
	}

	rec = doJSON(t, a, "GET", "/notifications?unread=true", buyer, nil)
	decodeJSON(t, rec, &unread)
	if len(unread) != len(mine)-1 {
		t.Fatalf("unread after read = %d, want %d", len(unread), len(mine)-1)
	}

	// reading twice stays 200
	if rec := doJSON(t, a, "PATCH", "/notifications/"+itoa(mine[0].ID)+"/read", buyer, nil); rec.Code != http.StatusOK {
		t.Fatalf("re-read: status = %d, want 200", rec.Code)
	}

	// someone else's note is invisible
	if rec := doJSON(t, a, "PATCH", "/notifications/"+itoa(mine[1%len(mine)].ID)+"/read", other, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign read: status = %d, want 404", rec.Code)
	}
	if rec := doJSON(t, a, "PATCH", "/notifications/garbage/read", buyer, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: status = %d, want 400", rec.Code)
	}
	if rec := doJSON(t, a, "GET", "/notifications?unread=yes", buyer, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad unread: status = %d, want 400", rec.Code)
	}
	if rec := doJSON(t, a, "GET", "/notifications", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: status = %d, want 401", rec.Code)
	}
}

func TestAdminListNotifications(t *testing.T) {
	a := newTestAPI(t)
	buyer, _, _ := seedReservation(t, a)
	admin, _ := newAdmin(t, a)

	rec := doJSON(t, a, "GET", "/admin/notifications", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []models.Notification `json:"items"`
		Total int64                 `json:"total"`
	}
	decodeJSON(t, rec, &out)
	if out.Total == 0 || len(out.Items) == 0 {
		t.Fatalf("expected seeded notifications: %+v", out)
	}

	rec = doJSON(t, a, "GET", "/admin/notifications?kind=reservation.requested", admin, nil)
	decodeJSON(t, rec, &out)
	if out.Total == 0 {
		t.Fatalf("kind filter hid everything")
	}
	for _, n := range out.Items {
		if n.Kind != "reservation.requested" {
			t.Fatalf("kind filter leaked %+v", n)
		}
	}

	if rec := doJSON(t, a, "GET", "/admin/notifications?kind=bogus", admin, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad kind: status = %d, want 400", rec.Code)
	}
	if rec := doJSON(t, a, "GET", "/admin/notifications", buyer, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("buyer: status = %d, want 403", rec.Code)
	}
}
