package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

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

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid reservation id")
		return
	}

	accepted := false
	err = db.WithImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()

		var status string
		var ownerID int64
		if err := conn.QueryRowContext(ctx, `
			SELECT r.status, c.owner_id FROM reservations r
			JOIN cars c ON c.id = r.car_id
			WHERE r.id = ?`, id).Scan(&status, &ownerID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "reservation not found")
				return db.ErrTxHandled
			}
			serverError(w, err)
			return db.ErrTxHandled
		}
		if ownerID != u.ID {
			writeError(w, http.StatusNotFound, "reservation not found")
			return db.ErrTxHandled
		}
		if status != "pending" {
			writeError(w, http.StatusConflict, "reservation is not pending")
			return db.ErrTxHandled
		}

		if _, err := conn.ExecContext(ctx, `UPDATE reservations SET status = 'accepted' WHERE id = ?`, id); err != nil {
			serverError(w, err)
			return db.ErrTxHandled
		}
		accepted = true
		return nil
	})
	if err != nil {
		serverError(w, err)
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

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid reservation id")
		return
	}

	rejected := false
	err = db.WithImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()

		var status string
		var ownerID int64
		if err := conn.QueryRowContext(ctx, `
			SELECT r.status, c.owner_id FROM reservations r
			JOIN cars c ON c.id = r.car_id
			WHERE r.id = ?`, id).Scan(&status, &ownerID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "reservation not found")
				return db.ErrTxHandled
			}
			serverError(w, err)
			return db.ErrTxHandled
		}
		if ownerID != u.ID {
			writeError(w, http.StatusNotFound, "reservation not found")
			return db.ErrTxHandled
		}
		if status != "pending" {
			writeError(w, http.StatusConflict, "reservation is not pending")
			return db.ErrTxHandled
		}

		if _, err := conn.ExecContext(ctx,
			`UPDATE payments SET status = 'rejected' WHERE reservation_id = ? AND status = 'pending'`, id); err != nil {
			serverError(w, err)
			return db.ErrTxHandled
		}
		if _, err := conn.ExecContext(ctx, `UPDATE reservations SET status = 'rejected' WHERE id = ?`, id); err != nil {
			serverError(w, err)
			return db.ErrTxHandled
		}
		rejected = true
		return nil
	})
	if err != nil {
		serverError(w, err)
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
