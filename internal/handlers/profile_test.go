package handlers

import (
	"net/http"
	"testing"
)

func TestUpdateMePhone(t *testing.T) {
	a := newTestAPI(t)
	token := registerBuyer(t, a, "buyer@example.com", "secret123")

	rec := doJSON(t, a, "PATCH", "/auth/me", token, map[string]any{"phone": "8765-4321"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Phone string `json:"phone"`
	}
	decodeJSON(t, rec, &out)
	if out.Phone != "+50587654321" {
		t.Fatalf("phone = %q, want +50587654321", out.Phone)
	}

	rec = doJSON(t, a, "GET", "/auth/me", token, nil)
	decodeJSON(t, rec, &out)
	if out.Phone != "+50587654321" {
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
	rec := doJSON(t, a, "PATCH", "/auth/me", "", map[string]any{"phone": "81234567"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestUpdateMeCannotChangeRoleOrEmail pins the writable surface: unknown fields
// are rejected by the strict decoder instead of being silently ignored.
func TestUpdateMeCannotChangeRoleOrEmail(t *testing.T) {
	a := newTestAPI(t)
	token := registerBuyer(t, a, "buyer@example.com", "secret123")

	rec := doJSON(t, a, "PATCH", "/auth/me", token, map[string]any{"roles": []string{"seller"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("roles change: status = %d, want 400", rec.Code)
	}
	rec = doJSON(t, a, "PATCH", "/auth/me", token, map[string]any{"email": "other@example.com"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("email change: status = %d, want 400", rec.Code)
	}
}

// TestUpdateMePhoneDoesNotGrantSeller pins the anti-escalation rule: saving a
// phone never grants the seller role, only become-seller does.
func TestUpdateMePhoneDoesNotGrantSeller(t *testing.T) {
	a := newTestAPI(t)
	token := registerBuyer(t, a, "buyer@example.com", "secret123")

	rec := doJSON(t, a, "PATCH", "/auth/me", token, map[string]any{"phone": "+50581234567"})
	if rec.Code != http.StatusOK {
		t.Fatalf("save phone: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Roles []string `json:"roles"`
	}
	decodeJSON(t, rec, &out)
	if len(out.Roles) != 1 || out.Roles[0] != "buyer" {
		t.Fatalf("roles = %v, want [buyer]", out.Roles)
	}
	if rec := doJSON(t, a, "POST", "/seller/cars", token, map[string]any{
		"name": "X", "price_per_day": 1,
	}); rec.Code != http.StatusForbidden {
		t.Fatalf("seller route: status = %d, want 403", rec.Code)
	}
}
