package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// maxAdminNoteLen caps the rejection note an admin can attach to a
// reservation.
const maxAdminNoteLen = 500

// validReservationStatuses are the values accepted by the admin history
// filter.
var validReservationStatuses = map[string]bool{
	"pending":        true,
	"awaiting_admin": true,
	"confirmed":      true,
	"cancelled":      true,
}

// ListAdminReservations returns every reservation, newest first, optionally
// filtered by status. It is the admin's view over both buyers' and sellers'
// history.
func (a *API) ListAdminReservations(w http.ResponseWriter, r *http.Request) {
	limit, offset, errMsg := paginate(r)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	query := reservationSelect
	args := []any{}
	if status := r.URL.Query().Get("status"); status != "" {
		if !validReservationStatuses[status] {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		query += "\n\t\tWHERE r.status = ?"
		args = append(args, status)
	}
	query += "\n\t\tORDER BY r.id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := a.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		serverError(w, err)
		return
	}
	defer func() { _ = rows.Close() }()

	views := []reservationView{}
	for rows.Next() {
		var v reservationView
		if err := scanReservation(rows, &v); err != nil {
			serverError(w, err)
			return
		}
		views = append(views, v)
	}
	if err := rows.Err(); err != nil {
		serverError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, views)
}

// ApproveReservation is the admin's final approval: it confirms the
// reservation and approves its payment in one transaction.
func (a *API) ApproveReservation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid reservation id")
		return
	}

	approved := false
	err = withImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()

		var status string
		if err := conn.QueryRowContext(ctx,
			`SELECT status FROM reservations WHERE id = ?`, id).Scan(&status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "reservation not found")
				return errTxHandled
			}
			serverError(w, err)
			return errTxHandled
		}
		if status != "awaiting_admin" {
			writeError(w, http.StatusConflict, "reservation is not awaiting approval")
			return errTxHandled
		}

		var pStatus string
		if err := conn.QueryRowContext(ctx,
			`SELECT status FROM payments WHERE reservation_id = ?`, id).Scan(&pStatus); err != nil {
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

		res, err := conn.ExecContext(ctx,
			`UPDATE payments SET status = 'approved' WHERE reservation_id = ? AND status = 'pending'`, id)
		if err != nil {
			serverError(w, err)
			return errTxHandled
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeError(w, http.StatusConflict, "payment is not pending")
			return errTxHandled
		}
		if _, err := conn.ExecContext(ctx,
			`UPDATE reservations SET status = 'confirmed' WHERE id = ?`, id); err != nil {
			serverError(w, err)
			return errTxHandled
		}
		approved = true
		return nil
	})
	if err != nil {
		serverError(w, err)
		return
	}
	if !approved {
		return
	}

	v, err := a.reservationView(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// AdminRejectReservation sends an accepted reservation back to pending with an
// optional correction note, so the seller can fix the request and accept
// again. The payment stays pending: the request only moves forward through a
// fresh seller acceptance followed by an admin approval.
func (a *API) AdminRejectReservation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid reservation id")
		return
	}

	var in struct {
		AdminNote string `json:"admin_note"`
	}
	if err := decodeBody(w, r, &in); err != nil {
		writeBodyErr(w, err)
		return
	}
	in.AdminNote = strings.TrimSpace(in.AdminNote)
	if len(in.AdminNote) > maxAdminNoteLen {
		writeError(w, http.StatusBadRequest, "admin_note is too long")
		return
	}

	rejected := false
	err = withImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()

		var status string
		if err := conn.QueryRowContext(ctx,
			`SELECT status FROM reservations WHERE id = ?`, id).Scan(&status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "reservation not found")
				return errTxHandled
			}
			serverError(w, err)
			return errTxHandled
		}
		if status != "awaiting_admin" {
			writeError(w, http.StatusConflict, "reservation is not awaiting approval")
			return errTxHandled
		}

		if _, err := conn.ExecContext(ctx,
			`UPDATE reservations SET status = 'pending', admin_note = ? WHERE id = ?`, in.AdminNote, id); err != nil {
			serverError(w, err)
			return errTxHandled
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
