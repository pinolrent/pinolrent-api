package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/db"
)

// CreateReservation books a car for the authenticated client, rejecting past
// dates, inactive cars, and overlapping active reservations.
func (a *API) CreateReservation(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	var in struct {
		CarID     int64  `json:"car_id"`
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	}
	if err := decodeBody(w, r, &in); err != nil {
		writeBodyErr(w, err)
		return
	}

	start, err := parseDate(in.StartDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid start_date, expected YYYY-MM-DD")
		return
	}
	end, err := parseDate(in.EndDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid end_date, expected YYYY-MM-DD")
		return
	}
	if end.Before(start) {
		writeError(w, http.StatusBadRequest, "end_date must be on or after start_date")
		return
	}
	// Dates are inclusive, so a same-day rental is 1 day. A 30-day cap
	// means end-start must stay below 30*24h (end = start+30d is 31 days).
	if end.Sub(start) >= maxRentalDays*24*time.Hour {
		writeError(w, http.StatusBadRequest, "reservation cannot be longer than "+strconv.Itoa(maxRentalDays)+" days")
		return
	}
	// String comparison, not time comparison: dates are canonical YYYY-MM-DD
	// (parseDate round-trips them), which sorts chronologically, and mixing a
	// UTC-parsed midnight with a local-zone midnight would compare instants.
	if in.StartDate < a.todayStr() {
		writeError(w, http.StatusBadRequest, "start_date cannot be in the past")
		return
	}
	if in.CarID == 0 {
		writeError(w, http.StatusBadRequest, "car_id is required")
		return
	}

	var reservationID int64
	var ownerID int64
	err = db.WithImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()

		var active int
		if err := conn.QueryRowContext(ctx, `SELECT active, owner_id FROM cars WHERE id = ?`, in.CarID).Scan(&active, &ownerID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &statusError{http.StatusNotFound, "car not found"}
			}
			return err
		}
		if active != 1 {
			return &statusError{http.StatusConflict, "car is not active"}
		}

		var overlap int
		if err := conn.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM reservations r
			WHERE r.car_id = ? AND r.status NOT IN (?, ?)
				AND `+db.OverlapPredicate, in.CarID, db.ReservationCancelled, db.ReservationRejected, in.EndDate, in.StartDate).Scan(&overlap); err != nil {
			return err
		}
		if overlap > 0 {
			return &statusError{http.StatusConflict, "car already reserved for the requested dates"}
		}

		res, err := conn.ExecContext(ctx, `
			INSERT INTO reservations (user_id, car_id, start_date, end_date) VALUES (?, ?, ?, ?)`,
			u.ID, in.CarID, in.StartDate, in.EndDate,
		)
		if err != nil {
			return err
		}
		reservationID, _ = res.LastInsertId()
		admins, err := adminIDs(ctx, conn)
		if err != nil {
			return err
		}
		if err := notifyUsers(ctx, conn, append(admins, ownerID), notifyRequested, reservationID); err != nil {
			return err
		}
		return nil
	})
	if writeTxErr(w, err) {
		return
	}
	if reservationID == 0 {
		return
	}

	v, err := a.reservationView(r.Context(), reservationID)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// ListReservations returns the reservations of the authenticated client,
// newest first.
func (a *API) ListReservations(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	limit, offset, errMsg := paginate(r)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	rows, err := a.DB.QueryContext(r.Context(), reservationSelect+`
		WHERE r.user_id = ? ORDER BY r.id DESC LIMIT ? OFFSET ?`, u.ID, limit, offset)
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

// GetReservation returns one reservation; buyers can access their own and
// sellers can access reservations for cars they own.
func (a *API) GetReservation(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	id, errMsg := pathID(r, "reservation")
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	v, err := a.reservationView(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "reservation not found")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	if u.ID != v.UserID && u.ID != v.Car.OwnerID {
		writeError(w, http.StatusNotFound, "reservation not found")
		return
	}

	writeJSON(w, http.StatusOK, v)
}

// CancelReservation cancels the authenticated buyer's pending reservation, as
// long as no payment has been recorded for it. The owner is notified in the
// same transaction: the cancelled dates were blocking its calendar.
func (a *API) CancelReservation(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	id, errMsg := pathID(r, "reservation")
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	cancelled := false
	err := db.WithImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()

		var buyerID int64
		var ownerID int64
		var status string
		if err := conn.QueryRowContext(ctx, `
			SELECT r.user_id, c.owner_id, r.status FROM reservations r
			JOIN cars c ON c.id = r.car_id
			WHERE r.id = ?`, id).Scan(&buyerID, &ownerID, &status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &statusError{http.StatusNotFound, "reservation not found"}
			}
			return err
		}
		if buyerID != u.ID {
			return &statusError{http.StatusNotFound, "reservation not found"}
		}
		if status != db.ReservationPending {
			return &statusError{http.StatusConflict, "reservation is not pending"}
		}

		var count int
		if err := conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM payments WHERE reservation_id = ?`, id).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return &statusError{http.StatusConflict, "payment already recorded, cannot cancel"}
		}

		if _, err := conn.ExecContext(ctx,
			`UPDATE reservations SET status = ? WHERE id = ? AND user_id = ?`, db.ReservationCancelled, id, u.ID); err != nil {
			return err
		}
		if err := a.auditAction(ctx, conn, u.ID, auditActionReservationCancel, targetReservations, id, ""); err != nil {
			return err
		}
		if err := notifyUsers(ctx, conn, []int64{ownerID}, notifyCancelled, id); err != nil {
			return err
		}
		cancelled = true
		return nil
	})
	if writeTxErr(w, err) {
		return
	}
	if !cancelled {
		return
	}

	v, err := a.reservationView(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// ListSellerReservations returns the reservations for the authenticated
// seller's cars, newest first.
func (a *API) ListSellerReservations(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	limit, offset, errMsg := paginate(r)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	rows, err := a.DB.QueryContext(r.Context(), reservationSelect+`
		WHERE c.owner_id = ? ORDER BY r.id DESC LIMIT ? OFFSET ?`, u.ID, limit, offset)
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
