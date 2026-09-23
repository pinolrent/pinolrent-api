package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/db"
	"github.com/pinolrent/pinolrent-api/internal/models"
)

const dateLayout = "2006-01-02"

const maxPricePerDay = 100_000_000

const carColumns = "id, owner_id, name, photo_url, price_per_day, active"

const carColumnsQualified = "c.id, c.owner_id, c.name, c.photo_url, c.price_per_day, c.active"

// normalizeCarName trims the name and returns the validation error message
// ("" when valid), so create and patch enforce the same rules.
func normalizeCarName(s string) (string, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "name is required"
	}
	if !lenBetween(s, 1, maxNameLen) {
		return "", "name is too long (max " + strconv.Itoa(maxNameLen) + " characters)"
	}
	return s, ""
}

// validateCarPrice returns the error message for a price outside the allowed
// range in centavos, or "" when valid.
func validateCarPrice(p int64) string {
	if p < 0 {
		return "price_per_day must be >= 0"
	}
	if p > maxPricePerDay {
		return "price_per_day must be <= " + strconv.FormatInt(maxPricePerDay, 10)
	}
	return ""
}

// validateCarPhotoURL returns the error message for an invalid photo URL, or
// "" — which also covers the empty value, meaning "no photo".
func validateCarPhotoURL(u string) string {
	if u == "" {
		return ""
	}
	if len(u) > maxURLLen {
		return "photo_url is too long"
	}
	if !validURL(u) {
		return "invalid photo_url"
	}
	return ""
}

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

	ownerCond := ""
	var ownerID int64
	if s := r.URL.Query().Get("owner_id"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "invalid owner_id")
			return
		}
		ownerID = n
		ownerCond = " AND c.owner_id = ?"
	}

	var cars []models.Car
	if startStr == "" {
		args := []any{limit, offset}
		if ownerCond != "" {
			args = append([]any{ownerID}, args...)
		}
		// #nosec G202 -- ownerCond/carColumns are fixed internal fragments,
		// not user input; owner_id is bound as a parameter.
		rows, err := a.DB.QueryContext(r.Context(),
			`SELECT `+carColumnsQualified+` FROM cars c WHERE c.active = 1`+ownerCond+` ORDER BY c.id LIMIT ? OFFSET ?`,
			args...)
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

		args := make([]any, 0, 5)
		if ownerCond != "" {
			args = append(args, ownerID)
		}
		args = append(args, endStr, startStr, limit, offset)

		// #nosec G202 -- db.OverlapPredicate, ownerCond and carColumns are fixed
		// internal SQL fragments, not user input; the values are bound params.
		rows, err := a.DB.QueryContext(r.Context(), `
			SELECT `+carColumnsQualified+`
			FROM cars c
			WHERE c.active = 1`+ownerCond+`
			AND NOT EXISTS (
				SELECT 1 FROM reservations r
				WHERE r.car_id = c.id
					AND r.status != 'cancelled'
					AND `+db.OverlapPredicate+`
			)
			ORDER BY c.id LIMIT ? OFFSET ?`, args...)
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
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid car id")
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
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid car id")
		return
	}

	var name, phone string
	err = a.DB.QueryRowContext(r.Context(),
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
		"whatsapp_url": waLink(phone, "Hola, vi tu "+name+" en PinolRent"),
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

// PatchCar updates the editable fields of a car owned by the authenticated
// seller: name, photo, price and the active flag, in any combination. A new
// price only applies to future reservations (the reservation row does not
// store a price). Deactivating keeps the existing guard: a car with future
// reservations cannot go inactive.
func (a *API) PatchCar(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid car id")
		return
	}

	var in struct {
		Name        *string `json:"name"`
		PhotoURL    *string `json:"photo_url"`
		PricePerDay *int64  `json:"price_per_day"`
		Active      *bool   `json:"active"`
	}
	if err := decodeBody(w, r, &in); err != nil {
		writeBodyErr(w, err)
		return
	}
	if in.Name == nil && in.PhotoURL == nil && in.PricePerDay == nil && in.Active == nil {
		writeError(w, http.StatusBadRequest, "no fields to update")
		return
	}
	if in.Name != nil {
		name, msg := normalizeCarName(*in.Name)
		if msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		*in.Name = name
	}
	if in.PricePerDay != nil {
		if msg := validateCarPrice(*in.PricePerDay); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
	}
	if in.PhotoURL != nil {
		if msg := validateCarPhotoURL(*in.PhotoURL); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
	}

	var updated bool
	err = withImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()

		var curName, curPhoto string
		var curPrice int64
		var curActive int
		if err := conn.QueryRowContext(ctx,
			`SELECT name, photo_url, price_per_day, active FROM cars WHERE id = ? AND owner_id = ?`,
			id, u.ID).Scan(&curName, &curPhoto, &curPrice, &curActive); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "car not found")
				return errTxHandled
			}
			serverError(w, err)
			return errTxHandled
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
		// reactivating must not be blocked by existing bookings.
		if in.Active != nil && !*in.Active {
			var hasFuture int
			if err := conn.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM reservations WHERE car_id = ? AND status != 'cancelled' AND end_date >= date('now')`, id).Scan(&hasFuture); err != nil {
				serverError(w, err)
				return errTxHandled
			}
			if hasFuture > 0 {
				writeError(w, http.StatusConflict, "car has future reservations, cannot deactivate")
				return errTxHandled
			}
		}

		res, err := conn.ExecContext(ctx,
			`UPDATE cars SET name = ?, photo_url = ?, price_per_day = ?, active = ? WHERE id = ? AND owner_id = ?`,
			curName, curPhoto, curPrice, curActive, id, u.ID)
		if err != nil {
			serverError(w, err)
			return errTxHandled
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeError(w, http.StatusNotFound, "car not found")
			return errTxHandled
		}
		updated = true
		return nil
	})
	if err != nil {
		serverError(w, err)
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

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid car id")
		return
	}

	deleted := false
	err = withImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()

		var ownerID int64
		if err := conn.QueryRowContext(ctx, `SELECT owner_id FROM cars WHERE id = ?`, id).Scan(&ownerID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "car not found")
				return errTxHandled
			}
			serverError(w, err)
			return errTxHandled
		}
		if ownerID != u.ID {
			writeError(w, http.StatusNotFound, "car not found")
			return errTxHandled
		}

		var reservations int
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM reservations WHERE car_id = ?`, id).Scan(&reservations); err != nil {
			serverError(w, err)
			return errTxHandled
		}
		if reservations > 0 {
			writeError(w, http.StatusConflict, "car has reservations, cannot delete")
			return errTxHandled
		}

		if _, err := conn.ExecContext(ctx, `DELETE FROM cars WHERE id = ? AND owner_id = ?`, id, u.ID); err != nil {
			serverError(w, err)
			return errTxHandled
		}
		deleted = true
		return nil
	})
	if err != nil {
		serverError(w, err)
		return
	}
	if !deleted {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func parseDate(s string) (time.Time, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return time.Time{}, err
	}
	if t.Format(dateLayout) != s {
		return time.Time{}, errors.New("invalid date")
	}
	return t, nil
}

func todayStart() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

// scanCar reads one row of carColumns into c. It takes the rowScanner
// interface so the single-row and multi-row paths share the field order.
func scanCar(row rowScanner, c *models.Car) error {
	var active int
	if err := row.Scan(&c.ID, &c.OwnerID, &c.Name, &c.PhotoURL, &c.PricePerDay, &active); err != nil {
		return err
	}
	c.Active = active == 1
	return nil
}

func scanCars(rows *sql.Rows) ([]models.Car, error) {
	defer func() { _ = rows.Close() }()
	var cars []models.Car
	for rows.Next() {
		var c models.Car
		if err := scanCar(rows, &c); err != nil {
			return nil, err
		}
		cars = append(cars, c)
	}
	return cars, rows.Err()
}
