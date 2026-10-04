package db

// Reservation statuses, mirroring the CHECK constraint from migration
// 00010. Compare and bind these instead of string literals so a typo fails
// at review time (wrong constant) rather than silently at runtime.
const (
	ReservationPending   = "pending"
	ReservationAccepted  = "accepted"
	ReservationConfirmed = "confirmed"
	ReservationRejected  = "rejected"
	ReservationCancelled = "cancelled"
)

// Payment statuses, mirroring the CHECK constraint from migration 00001.
const (
	PaymentPending  = "pending"
	PaymentApproved = "approved"
	PaymentRejected = "rejected"
)

// Roles, mirroring the memberships stored in user_roles. The admin role is
// granted from the ADMIN_EMAILS allow-list only; see auth.SyncAdminRoles.
const (
	RoleBuyer  = "buyer"
	RoleSeller = "seller"
	RoleAdmin  = "admin"
)

// ValidReservationStatus reports whether s is a known reservation status.
func ValidReservationStatus(s string) bool {
	switch s {
	case ReservationPending,
		ReservationAccepted,
		ReservationConfirmed,
		ReservationRejected,
		ReservationCancelled:
		return true
	}
	return false
}

// ValidPaymentStatus reports whether s is a known payment status.
func ValidPaymentStatus(s string) bool {
	switch s {
	case PaymentPending,
		PaymentApproved,
		PaymentRejected:
		return true
	}
	return false
}
