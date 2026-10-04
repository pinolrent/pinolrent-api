package handlers

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/db"
)

// AcceptReservation marks the buyer's pending request as accepted by the owner
// of the car. It is the first half of the two-step confirmation: the
// reservation still needs the administrator to validate the payment before it
// becomes confirmed. Only the seller that owns the car can accept, and only a
// pending reservation moves.
func (a *API) AcceptReservation(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	id, errMsg := pathID(r, "reservation")
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	accepted := false
	err := db.WithImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()

		var status string
		var ownerID int64
		var buyerID int64
		if err := conn.QueryRowContext(ctx, `
			SELECT r.status, c.owner_id, r.user_id FROM reservations r
			JOIN cars c ON c.id = r.car_id
			WHERE r.id = ?`, id).Scan(&status, &ownerID, &buyerID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &statusError{http.StatusNotFound, "reservation not found"}
			}
			return err
		}
		if ownerID != u.ID {
			return &statusError{http.StatusNotFound, "reservation not found"}
		}
		if status != db.ReservationPending {
			return &statusError{http.StatusConflict, "reservation is not pending"}
		}

		if _, err := conn.ExecContext(ctx, `UPDATE reservations SET status = ? WHERE id = ?`, db.ReservationAccepted, id); err != nil {
			return err
		}
		if err := a.auditAction(ctx, conn, u.ID, auditActionReservationAccept, targetReservations, id, ""); err != nil {
			return err
		}
		if err := notifyUsers(ctx, conn, []int64{buyerID}, notifyAccepted, id); err != nil {
			return err
		}
		accepted = true
		return nil
	})
	if writeTxErr(w, err) {
		return
	}
	if !accepted {
		return
	}

	v, err := a.reservationView(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// RejectReservation turns the buyer's pending request down, atomically,
// freeing the dates. Unlike a buyer cancellation this is the owner's decision,
// so it does not need a recorded payment: when one exists and is still
// pending it is rejected alongside the reservation as an audit trail. Only
// the seller that owns the car can reject, and only a pending reservation
// moves.
func (a *API) RejectReservation(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	id, errMsg := pathID(r, "reservation")
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	rejected := false
	err := db.WithImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()

		var status string
		var ownerID int64
		var buyerID int64
		if err := conn.QueryRowContext(ctx, `
			SELECT r.status, c.owner_id, r.user_id FROM reservations r
			JOIN cars c ON c.id = r.car_id
			WHERE r.id = ?`, id).Scan(&status, &ownerID, &buyerID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &statusError{http.StatusNotFound, "reservation not found"}
			}
			return err
		}
		if ownerID != u.ID {
			return &statusError{http.StatusNotFound, "reservation not found"}
		}
		if status != db.ReservationPending {
			return &statusError{http.StatusConflict, "reservation is not pending"}
		}

		if _, err := conn.ExecContext(ctx,
			`UPDATE payments SET status = ? WHERE reservation_id = ? AND status = ?`, db.PaymentRejected, id, db.PaymentPending); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, `UPDATE reservations SET status = ? WHERE id = ?`, db.ReservationRejected, id); err != nil {
			return err
		}
		if err := a.auditAction(ctx, conn, u.ID, auditActionReservationReject, targetReservations, id, ""); err != nil {
			return err
		}
		admins, err := adminIDs(ctx, conn)
		if err != nil {
			return err
		}
		if err := notifyUsers(ctx, conn, append(admins, buyerID), notifyRejected, id); err != nil {
			return err
		}
		rejected = true
		return nil
	})
	if writeTxErr(w, err) {
		return
	}
	if !rejected {
		return
	}

	v, err := a.reservationView(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
