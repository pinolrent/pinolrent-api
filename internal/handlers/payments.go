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

// RecordPayment records the payment for the client's reservation, or resubmits
// it after the administrator sent it back for correction: an accepted
// reservation whose payment was rejected gets its proof replaced and the
// payment returns to pending, instead of forcing a new reservation.
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
	corrected := false
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
		if status != "pending" && status != "accepted" {
			writeError(w, http.StatusConflict, "reservation is not pending")
			return db.ErrTxHandled
		}

		var payID int64
		var payStatus string
		payErr := conn.QueryRowContext(ctx,
			`SELECT id, status FROM payments WHERE reservation_id = ?`, id).Scan(&payID, &payStatus)
		if payErr != nil && !errors.Is(payErr, sql.ErrNoRows) {
			serverError(w, payErr)
			return db.ErrTxHandled
		}
		if payErr == nil {
			if payStatus == "rejected" && status == "accepted" {
				if _, err := conn.ExecContext(ctx,
					`UPDATE payments SET method = ?, proof_url = ?, status = 'pending' WHERE id = ?`,
					in.Method, in.ProofURL, payID); err != nil {
					serverError(w, err)
					return db.ErrTxHandled
				}
				pid = payID
				payMethod = in.Method
				payProofURL = in.ProofURL
				corrected = true
				return nil
			}
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
	if !created && !corrected {
		return
	}

	code := http.StatusCreated
	if corrected {
		code = http.StatusOK
	}
	writeJSON(w, code, models.Payment{
		ID:            pid,
		ReservationID: id,
		Method:        payMethod,
		Status:        "pending",
		ProofURL:      payProofURL,
	})
}
