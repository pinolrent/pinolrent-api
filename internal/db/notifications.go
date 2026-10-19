package db

// Notification kinds, mirroring the CHECK constraint from migration 00011
// (widened by 00012). Bind these instead of string literals so a typo fails
// at review time, like the reservation and payment statuses.
const (
	NotifyRequested           = "reservation.requested"
	NotifyAccepted            = "reservation.accepted"
	NotifyRejected            = "reservation.rejected"
	NotifyConfirmed           = "reservation.confirmed"
	NotifyCorrectionRequested = "reservation.correction_requested"
	NotifyCancelled           = "reservation.cancelled"
)

// ValidNotifyKind reports whether s is a known notification kind.
func ValidNotifyKind(s string) bool {
	switch s {
	case NotifyRequested,
		NotifyAccepted,
		NotifyRejected,
		NotifyConfirmed,
		NotifyCorrectionRequested,
		NotifyCancelled:
		return true
	}
	return false
}
