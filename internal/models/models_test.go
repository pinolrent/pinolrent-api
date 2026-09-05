package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUserHidesPasswordHash(t *testing.T) {
	b, err := json.Marshal(User{ID: 1, Email: "u@example.com", PasswordHash: "secret", Role: "buyer"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "secret") {
		t.Fatalf("password hash leaked: %s", b)
	}
	if strings.Contains(string(b), "password_hash") {
		t.Fatalf("password_hash key present: %s", b)
	}
}

func TestOmitemptyFields(t *testing.T) {
	b, err := json.Marshal(Car{ID: 1, OwnerID: 2, Name: "Yaris", PricePerDay: 45000, Active: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "photo_url") {
		t.Fatalf("empty photo_url should be omitted: %s", b)
	}

	b, err = json.Marshal(Payment{ID: 1, ReservationID: 2, Method: "pos", Status: "pending"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "proof_url") {
		t.Fatalf("empty proof_url should be omitted: %s", b)
	}
}

func TestSnakeCaseKeys(t *testing.T) {
	b, err := json.Marshal(Reservation{ID: 1, UserID: 2, CarID: 3, StartDate: "2027-01-10", EndDate: "2027-01-12", Status: "pending"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"user_id", "car_id", "start_date", "end_date"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("key %q missing in %s", k, b)
		}
	}
}
