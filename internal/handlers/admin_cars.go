package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/db"
	"github.com/pinolrent/pinolrent-api/internal/models"
)

// AdminListCars lists all cars in the system, including inactive ones. This is
// an administrative view: the ownership constraints that limit ListMyCars do
// not apply.
func (a *API) AdminListCars(w http.ResponseWriter, r *http.Request) {
	// outCar is the car with its owner's email: the embedded models.Car keeps
	// the car shape in one place, the extra comes from the join.
	type outCar struct {
		models.Car
		OwnerEmail string `json:"owner_email,omitempty"`
	}
	listPage(a, w, r,
		`SELECT COUNT(*) FROM cars c`,
		`SELECT `+carColumnsQualified+`, u.email
		 FROM cars c
		 LEFT JOIN users u ON u.id = c.owner_id`,
		`ORDER BY c.id ASC`,
		func(f *filter) string {
			if s := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); s != "" {
				f.add("(lower(c.name) LIKE ? OR CAST(c.id AS TEXT)=?)", "%"+s+"%", s)
			}
			if oid, present, errMsg := queryID(r, "owner_id"); errMsg != "" {
				return errMsg
			} else if present {
				f.add("c.owner_id = ?", oid)
			}
			if s := r.URL.Query().Get("active"); s != "" {
				v := strings.ToLower(s)
				if v != "true" && v != "false" {
					return "invalid active"
				}
				active := 0
				if v == "true" {
					active = 1
				}
				f.add("c.active = ?", active)
			}
			return ""
		},
		func(row rowScanner) (outCar, error) {
			var oc outCar
			var active int
			var ownerEmail sql.NullString
			if err := row.Scan(&oc.ID, &oc.OwnerID, &oc.Name, &oc.PhotoURL, &oc.PricePerDay, &active, &ownerEmail); err != nil {
				return oc, err
			}
			oc.Active = active == 1
			if ownerEmail.Valid {
				oc.OwnerEmail = ownerEmail.String
			}
			return oc, nil
		})
}

// AdminPatchCar updates any car in the system, without the ownership check
// and without blocking deactivation when there are future reservations. The
// administrative override is deliberate: an administrator must be able to
// unpublish a problematic car regardless of its booking history.
func (a *API) AdminPatchCar(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.CurrentUser(r.Context())
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
		if err := a.applyCarPatch(r.Context(), conn, id, in, nil, false); err != nil {
			return err
		}
		if err := a.auditAction(r.Context(), conn, actor.ID, auditActionCarUpdate, targetCars, id, ""); err != nil {
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
	id, errMsg := pathID(r, "car")
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	deleted := false
	err := db.WithImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
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
