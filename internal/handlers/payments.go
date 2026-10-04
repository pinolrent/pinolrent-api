package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/db"
	"github.com/pinolrent/pinolrent-api/internal/models"
)

// RecordPayment records a single pending payment for the client's reservation.
func (a *API) RecordPayment(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid reservation id")
		return
	}

	var in struct {
		Method   string `json:"method"`
		ProofURL string `json:"proof_url"`
	}
	if err := decodeBody(w, r, &in); err != nil {
		writeBodyErr(w, err)
		return
	}
	if !validMethods[in.Method] {
		writeError(w, http.StatusBadRequest, "method must be pos or cash")
		return
	}
	if msg := validateProofURL(in.ProofURL); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	var pid int64
	var payMethod string
	var payProofURL string
	created := false
	err = db.WithImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()

		var buyerID int64
		var status string
		if err := conn.QueryRowContext(ctx,
			`SELECT user_id, status FROM reservations WHERE id = ?`, id).Scan(&buyerID, &status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "reservation not found")
				return db.ErrTxHandled
			}
			serverError(w, err)
			return db.ErrTxHandled
		}
		if buyerID != u.ID {
			writeError(w, http.StatusNotFound, "reservation not found")
			return db.ErrTxHandled
		}
		if status != "pending" {
			writeError(w, http.StatusConflict, "reservation is not pending")
			return db.ErrTxHandled
		}

		var count int
		if err := conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM payments WHERE reservation_id = ?`, id).Scan(&count); err != nil {
			serverError(w, err)
			return db.ErrTxHandled
		}
		if count > 0 {
			writeError(w, http.StatusConflict, "payment already recorded")
			return db.ErrTxHandled
		}

		res, err := conn.ExecContext(ctx,
			`INSERT INTO payments (reservation_id, method, proof_url) VALUES (?, ?, ?)`,
			id, in.Method, in.ProofURL)
		if err != nil {
			if isUniqueViolation(err) {
				writeError(w, http.StatusConflict, "payment already recorded")
				return db.ErrTxHandled
			}
			serverError(w, err)
			return db.ErrTxHandled
		}
		pid, _ = res.LastInsertId()
		payMethod = in.Method
		payProofURL = in.ProofURL
		created = true
		return nil
	})
	if err != nil {
		serverError(w, err)
		return
	}
	if !created {
		return
	}

	writeJSON(w, http.StatusCreated, models.Payment{
		ID:            pid,
		ReservationID: id,
		Method:        payMethod,
		Status:        "pending",
		ProofURL:      payProofURL,
	})
}

// ConfirmReservation approves the reservation payment and marks the
// reservation as confirmed, atomically. Only the seller that owns the car can
// confirm its reservations.
func (a *API) ConfirmReservation(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid reservation id")
		return
	}

	confirmed := false
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

		var pStatus string
		if err := conn.QueryRowContext(ctx, `SELECT method, status FROM payments WHERE reservation_id = ?`, id).Scan(new(string), &pStatus); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusConflict, "no payment recorded for this reservation")
				return db.ErrTxHandled
			}
			serverError(w, err)
			return db.ErrTxHandled
		}
		if pStatus != "pending" {
			writeError(w, http.StatusConflict, "payment is not pending")
			return db.ErrTxHandled
		}

		res, err := conn.ExecContext(ctx, `UPDATE payments SET status = 'approved' WHERE reservation_id = ? AND status = 'pending'`, id)
		if err != nil {
			serverError(w, err)
			return db.ErrTxHandled
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeError(w, http.StatusConflict, "no payment recorded for this reservation")
			return db.ErrTxHandled
		}
		if _, err := conn.ExecContext(ctx, `UPDATE reservations SET status = 'confirmed' WHERE id = ?`, id); err != nil {
			serverError(w, err)
			return db.ErrTxHandled
		}
		confirmed = true
		return nil
	})
	if err != nil {
		serverError(w, err)
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

// RejectReservation rejects the recorded payment and cancels the reservation,
// atomically, freeing the dates. It is the escape hatch for a bogus or missing
// transfer: without it a paid pending reservation could only move forward to
// confirmed, and its dates would stay blocked forever. Only the seller that
// owns the car can reject.
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

		var pStatus string
		if err := conn.QueryRowContext(ctx, `SELECT status FROM payments WHERE reservation_id = ?`, id).Scan(&pStatus); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusConflict, "no payment recorded for this reservation")
				return db.ErrTxHandled
			}
			serverError(w, err)
			return db.ErrTxHandled
		}
		if pStatus != "pending" {
			writeError(w, http.StatusConflict, "payment is not pending")
			return db.ErrTxHandled
		}

		res, err := conn.ExecContext(ctx, `UPDATE payments SET status = 'rejected' WHERE reservation_id = ? AND status = 'pending'`, id)
		if err != nil {
			serverError(w, err)
			return db.ErrTxHandled
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeError(w, http.StatusConflict, "payment is not pending")
			return db.ErrTxHandled
		}
		if _, err := conn.ExecContext(ctx, `UPDATE reservations SET status = 'cancelled' WHERE id = ?`, id); err != nil {
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
