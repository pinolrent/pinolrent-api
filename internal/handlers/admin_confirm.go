package handlers

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/db"
)

// AdminConfirmReservation is the second half of the two-step confirmation: the
// administrator validates the payment of a reservation the owner already
// accepted, approving the payment and marking the reservation as confirmed,
// atomically. A reservation only becomes confirmed through this endpoint.
func (a *API) AdminConfirmReservation(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	id, errMsg := pathID(r, "reservation")
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	confirmed := false
	err := db.WithImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()

		var status string
		var buyerID int64
		var ownerID int64
		if err := conn.QueryRowContext(ctx, `
			SELECT r.status, r.user_id, c.owner_id FROM reservations r
			JOIN cars c ON c.id = r.car_id
			WHERE r.id = ?`, id).Scan(&status, &buyerID, &ownerID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &statusError{http.StatusNotFound, "reservation not found"}
			}
			return err
		}
		if status != db.ReservationAccepted {
			return &statusError{http.StatusConflict, "reservation is not accepted"}
		}

		var pStatus string
		if err := conn.QueryRowContext(ctx, `SELECT status FROM payments WHERE reservation_id = ?`, id).Scan(&pStatus); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &statusError{http.StatusConflict, "no payment recorded for this reservation"}
			}
			return err
		}
		if pStatus != db.PaymentPending {
			return &statusError{http.StatusConflict, "payment is not pending"}
		}

		if _, err := conn.ExecContext(ctx, `UPDATE payments SET status = ? WHERE reservation_id = ? AND status = ?`, db.PaymentApproved, id, db.PaymentPending); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, `UPDATE reservations SET status = ? WHERE id = ?`, db.ReservationConfirmed, id); err != nil {
			return err
		}
		if err := a.auditAction(ctx, conn, u.ID, auditActionReservationConfirm, targetReservations, id, ""); err != nil {
			return err
		}
		admins, err := adminIDs(ctx, conn)
		if err != nil {
			return err
		}
		if err := notifyUsers(ctx, conn, append(admins, buyerID, ownerID), notifyConfirmed, id); err != nil {
			return err
		}
		confirmed = true
		return nil
	})
	if writeTxErr(w, err) {
		return
	}
	if !confirmed {
		return
	}

	v, err := a.reservationView(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// AdminRequestCorrection sends the payment of an accepted reservation back to
// the buyer: the payment moves to rejected but the reservation stays accepted,
// so the buyer can attach a corrected proof instead of starting over.
func (a *API) AdminRequestCorrection(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	id, errMsg := pathID(r, "reservation")
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	corrected := false
	err := db.WithImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()

		var status string
		var buyerID int64
		if err := conn.QueryRowContext(ctx,
			`SELECT status, user_id FROM reservations WHERE id = ?`, id).Scan(&status, &buyerID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &statusError{http.StatusNotFound, "reservation not found"}
			}
			return err
		}
		if status != db.ReservationAccepted {
			return &statusError{http.StatusConflict, "reservation is not accepted"}
		}

		res, err := conn.ExecContext(ctx, `UPDATE payments SET status = ? WHERE reservation_id = ? AND status = ?`, db.PaymentRejected, id, db.PaymentPending)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return &statusError{http.StatusConflict, "payment is not pending"}
		}
		if err := a.auditAction(ctx, conn, u.ID, auditActionReservationRequestCorrection, targetReservations, id, ""); err != nil {
			return err
		}
		if err := notifyUsers(ctx, conn, []int64{buyerID}, notifyCorrectionRequested, id); err != nil {
			return err
		}
		corrected = true
		return nil
	})
	if writeTxErr(w, err) {
		return
	}
	if !corrected {
		return
	}

	v, err := a.reservationView(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
