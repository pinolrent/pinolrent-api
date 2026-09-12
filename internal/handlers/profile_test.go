package handlers

import (
	"net/http"
	"testing"
)

func TestUpdateMePhone(t *testing.T) {
	a := newTestAPI(t)
	token := registerBuyer(t, a, "buyer@example.com", "secret123")

	rec := doJSON(t, a, "PATCH", "/auth/me", token, map[string]any{"phone": "9 8765 4321"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Phone string `json:"phone"`
	}
	decodeJSON(t, rec, &out)
	if out.Phone != "+56987654321" {
		t.Fatalf("phone = %q, want +56987654321", out.Phone)
	}

	rec = doJSON(t, a, "GET", "/auth/me", token, nil)
	decodeJSON(t, rec, &out)
	if out.Phone != "+56987654321" {
		t.Fatalf("phone did not persist: %q", out.Phone)
	}
}

func TestUpdateMeValidatesPhone(t *testing.T) {
	a := newTestAPI(t)
	buyer := registerBuyer(t, a, "buyer@example.com", "secret123")
	seller := newSeller(t, a)

	rec := doJSON(t, a, "PATCH", "/auth/me", buyer, map[string]any{"phone": "no-es-numero"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid phone: status = %d, want 400", rec.Code)
	}

	// A buyer may clear the number; a seller cannot, because it is the only
	// way buyers have to reach them.
	rec = doJSON(t, a, "PATCH", "/auth/me", buyer, map[string]any{"phone": ""})
	if rec.Code != http.StatusOK {
		t.Fatalf("buyer clear: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, a, "PATCH", "/auth/me", seller, map[string]any{"phone": ""})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("seller clear: status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestUpdateMeRequiresAuth(t *testing.T) {
	a := newTestAPI(t)
	rec := doJSON(t, a, "PATCH", "/auth/me", "", map[string]any{"phone": "912345678"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestUpdateMeCannotChangeRoleOrEmail pins the writable surface: unknown fields
// are rejected by the strict decoder instead of being silently ignored.
func TestUpdateMeCannotChangeRoleOrEmail(t *testing.T) {
	a := newTestAPI(t)
	token := registerBuyer(t, a, "buyer@example.com", "secret123")

	rec := doJSON(t, a, "PATCH", "/auth/me", token, map[string]any{"role": "seller"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("role change: status = %d, want 400", rec.Code)
	}
	rec = doJSON(t, a, "PATCH", "/auth/me", token, map[string]any{"email": "other@example.com"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("email change: status = %d, want 400", rec.Code)
	}
}
