package db

import "testing"

func TestValidReservationStatus(t *testing.T) {
	for _, s := range []string{
		ReservationPending,
		ReservationAccepted,
		ReservationConfirmed,
		ReservationRejected,
		ReservationCancelled,
	} {
		if !ValidReservationStatus(s) {
			t.Errorf("ValidReservationStatus(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "PENDING", "approved", "done"} {
		if ValidReservationStatus(s) {
			t.Errorf("ValidReservationStatus(%q) = true, want false", s)
		}
	}
}

func TestValidPaymentStatus(t *testing.T) {
	for _, s := range []string{PaymentPending, PaymentApproved, PaymentRejected} {
		if !ValidPaymentStatus(s) {
			t.Errorf("ValidPaymentStatus(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "PENDING", "confirmed", "done"} {
		if ValidPaymentStatus(s) {
			t.Errorf("ValidPaymentStatus(%q) = true, want false", s)
		}
	}
}
