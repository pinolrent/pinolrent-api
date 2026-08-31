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

	ctx := r.Context()
	conn, err := a.DB.Conn(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		serverError(w, err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()

	var buyerID int64
	var status string
	err = conn.QueryRowContext(ctx,
		`SELECT user_id, status FROM reservations WHERE id = ?`, id).Scan(&buyerID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "reservation not found")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if buyerID != u.ID {
		writeError(w, http.StatusNotFound, "reservation not found")
		return
	}
	if status == "cancelled" {
		writeError(w, http.StatusConflict, "cancelled reservation cannot be paid")
		return
	}

	var count int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM payments WHERE reservation_id = ?`, id).Scan(&count); err != nil {
		serverError(w, err)
		return
	}
	if count > 0 {
		writeError(w, http.StatusConflict, "payment already recorded")
		return
	}

	res, err := conn.ExecContext(ctx,
		`INSERT INTO payments (reservation_id, method, proof_url) VALUES (?, ?, ?)`,
		id, in.Method, in.ProofURL)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "payment already recorded")
			return
		}
		serverError(w, err)
		return
	}
	pid, _ := res.LastInsertId()

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		serverError(w, err)
		return
	}
	committed = true
	_ = conn.Close()

	writeJSON(w, http.StatusCreated, models.Payment{
		ID:            pid,
		ReservationID: id,
		Method:        in.Method,
		Status:        "pending",
		ProofURL:      in.ProofURL,
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

	ctx := r.Context()
	conn, err := a.DB.Conn(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		serverError(w, err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()

	var status string
	var ownerID int64
	err = conn.QueryRowContext(ctx, `
		SELECT r.status, c.owner_id FROM reservations r
		JOIN cars c ON c.id = r.car_id
		WHERE r.id = ?`, id).Scan(&status, &ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "reservation not found")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if ownerID != u.ID {
		writeError(w, http.StatusNotFound, "reservation not found")
		return
	}
	if status != "pending" {
		writeError(w, http.StatusConflict, "reservation is not pending")
		return
	}

	var method string
	err = conn.QueryRowContext(ctx, `SELECT method FROM payments WHERE reservation_id = ?`, id).Scan(&method)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusConflict, "no payment recorded for this reservation")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	if _, err := conn.ExecContext(ctx, `UPDATE payments SET status = 'approved' WHERE reservation_id = ?`, id); err != nil {
		serverError(w, err)
		return
	}
	if _, err := conn.ExecContext(ctx, `UPDATE reservations SET status = 'confirmed' WHERE id = ?`, id); err != nil {
		serverError(w, err)
		return
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		serverError(w, err)
		return
	}
	committed = true
	_ = conn.Close()

	v, err := a.reservationView(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
