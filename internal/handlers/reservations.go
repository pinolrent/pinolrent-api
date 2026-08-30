package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/db"
	"github.com/pinolrent/pinolrent-api/internal/models"
)

type reservationView struct {
	models.Reservation
	Car     *models.Car     `json:"car"`
	Payment *models.Payment `json:"payment,omitempty"`
}

const reservationSelect = `
SELECT r.id, r.user_id, r.car_id, r.start_date, r.end_date, r.status,
	c.id, c.owner_id, c.name, c.photo_url, c.price_per_day, c.active,
	p.id, p.reservation_id, p.method, p.status, p.proof_url
FROM reservations r
JOIN cars c ON c.id = r.car_id
LEFT JOIN payments p ON p.reservation_id = r.id
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanReservation(row rowScanner, v *reservationView) error {
	var c models.Car
	var p models.Payment
	var active int
	var pID, pResID sql.NullInt64
	var pMethod, pStatus, pProof sql.NullString

	err := row.Scan(
		&v.ID, &v.UserID, &v.CarID, &v.StartDate, &v.EndDate, &v.Status,
		&c.ID, &c.OwnerID, &c.Name, &c.PhotoURL, &c.PricePerDay, &active,
		&pID, &pResID, &pMethod, &pStatus, &pProof,
	)
	if err != nil {
		return err
	}

	c.Active = active == 1
	v.Car = &c
	if pID.Valid {
		p.ID = pID.Int64
		p.ReservationID = pResID.Int64
		p.Method = pMethod.String
		p.Status = pStatus.String
		p.ProofURL = pProof.String
		v.Payment = &p
	}
	return nil
}

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
	if start.Before(todayStart()) {
		writeError(w, http.StatusBadRequest, "start_date cannot be in the past")
		return
	}
	if in.CarID == 0 {
		writeError(w, http.StatusBadRequest, "car_id is required")
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

	var active int
	err = conn.QueryRowContext(ctx, `SELECT active FROM cars WHERE id = ?`, in.CarID).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "car not found")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if active != 1 {
		writeError(w, http.StatusConflict, "car is not active")
		return
	}

	var overlap int
	err = conn.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM reservations r
		WHERE r.car_id = ? AND r.status != 'cancelled'
			AND `+db.OverlapPredicate, in.CarID, in.EndDate, in.StartDate).Scan(&overlap)
	if err != nil {
		serverError(w, err)
		return
	}
	if overlap > 0 {
		writeError(w, http.StatusConflict, "car already reserved for the requested dates")
		return
	}

	res, err := conn.ExecContext(ctx, `
		INSERT INTO reservations (user_id, car_id, start_date, end_date) VALUES (?, ?, ?, ?)`,
		u.ID, in.CarID, in.StartDate, in.EndDate,
	)
	if err != nil {
		serverError(w, err)
		return
	}
	id, _ := res.LastInsertId()

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
	writeJSON(w, http.StatusCreated, v)
}

// ListReservations returns the reservations of the authenticated client,
// newest first.
func (a *API) ListReservations(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	rows, err := a.DB.QueryContext(r.Context(), reservationSelect+`
		WHERE r.user_id = ? ORDER BY r.id DESC`, u.ID)
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

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid reservation id")
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

// ListSellerReservations returns the reservations for the authenticated
// seller's cars, newest first.
func (a *API) ListSellerReservations(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	rows, err := a.DB.QueryContext(r.Context(), reservationSelect+`
		WHERE c.owner_id = ? ORDER BY r.id DESC`, u.ID)
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

func (a *API) reservationView(ctx context.Context, id int64) (reservationView, error) {
	var v reservationView
	err := scanReservation(a.DB.QueryRowContext(ctx, reservationSelect+` WHERE r.id = ?`, id), &v)
	return v, err
}
