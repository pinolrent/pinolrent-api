package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/models"
)

var validMethods = map[string]bool{"pos": true, "cash": true}

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
	if in.ProofURL != "" {
		if len(in.ProofURL) > maxURLLen {
			writeError(w, http.StatusBadRequest, "proof_url is too long")
			return
		}
		if !validURL(in.ProofURL) {
			writeError(w, http.StatusBadRequest, "invalid proof_url")
			return
		}
	}

	var pid int64
	var payMethod string
	var payProofURL string
	created := false
	err = withImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()

		var buyerID int64
		var status string
		if err := conn.QueryRowContext(ctx,
			`SELECT user_id, status FROM reservations WHERE id = ?`, id).Scan(&buyerID, &status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "reservation not found")
				return errTxHandled
			}
			serverError(w, err)
			return errTxHandled
		}
		if buyerID != u.ID {
			writeError(w, http.StatusNotFound, "reservation not found")
			return errTxHandled
		}
		if status != "pending" {
			writeError(w, http.StatusConflict, "reservation is not pending")
			return errTxHandled
		}

		var count int
		if err := conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM payments WHERE reservation_id = ?`, id).Scan(&count); err != nil {
			serverError(w, err)
			return errTxHandled
		}
		if count > 0 {
			writeError(w, http.StatusConflict, "payment already recorded")
			return errTxHandled
		}

		res, err := conn.ExecContext(ctx,
			`INSERT INTO payments (reservation_id, method, proof_url) VALUES (?, ?, ?)`,
			id, in.Method, in.ProofURL)
		if err != nil {
			if isUniqueViolation(err) {
				writeError(w, http.StatusConflict, "payment already recorded")
				return errTxHandled
			}
			serverError(w, err)
			return errTxHandled
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
	err = withImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()

		var status string
		var ownerID int64
		if err := conn.QueryRowContext(ctx, `
			SELECT r.status, c.owner_id FROM reservations r
			JOIN cars c ON c.id = r.car_id
			WHERE r.id = ?`, id).Scan(&status, &ownerID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "reservation not found")
				return errTxHandled
			}
			serverError(w, err)
			return errTxHandled
		}
		if ownerID != u.ID {
			writeError(w, http.StatusNotFound, "reservation not found")
			return errTxHandled
		}
		if status != "pending" {
			writeError(w, http.StatusConflict, "reservation is not pending")
			return errTxHandled
		}

		var pStatus string
		if err := conn.QueryRowContext(ctx, `SELECT method, status FROM payments WHERE reservation_id = ?`, id).Scan(new(string), &pStatus); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusConflict, "no payment recorded for this reservation")
				return errTxHandled
			}
			serverError(w, err)
			return errTxHandled
		}
		if pStatus != "pending" {
			writeError(w, http.StatusConflict, "payment is not pending")
			return errTxHandled
		}

		res, err := conn.ExecContext(ctx, `UPDATE payments SET status = 'approved' WHERE reservation_id = ? AND status = 'pending'`, id)
		if err != nil {
			serverError(w, err)
			return errTxHandled
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeError(w, http.StatusConflict, "no payment recorded for this reservation")
			return errTxHandled
		}
		if _, err := conn.ExecContext(ctx, `UPDATE reservations SET status = 'confirmed' WHERE id = ?`, id); err != nil {
			serverError(w, err)
			return errTxHandled
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
