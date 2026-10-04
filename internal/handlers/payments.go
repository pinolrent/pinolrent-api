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
				return &statusError{http.StatusNotFound, "reservation not found"}
			}
			return err
		}
		if buyerID != u.ID {
			return &statusError{http.StatusNotFound, "reservation not found"}
		}
		if status != db.ReservationPending && status != db.ReservationAccepted {
			return &statusError{http.StatusConflict, "reservation is not pending"}
		}

		var payID int64
		var payStatus string
		payErr := conn.QueryRowContext(ctx,
			`SELECT id, status FROM payments WHERE reservation_id = ?`, id).Scan(&payID, &payStatus)
		if payErr != nil && !errors.Is(payErr, sql.ErrNoRows) {
			return payErr
		}
		if payErr == nil {
			if payStatus == db.PaymentRejected && status == db.ReservationAccepted {
				if _, err := conn.ExecContext(ctx,
					`UPDATE payments SET method = ?, proof_url = ?, status = ? WHERE id = ?`,
					in.Method, in.ProofURL, db.PaymentPending, payID); err != nil {
					return err
				}
				pid = payID
				payMethod = in.Method
				payProofURL = in.ProofURL
				corrected = true
				return nil
			}
			return &statusError{http.StatusConflict, "payment already recorded"}
		}

		res, err := conn.ExecContext(ctx,
			`INSERT INTO payments (reservation_id, method, proof_url) VALUES (?, ?, ?)`,
			id, in.Method, in.ProofURL)
		if err != nil {
			if isUniqueViolation(err) {
				return &statusError{http.StatusConflict, "payment already recorded"}
			}
			return err
		}
		pid, _ = res.LastInsertId()
		payMethod = in.Method
		payProofURL = in.ProofURL
		created = true
		return nil
	})
	if writeTxErr(w, err) {
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
		Status:        db.PaymentPending,
		ProofURL:      payProofURL,
	})
}
