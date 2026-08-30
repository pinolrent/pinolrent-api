package handlers

import (
	"database/sql"
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
			`SELECT c.`+carColumns+` FROM cars c WHERE c.active = 1`+ownerCond+` ORDER BY c.id LIMIT ? OFFSET ?`,
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
			SELECT c.`+carColumns+`
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

	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if in.PricePerDay < 0 {
		writeError(w, http.StatusBadRequest, "price_per_day must be >= 0")
		return
	}
	if in.PricePerDay > maxPricePerDay {
		writeError(w, http.StatusBadRequest, "price_per_day must be <= "+strconv.FormatInt(maxPricePerDay, 10))
		return
	}
	if in.PhotoURL != "" && !validURL(in.PhotoURL) {
		writeError(w, http.StatusBadRequest, "invalid photo_url")
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

// PatchCar toggles the active flag of a car owned by the authenticated seller.
func (a *API) PatchCar(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid car id")
		return
	}

	var in struct {
		Active *bool `json:"active"`
	}
	if err := decodeBody(w, r, &in); err != nil {
		writeBodyErr(w, err)
		return
	}
	if in.Active == nil {
		writeError(w, http.StatusBadRequest, "active is required")
		return
	}

	res, err := a.DB.ExecContext(r.Context(), `UPDATE cars SET active = ? WHERE id = ? AND owner_id = ?`, *in.Active, id, u.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "car not found")
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

func parseDate(s string) (time.Time, error) {
	return time.Parse(dateLayout, s)
}

func todayStart() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func scanCar(row *sql.Row, c *models.Car) error {
	var active int
	err := row.Scan(&c.ID, &c.OwnerID, &c.Name, &c.PhotoURL, &c.PricePerDay, &active)
	c.Active = active == 1
	return err
}

func scanCars(rows *sql.Rows) ([]models.Car, error) {
	defer func() { _ = rows.Close() }()
	var cars []models.Car
	for rows.Next() {
		var c models.Car
		var active int
		if err := rows.Scan(&c.ID, &c.OwnerID, &c.Name, &c.PhotoURL, &c.PricePerDay, &active); err != nil {
			return nil, err
		}
		c.Active = active == 1
		cars = append(cars, c)
	}
	return cars, rows.Err()
}
