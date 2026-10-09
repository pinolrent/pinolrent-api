package handlers

import (
	"net/http"
	"testing"
	"time"

	// Test-side copy of the zone database, mirroring the import cmd/api
	// carries for the deployment image.
	_ "time/tzdata"
)

// TestTodayStrUsesBusinessZone pins the boundary the bug lived in: at 02:00
// UTC the UTC date has already rolled over, but it is still the previous
// evening in Managua (UTC-6). The API must count the user's day, so a booking
// for that day is valid and not "in the past".
func TestTodayStrUsesBusinessZone(t *testing.T) {
	a := newTestAPI(t)
	loc, err := time.LoadLocation("America/Managua")
	if err != nil {
		t.Fatalf("load zone: %v", err)
	}
	a.Location = loc

	restore := timeNow
	t.Cleanup(func() { timeNow = restore })
	// 2026-06-16 02:00 UTC = 2026-06-15 20:00 in Managua.
	timeNow = func() time.Time { return time.Date(2026, 6, 16, 2, 0, 0, 0, time.UTC) }

	if got := a.todayStr(); got != "2026-06-15" {
		t.Fatalf("todayStr = %q, want 2026-06-15 (the day in Managua)", got)
	}

	// With no zone configured the old behaviour stands: UTC.
	a.Location = nil
	if got := a.todayStr(); got != "2026-06-16" {
		t.Fatalf("todayStr (nil location) = %q, want 2026-06-16 (UTC)", got)
	}
}

// TestCreateReservationSameDayAfterUTCMidnight books "today" at the moment
// UTC has already rolled over: before the fix the request was rejected as
// past, now it is accepted because today is counted in the business zone.
func TestCreateReservationSameDayAfterUTCMidnight(t *testing.T) {
	a := newTestAPI(t)
	loc, err := time.LoadLocation("America/Managua")
	if err != nil {
		t.Fatalf("load zone: %v", err)
	}
	a.Location = loc

	restore := timeNow
	t.Cleanup(func() { timeNow = restore })
	// 2026-06-16 02:00 UTC = 2026-06-15 20:00 in Managua, a plausible
	// moment to book a car for the next morning.
	timeNow = func() time.Time { return time.Date(2026, 6, 16, 2, 0, 0, 0, time.UTC) }

	car := seedCar(t, a)
	token := registerBuyer(t, a, "evening@example.com", "secret123")

	rec := doJSON(t, a, "POST", "/reservations", token, map[string]any{
		"car_id": car.ID, "start_date": "2026-06-15", "end_date": "2026-06-17",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("same-day booking after UTC midnight: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}

	// The day before the business day stays rejected.
	rec = doJSON(t, a, "POST", "/reservations", token, map[string]any{
		"car_id": car.ID, "start_date": "2026-06-14", "end_date": "2026-06-17",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("past booking: status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}
