package handlers

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/pinolrent/pinolrent-api/internal/db"
	"github.com/pinolrent/pinolrent-api/internal/models"
)

const adminReservationSelect = `
SELECT r.id, r.user_id, r.car_id, r.start_date, r.end_date, r.status,
       u.email, c.name
FROM reservations r
LEFT JOIN users u ON u.id = r.user_id
LEFT JOIN cars c ON c.id = r.car_id
`

// AdminListReservations returns all reservations on the platform.
func (a *API) AdminListReservations(w http.ResponseWriter, r *http.Request) {
	listPage(a, w, r,
		`SELECT COUNT(*) FROM reservations r`,
		adminReservationSelect,
		`ORDER BY r.id ASC`,
		func(f *filter) string {
			if s := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status"))); s != "" {
				if !db.ValidReservationStatus(s) {
					return "invalid status"
				}
				f.add("r.status = ?", s)
			}
			if uid, present, errMsg := queryID(r, "user_id"); errMsg != "" {
				return errMsg
			} else if present {
				f.add("r.user_id = ?", uid)
			}
			if cid, present, errMsg := queryID(r, "car_id"); errMsg != "" {
				return errMsg
			} else if present {
				f.add("r.car_id = ?", cid)
			}
			return ""
		},
		func(row rowScanner) (adminReservationOut, error) {
			var res adminReservationOut
			var buyerEmail, carName sql.NullString
			if err := row.Scan(&res.ID, &res.UserID, &res.CarID, &res.StartDate, &res.EndDate, &res.Status, &buyerEmail, &carName); err != nil {
				return res, err
			}
			if buyerEmail.Valid {
				res.BuyerEmail = buyerEmail.String
			}
			if carName.Valid {
				res.CarName = carName.String
			}
			return res, nil
		})
}

type adminReservationOut struct {
	models.Reservation
	BuyerEmail string `json:"buyer_email,omitempty"`
	CarName    string `json:"car_name,omitempty"`
}
