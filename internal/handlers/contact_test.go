package handlers

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestCarContactRequiresAuth(t *testing.T) {
	a := newTestAPI(t)
	seller := newSeller(t, a)
	car := createCar(t, a, seller, map[string]any{"name": "Toyota Yaris", "price_per_day": 100})

	rec := doJSON(t, a, "GET", "/cars/"+itoa(car.ID)+"/contact", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: status = %d, want 401", rec.Code)
	}
}

func TestCarContactReturnsWhatsappLink(t *testing.T) {
	a := newTestAPI(t)
	seller := newSeller(t, a)
	car := createCar(t, a, seller, map[string]any{"name": "Toyota Yaris", "price_per_day": 100})
	buyer := registerBuyer(t, a, "buyer@example.com", "secret123")

	rec := doJSON(t, a, "GET", "/cars/"+itoa(car.ID)+"/contact", buyer, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var out struct {
		WhatsappURL string `json:"whatsapp_url"`
	}
	decodeJSON(t, rec, &out)

	u, err := url.Parse(out.WhatsappURL)
	if err != nil {
		t.Fatalf("parse %q: %v", out.WhatsappURL, err)
	}
	if u.Host != "wa.me" {
		t.Fatalf("host = %q, want wa.me", u.Host)
	}
	if got := strings.TrimPrefix(u.Path, "/"); got != "56912345678" {
		t.Fatalf("number in link = %q, want 56912345678 (no leading +)", got)
	}
	if msg := u.Query().Get("text"); !strings.Contains(msg, "Toyota Yaris") {
		t.Fatalf("prefilled message %q should mention the car", msg)
	}
}

// TestCarContactWithoutPhone covers accounts created before phones were
// mandatory for sellers.
func TestCarContactWithoutPhone(t *testing.T) {
	a := newTestAPI(t)
	seller := registerSeller(t, a, "seller@example.com", "secret123")
	car := createCar(t, a, seller, map[string]any{"name": "Toyota Yaris", "price_per_day": 100})
	buyer := registerBuyer(t, a, "buyer@example.com", "secret123")

	if _, err := a.DB.ExecContext(context.Background(),
		`UPDATE users SET phone = '' WHERE email = ?`, "seller@example.com"); err != nil {
		t.Fatalf("clear phone: %v", err)
	}

	rec := doJSON(t, a, "GET", "/cars/"+itoa(car.ID)+"/contact", buyer, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestCarContactNotFound(t *testing.T) {
	a := newTestAPI(t)
	buyer := registerBuyer(t, a, "buyer@example.com", "secret123")

	for _, tc := range []struct {
		name   string
		path   string
		status int
	}{
		{"unknown car", "/cars/999999/contact", http.StatusNotFound},
		{"invalid id", "/cars/abc/contact", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, a, "GET", tc.path, buyer, nil)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.status, rec.Body.String())
			}
		})
	}

	seller := newSeller(t, a)
	car := createCar(t, a, seller, map[string]any{"name": "Auto Inactivo", "price_per_day": 100})
	rec := doJSON(t, a, "PATCH", "/seller/cars/"+itoa(car.ID), seller, map[string]any{"active": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("deactivate: status = %d", rec.Code)
	}
	rec = doJSON(t, a, "GET", "/cars/"+itoa(car.ID)+"/contact", buyer, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("inactive car: status = %d, want 404", rec.Code)
	}
}

// TestCarContactNotInPublicCatalog guards the privacy rule: neither the list
// nor the public detail may carry the phone or the contact link.
func TestCarContactNotInPublicCatalog(t *testing.T) {
	a := newTestAPI(t)
	seller := newSeller(t, a)
	car := createCar(t, a, seller, map[string]any{"name": "Toyota Yaris", "price_per_day": 100})

	for _, path := range []string{"/cars", "/cars/" + itoa(car.ID)} {
		rec := doJSON(t, a, "GET", path, "", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d", path, rec.Code)
		}
		body := rec.Body.String()
		if strings.Contains(body, "whatsapp") || strings.Contains(body, "56912345678") {
			t.Fatalf("GET %s leaks the seller contact: %s", path, body)
		}
	}
}
