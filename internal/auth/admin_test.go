package auth

import (
	"context"
	"slices"
	"testing"
)

// rolesOf reads the role memberships of an account, the same way RequireAuth
// does on every request.
func rolesOf(t *testing.T, a *Auth, userID int64) []string {
	t.Helper()
	roles, err := a.UserRoles(context.Background(), userID)
	if err != nil {
		t.Fatalf("roles of %d: %v", userID, err)
	}
	return roles
}

func TestSyncAdminRolesGrantsToListedAccounts(t *testing.T) {
	a := newTestAuth(t)
	listed := seedUser(t, a, "admin@example.com", "buyer")
	other := seedUser(t, a, "other@example.com", "buyer")

	granted, revoked, err := a.SyncAdminRoles(context.Background(), []string{"admin@example.com"})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if granted != 1 || revoked != 0 {
		t.Fatalf("granted/revoked = %d/%d, want 1/0", granted, revoked)
	}
	if !slices.Contains(rolesOf(t, a, listed), "admin") {
		t.Fatalf("listed account not admin: %v", rolesOf(t, a, listed))
	}
	if slices.Contains(rolesOf(t, a, other), "admin") {
		t.Fatalf("unlisted account became admin: %v", rolesOf(t, a, other))
	}
	// The membership is additive: the account keeps working as a buyer.
	if roles := rolesOf(t, a, listed); !slices.Contains(roles, "buyer") {
		t.Fatalf("buyer role lost: %v", roles)
	}
}

// TestSyncAdminRolesIsCaseInsensitive mirrors how accounts store their email:
// lower-cased, with a unique index on lower(email). Two spellings are one
// account, so the allow-list must agree.
func TestSyncAdminRolesIsCaseInsensitive(t *testing.T) {
	a := newTestAuth(t)
	id := seedUser(t, a, "admin@example.com", "buyer")

	if _, _, err := a.SyncAdminRoles(context.Background(), []string{"  Admin@Example.COM  "}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !slices.Contains(rolesOf(t, a, id), "admin") {
		t.Fatalf("admin not granted: %v", rolesOf(t, a, id))
	}
}

// TestSyncAdminRolesRevokesEveryoneElse is the property that makes the
// allow-list authoritative: dropping an address and restarting takes the
// access away, with no database surgery in between.
func TestSyncAdminRolesRevokesEveryoneElse(t *testing.T) {
	a := newTestAuth(t)
	kept := seedUser(t, a, "kept@example.com", "buyer")
	dropped := seedUser(t, a, "dropped@example.com", "buyer")

	if _, _, err := a.SyncAdminRoles(context.Background(), []string{"kept@example.com", "dropped@example.com"}); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if !slices.Contains(rolesOf(t, a, dropped), "admin") {
		t.Fatal("dropped account is not admin after first sync")
	}

	// The second boot runs with a shorter list.
	granted, revoked, err := a.SyncAdminRoles(context.Background(), []string{"kept@example.com"})
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if granted != 0 || revoked != 1 {
		t.Fatalf("granted/revoked = %d/%d, want 0/1", granted, revoked)
	}
	if slices.Contains(rolesOf(t, a, dropped), "admin") {
		t.Fatalf("dropped account kept admin: %v", rolesOf(t, a, dropped))
	}
	if !slices.Contains(rolesOf(t, a, kept), "admin") {
		t.Fatalf("kept account lost admin: %v", rolesOf(t, a, kept))
	}
}

// TestSyncAdminRolesEmptyListRevokesAll pins that an unconfigured deployment
// has no administrator, instead of inheriting one from a previous run.
func TestSyncAdminRolesEmptyListRevokesAll(t *testing.T) {
	a := newTestAuth(t)
	id := seedUser(t, a, "stale@example.com", "buyer", "admin")

	granted, revoked, err := a.SyncAdminRoles(context.Background(), nil)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if granted != 0 || revoked != 1 {
		t.Fatalf("granted/revoked = %d/%d, want 0/1", granted, revoked)
	}
	if slices.Contains(rolesOf(t, a, id), "admin") {
		t.Fatalf("admin survived an empty allow-list: %v", rolesOf(t, a, id))
	}
	if !slices.Contains(rolesOf(t, a, id), "buyer") {
		t.Fatalf("buyer role lost: %v", rolesOf(t, a, id))
	}
}

// TestSyncAdminRolesIsIdempotent matters because it runs on every boot: a
// server that restarts often must not report changes that did not happen, and
// must not duplicate rows.
func TestSyncAdminRolesIsIdempotent(t *testing.T) {
	a := newTestAuth(t)
	id := seedUser(t, a, "admin@example.com", "buyer")

	emails := []string{"admin@example.com"}
	if _, _, err := a.SyncAdminRoles(context.Background(), emails); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	granted, revoked, err := a.SyncAdminRoles(context.Background(), emails)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if granted != 0 || revoked != 0 {
		t.Fatalf("second sync reported granted/revoked = %d/%d, want 0/0", granted, revoked)
	}
	if roles := rolesOf(t, a, id); len(roles) != 2 {
		t.Fatalf("roles = %v, want [admin buyer]", roles)
	}
}

// TestSyncAdminRolesIgnoresUnknownAddresses keeps the startup from failing
// because the allow-list names an account that has not registered yet. The
// role lands when that account registers.
func TestSyncAdminRolesIgnoresUnknownAddresses(t *testing.T) {
	a := newTestAuth(t)
	id := seedUser(t, a, "real@example.com", "buyer")

	granted, revoked, err := a.SyncAdminRoles(context.Background(), []string{
		"real@example.com", "not-registered-yet@example.com",
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if granted != 1 || revoked != 0 {
		t.Fatalf("granted/revoked = %d/%d, want 1/0", granted, revoked)
	}
	if !slices.Contains(rolesOf(t, a, id), "admin") {
		t.Fatalf("admin not granted: %v", rolesOf(t, a, id))
	}
}

// TestSyncAdminRolesLeavesOtherRolesAlone pins that the sync only ever touches
// the admin membership, whatever it does to the rest of the account.
func TestSyncAdminRolesLeavesOtherRolesAlone(t *testing.T) {
	a := newTestAuth(t)
	seller := seedUser(t, a, "seller@example.com", "buyer", "seller")
	plain := seedUser(t, a, "plain@example.com", "buyer")

	if _, _, err := a.SyncAdminRoles(context.Background(), []string{"seller@example.com", "plain@example.com"}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	for _, id := range []int64{seller, plain} {
		roles := rolesOf(t, a, id)
		if !slices.Contains(roles, "admin") || !slices.Contains(roles, "buyer") {
			t.Fatalf("roles of %d = %v, want admin and buyer", id, roles)
		}
	}
	if roles := rolesOf(t, a, seller); !slices.Contains(roles, "seller") {
		t.Fatalf("seller role lost: %v", roles)
	}
}
