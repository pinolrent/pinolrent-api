package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/models"
)

var validMethods = map[string]bool{"pos": true, "cash": true}

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
	if err := decodeBody(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !validMethods[in.Method] {
		writeError(w, http.StatusBadRequest, "method must be pos or cash")
		return
	}
	if in.ProofURL != "" {
		if _, err := url.ParseRequestURI(in.ProofURL); err != nil {
			writeError(w, http.StatusBadRequest, "invalid proof_url")
			return
		}
	}

	var ownerID int64
	var status string
	err = a.DB.QueryRow(`SELECT user_id, status FROM reservations WHERE id = ?`, id).Scan(&ownerID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "reservation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server error")
		return
	}
	if ownerID != u.ID {
		writeError(w, http.StatusNotFound, "reservation not found")
		return
	}
	if status == "cancelled" {
		writeError(w, http.StatusConflict, "cancelled reservation cannot be paid")
		return
	}

	var count int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM payments WHERE reservation_id = ?`, id).Scan(&count); err != nil {
		writeError(w, http.StatusInternalServerError, "server error")
		return
	}
	if count > 0 {
		writeError(w, http.StatusConflict, "payment already recorded")
		return
	}

	res, err := a.DB.Exec(`INSERT INTO payments (reservation_id, method, proof_url) VALUES (?, ?, ?)`,
		id, in.Method, in.ProofURL)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "payment already recorded")
			return
		}
		writeError(w, http.StatusInternalServerError, "server error")
		return
	}
	pid, _ := res.LastInsertId()

	writeJSON(w, http.StatusCreated, models.Payment{
		ID:            pid,
		ReservationID: id,
		Method:        in.Method,
		Status:        "pending",
		ProofURL:      in.ProofURL,
	})
}

func (a *API) ConfirmReservation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid reservation id")
		return
	}

	ctx := r.Context()
	conn, err := a.DB.Conn(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server error")
		return
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		writeError(w, http.StatusInternalServerError, "server error")
		return
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()

	var status string
	err = conn.QueryRowContext(ctx, `SELECT status FROM reservations WHERE id = ?`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "reservation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server error")
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
		writeError(w, http.StatusInternalServerError, "server error")
		return
	}

	if _, err := conn.ExecContext(ctx, `UPDATE payments SET status = 'approved' WHERE reservation_id = ?`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "server error")
		return
	}
	if _, err := conn.ExecContext(ctx, `UPDATE reservations SET status = 'confirmed' WHERE id = ?`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "server error")
		return
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		writeError(w, http.StatusInternalServerError, "server error")
		return
	}
	committed = true
	_ = conn.Close()

	v, err := a.reservationView(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server error")
		return
	}
	writeJSON(w, http.StatusOK, v)
}