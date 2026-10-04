package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/db"
)

// AdminListCars lists all cars in the system, including inactive ones. This is
// an administrative view: the ownership constraints that limit ListMyCars do
// not apply.
func (a *API) AdminListCars(w http.ResponseWriter, r *http.Request) {
	limit, offset, errMsg := paginate(r)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	clauses := []string{"1=1"}
	args := []any{}
	if s := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); s != "" {
		clauses = append(clauses, "(lower(c.name) LIKE ? OR CAST(c.id AS TEXT)=?)")
		args = append(args, "%"+s+"%", s)
	}
	if s := r.URL.Query().Get("owner_id"); s != "" {
		oid, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid owner_id")
			return
		}
		clauses = append(clauses, "c.owner_id = ?")
		args = append(args, oid)
	}
	if s := r.URL.Query().Get("active"); s != "" {
		v := strings.ToLower(s)
		if v != "true" && v != "false" {
			writeError(w, http.StatusBadRequest, "invalid active")
			return
		}
		active := 0
		if v == "true" {
			active = 1
		}
		clauses = append(clauses, "c.active = ?")
		args = append(args, active)
	}

	var total int64
	// #nosec G202 -- clauses are built here from fixed fragments with placeholders;
	// every value from the query string is bound as a parameter.
	if err := a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM cars c WHERE `+strings.Join(clauses, " AND "), args...).Scan(&total); err != nil {
		serverError(w, err)
		return
	}

	// #nosec G202 -- same as the count above.
	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT `+carColumnsQualified+`, u.email
		 FROM cars c
		 LEFT JOIN users u ON u.id = c.owner_id
		 WHERE `+strings.Join(clauses, " AND ")+`
		 ORDER BY c.id ASC
		 LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		serverError(w, err)
		return
	}
	defer func() { _ = rows.Close() }()

	type outCar struct {
		ID          int64  `json:"id"`
		OwnerID     int64  `json:"owner_id"`
		Name        string `json:"name"`
		PhotoURL    string `json:"photo_url,omitempty"`
		PricePerDay int64  `json:"price_per_day"`
		Active      bool   `json:"active"`
		OwnerEmail  string `json:"owner_email,omitempty"`
	}
	out := make([]outCar, 0, limit)
	for rows.Next() {
		var id int64
		var ownerID int64
		var name string
		var photoURL string
		var price int64
		var active int
		var ownerEmail sql.NullString
		if err := rows.Scan(&id, &ownerID, &name, &photoURL, &price, &active, &ownerEmail); err != nil {
			serverError(w, err)
			return
		}
		oc := outCar{ID: id, OwnerID: ownerID, Name: name, PhotoURL: photoURL, PricePerDay: price, Active: active == 1}
		if ownerEmail.Valid {
			oc.OwnerEmail = ownerEmail.String
		}
		out = append(out, oc)
	}
	if err := rows.Err(); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  out,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// AdminPatchCar updates any car in the system, without the ownership check
// and without blocking deactivation when there are future reservations. The
// administrative override is deliberate: an administrator must be able to
// unpublish a problematic car regardless of its booking history.
func (a *API) AdminPatchCar(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.CurrentUser(r.Context())
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
	err = db.WithImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()
		var curName, curPhoto string
		var curPrice int64
		var curActive int
		if err := conn.QueryRowContext(ctx,
			`SELECT name, photo_url, price_per_day, active FROM cars WHERE id = ?`, id).Scan(&curName, &curPhoto, &curPrice, &curActive); err != nil {
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
			if *in.Active {
				curActive = 1
			} else {
				curActive = 0
			}
		}
		if _, err := conn.ExecContext(ctx,
			`UPDATE cars SET name=?, photo_url=?, price_per_day=?, active=? WHERE id=?`,
			curName, curPhoto, curPrice, curActive, id); err != nil {
			return err
		}
		if err := a.auditAction(ctx, conn, actor.ID, auditActionCarUpdate, targetCars, id, ""); err != nil {
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
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// AdminDeleteCar removes any car in the system, but only if it never had
// reservations (same guard as the seller path). The administrator can delete a
// listing without being its owner.
func (a *API) AdminDeleteCar(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.CurrentUser(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid car id")
		return
	}

	deleted := false
	err = db.WithImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()
		var exists int
		if err := conn.QueryRowContext(ctx, `SELECT 1 FROM cars WHERE id = ?`, id).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &statusError{http.StatusNotFound, "car not found"}
			}
			return err
		}
		var reservations int
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM reservations WHERE car_id = ?`, id).Scan(&reservations); err != nil {
			return err
		}
		if reservations > 0 {
			return &statusError{http.StatusConflict, "car has reservations, cannot delete"}
		}
		if _, err := conn.ExecContext(ctx, `DELETE FROM cars WHERE id = ?`, id); err != nil {
			return err
		}
		if err := a.auditAction(ctx, conn, actor.ID, auditActionCarDelete, targetCars, id, ""); err != nil {
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
