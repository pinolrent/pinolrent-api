package db

// Notification kinds, mirroring the CHECK constraint from migration 00011
// (widened by 00012 and 00013). Bind these instead of string literals so a
// typo fails at review time, like the reservation and payment statuses.
const (
	NotifyRequested           = "reservation.requested"
	NotifyAccepted            = "reservation.accepted"
	NotifyRejected            = "reservation.rejected"
	NotifyConfirmed           = "reservation.confirmed"
	NotifyCorrectionRequested = "reservation.correction_requested"
	NotifyCancelled           = "reservation.cancelled"
	NotifyCorrectionSubmitted = "reservation.correction_submitted"
)

// ValidNotifyKind reports whether s is a known notification kind.
func ValidNotifyKind(s string) bool {
	switch s {
	case NotifyRequested,
		NotifyAccepted,
		NotifyRejected,
		NotifyConfirmed,
		NotifyCorrectionRequested,
		NotifyCancelled,
		NotifyCorrectionSubmitted:
		return true
	}
	return false
}
