package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/db"
	"github.com/pinolrent/pinolrent-api/internal/models"
)

// ListCars returns active cars, optionally filtered by owner and excluding
// those already reserved in the [start_date, end_date] range.
func (a *API) ListCars(w http.ResponseWriter, r *http.Request) {
	startStr := r.URL.Query().Get("start_date")
	endStr := r.URL.Query().Get("end_date")

	if (startStr == "") != (endStr == "") {
		writeError(w, http.StatusBadRequest, "start_date and end_date must be provided together")
		return
	}

	limit, offset, errMsg := paginate(r)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	var f filter
	f.add("c.active = 1")
	if oid, present, errMsg := queryID(r, "owner_id"); errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	} else if present {
		f.add("c.owner_id = ?", oid)
	}

	var cars []models.Car
	if startStr == "" {
		// #nosec G202 -- the WHERE body comes from fixed filter fragments,
		// not user input; owner_id is bound as a parameter.
		rows, err := a.DB.QueryContext(r.Context(),
			`SELECT `+carColumnsQualified+` FROM cars c WHERE `+f.where()+` ORDER BY c.id LIMIT ? OFFSET ?`,
			f.page(limit, offset)...)
		if err != nil {
			serverError(w, err)
			return
		}
		cars, err = scanCars(rows)
		if err != nil {
			serverError(w, err)
			return
		}
	} else {
		start, err := parseDate(startStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid start_date, expected YYYY-MM-DD")
			return
		}
		end, err := parseDate(endStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid end_date, expected YYYY-MM-DD")
			return
		}
		if end.Before(start) {
			writeError(w, http.StatusBadRequest, "end_date must be on or after start_date")
			return
		}

		f.add(`NOT EXISTS (
				SELECT 1 FROM reservations r
				WHERE r.car_id = c.id
					AND r.status NOT IN (?, ?)
					AND `+db.OverlapPredicate+`
			)`, db.ReservationCancelled, db.ReservationRejected, endStr, startStr)

		// #nosec G202 -- the WHERE body comes from fixed filter fragments
		// (including db.OverlapPredicate), not user input; the values are
		// bound params.
		rows, err := a.DB.QueryContext(r.Context(), `
			SELECT `+carColumnsQualified+`
			FROM cars c
			WHERE `+f.where()+`
			ORDER BY c.id LIMIT ? OFFSET ?`, f.page(limit, offset)...)
		if err != nil {
			serverError(w, err)
			return
		}
		cars, err = scanCars(rows)
		if err != nil {
			serverError(w, err)
			return
		}
	}

	if cars == nil {
		cars = []models.Car{}
	}
	writeJSON(w, http.StatusOK, cars)
}

// GetCar returns a single active car from the catalog by id. Inactive cars are
// hidden from the public catalog, so they 404 here too.
func (a *API) GetCar(w http.ResponseWriter, r *http.Request) {
	id, errMsg := pathID(r, "car")
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	var car models.Car
	if err := scanCar(a.DB.QueryRowContext(r.Context(),
		`SELECT `+carColumns+` FROM cars WHERE id = ? AND active = 1`, id), &car); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "car not found")
			return
		}
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, car)
}

// GetCarContact returns the WhatsApp link of the seller who owns an active car.
// This is the only place a phone number is exposed, and it requires auth on
// purpose: the public catalog must not be harvestable for phone numbers.
func (a *API) GetCarContact(w http.ResponseWriter, r *http.Request) {
	id, errMsg := pathID(r, "car")
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	var name, phone string
	err := a.DB.QueryRowContext(r.Context(),
		`SELECT c.name, u.phone FROM cars c JOIN users u ON u.id = c.owner_id
		 WHERE c.id = ? AND c.active = 1`, id).Scan(&name, &phone)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "car not found")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	// Accounts created before phones were mandatory, or a buyer-owned car,
	// have no number to contact.
	if phone == "" {
		writeError(w, http.StatusConflict, "seller has no contact phone")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"whatsapp_url": waLink(phone, "Hi, I saw your "+name+" on PinolRent"),
	})
}

// CreateCar adds a new car to the catalog, owned by the authenticated seller.
func (a *API) CreateCar(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	var in struct {
		Name        string `json:"name"`
		PhotoURL    string `json:"photo_url"`
		PricePerDay int64  `json:"price_per_day"`
	}
	if err := decodeBody(w, r, &in); err != nil {
		writeBodyErr(w, err)
		return
	}

	var msg string
	if in.Name, msg = normalizeCarName(in.Name); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if msg = validateCarPrice(in.PricePerDay); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if msg = validateCarPhotoURL(in.PhotoURL); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	res, err := a.DB.ExecContext(r.Context(),
		`INSERT INTO cars (owner_id, name, photo_url, price_per_day) VALUES (?, ?, ?, ?)`,
		u.ID, in.Name, in.PhotoURL, in.PricePerDay)
	if err != nil {
		serverError(w, err)
		return
	}
	id, _ := res.LastInsertId()

	writeJSON(w, http.StatusCreated, models.Car{
		ID:          id,
		OwnerID:     u.ID,
		Name:        in.Name,
		PhotoURL:    in.PhotoURL,
		PricePerDay: in.PricePerDay,
		Active:      true,
	})
}

// ListMyCars returns the cars owned by the authenticated seller, newest first.
func (a *API) ListMyCars(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	limit, offset, errMsg := paginate(r)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT `+carColumns+` FROM cars WHERE owner_id = ? ORDER BY id DESC LIMIT ? OFFSET ?`,
		u.ID, limit, offset)
	if err != nil {
		serverError(w, err)
		return
	}
	cars, err := scanCars(rows)
	if err != nil {
		serverError(w, err)
		return
	}
	if cars == nil {
		cars = []models.Car{}
	}
	writeJSON(w, http.StatusOK, cars)
}

// carPatch is the mutable part of a car. The seller and admin endpoints share
// it, so both validate and apply the same fields in the same way.
type carPatch struct {
	Name        *string `json:"name"`
	PhotoURL    *string `json:"photo_url"`
	PricePerDay *int64  `json:"price_per_day"`
	Active      *bool   `json:"active"`
}

// decodeCarPatch decodes and validates a car patch, writing the error response
// itself and reporting false when the body is unusable. The returned patch has
// the name already normalized.
func decodeCarPatch(w http.ResponseWriter, r *http.Request) (carPatch, bool) {
	var in carPatch
	if err := decodeBody(w, r, &in); err != nil {
		writeBodyErr(w, err)
		return in, false
	}
	if in.Name == nil && in.PhotoURL == nil && in.PricePerDay == nil && in.Active == nil {
		writeError(w, http.StatusBadRequest, "no fields to update")
		return in, false
	}
	if in.Name != nil {
		name, msg := normalizeCarName(*in.Name)
		if msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return in, false
		}
		*in.Name = name
	}
	if in.PricePerDay != nil {
		if msg := validateCarPrice(*in.PricePerDay); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return in, false
		}
	}
	if in.PhotoURL != nil {
		if msg := validateCarPhotoURL(*in.PhotoURL); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return in, false
		}
	}
	return in, true
}

// applyCarPatch merges a validated patch into the car inside the caller's
// transaction. A non-nil ownerID restricts the lookup to that seller; nil is
// the administrator view, which edits any car. guardDeactivation blocks the
// transition to inactive while reservations end today or later, a guard the
// admin path deliberately skips. A missing row, or another owner's row, is the
// same 404 statusError the endpoints report.
func (a *API) applyCarPatch(ctx context.Context, conn *sql.Conn, id int64, in carPatch, ownerID *int64, guardDeactivation bool) error {
	query := `SELECT name, photo_url, price_per_day, active FROM cars WHERE id = ?`
	args := []any{id}
	if ownerID != nil {
		query += ` AND owner_id = ?`
		args = append(args, *ownerID)
	}

	var curName, curPhoto string
	var curPrice int64
	var curActive int
	if err := conn.QueryRowContext(ctx, query, args...).Scan(&curName, &curPhoto, &curPrice, &curActive); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &statusError{http.StatusNotFound, "car not found"}
		}
		return err
	}

	if in.Name != nil {
		curName = *in.Name
	}
	if in.PhotoURL != nil {
		curPhoto = *in.PhotoURL
	}
	if in.PricePerDay != nil {
		curPrice = *in.PricePerDay
	}
	if in.Active != nil {
		curActive = 0
		if *in.Active {
			curActive = 1
		}
	}

	// The guard only gates the transition to inactive; editing content or
	// reactivating must not be blocked by existing bookings. The end bound is
	// today in the business time zone (todayStr), not SQLite's date('now'): a
	// reservation ending on the user's today must block the transition there,
	// not on UTC's date.
	if guardDeactivation && in.Active != nil && !*in.Active {
		var hasFuture int
		if err := conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM reservations WHERE car_id = ? AND status NOT IN (?, ?) AND end_date >= ?`,
			id, db.ReservationCancelled, db.ReservationRejected, a.todayStr()).Scan(&hasFuture); err != nil {
			return err
		}
		if hasFuture > 0 {
			return &statusError{http.StatusConflict, "car has future reservations, cannot deactivate"}
		}
	}

	update := `UPDATE cars SET name = ?, photo_url = ?, price_per_day = ?, active = ? WHERE id = ?`
	updateArgs := []any{curName, curPhoto, curPrice, curActive, id}
	if ownerID != nil {
		update += ` AND owner_id = ?`
		updateArgs = append(updateArgs, *ownerID)
	}
	res, err := conn.ExecContext(ctx, update, updateArgs...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &statusError{http.StatusNotFound, "car not found"}
	}
	return nil
}

// PatchCar updates the editable fields of a car owned by the authenticated
// seller: name, photo, price and the active flag, in any combination. A new
// price only applies to future reservations (the reservation row does not
// store a price). Deactivating keeps the existing guard: a car with future
// reservations cannot go inactive.
func (a *API) PatchCar(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	id, errMsg := pathID(r, "car")
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	in, ok := decodeCarPatch(w, r)
	if !ok {
		return
	}

	updated := false
	err := db.WithImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		if err := a.applyCarPatch(r.Context(), conn, id, in, &u.ID, true); err != nil {
			return err
		}
		updated = true
		return nil
	})
	if writeTxErr(w, err) {
		return
	}
	if !updated {
		return
	}

	var car models.Car
	if err := scanCar(a.DB.QueryRowContext(r.Context(),
		`SELECT `+carColumns+` FROM cars WHERE id = ? AND owner_id = ?`, id, u.ID), &car); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, car)
}

// DeleteCar removes a car owned by the authenticated seller, but only when it
// never had reservations: every reservation row references the car (FK) and
// every reservation view joins it, so a car with history must be deactivated
// instead. Responds 409 to keep the raw FK violation from surfacing as a 500.
func (a *API) DeleteCar(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	id, errMsg := pathID(r, "car")
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	deleted := false
	err := db.WithImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()

		var ownerID int64
		if err := conn.QueryRowContext(ctx, `SELECT owner_id FROM cars WHERE id = ?`, id).Scan(&ownerID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &statusError{http.StatusNotFound, "car not found"}
			}
			return err
		}
		if ownerID != u.ID {
			return &statusError{http.StatusNotFound, "car not found"}
		}

		var reservations int
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM reservations WHERE car_id = ?`, id).Scan(&reservations); err != nil {
			return err
		}
		if reservations > 0 {
			return &statusError{http.StatusConflict, "car has reservations, cannot delete"}
		}

		if _, err := conn.ExecContext(ctx, `DELETE FROM cars WHERE id = ? AND owner_id = ?`, id, u.ID); err != nil {
			return err
		}
		deleted = true
		return nil
	})
	if writeTxErr(w, err) {
		return
	}
	if !deleted {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
