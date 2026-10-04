package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
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
	limit, offset, errMsg := paginate(r)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	clauses := []string{"1=1"}
	args := []any{}
	if s := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status"))); s != "" {
		// The reservations CHECK admits pending, accepted, confirmed,
		// rejected and cancelled. A reservation is paid when its payment row
		// is approved, not through its own status.
		if s != "pending" && s != "accepted" && s != "confirmed" && s != "rejected" && s != "cancelled" {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		clauses = append(clauses, "r.status = ?")
		args = append(args, s)
	}
	if s := r.URL.Query().Get("user_id"); s != "" {
		uid, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid user_id")
			return
		}
		clauses = append(clauses, "r.user_id = ?")
		args = append(args, uid)
	}
	if s := r.URL.Query().Get("car_id"); s != "" {
		cid, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid car_id")
			return
		}
		clauses = append(clauses, "r.car_id = ?")
		args = append(args, cid)
	}

	var total int64
	if err := a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM reservations r WHERE `+strings.Join(clauses, " AND "), args...).Scan(&total); err != nil {
		serverError(w, err)
		return
	}

	rows, err := a.DB.QueryContext(r.Context(),
		adminReservationSelect+`
		 WHERE `+strings.Join(clauses, " AND ")+`
		 ORDER BY r.id ASC
		 LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		serverError(w, err)
		return
	}
	defer func() { _ = rows.Close() }()

	out := make([]adminReservationOut, 0, limit)
	for rows.Next() {
		var r adminReservationOut
		var buyerEmail, carName sql.NullString
		if err := rows.Scan(&r.ID, &r.UserID, &r.CarID, &r.StartDate, &r.EndDate, &r.Status, &buyerEmail, &carName); err != nil {
			serverError(w, err)
			return
		}
		if buyerEmail.Valid {
			r.BuyerEmail = buyerEmail.String
		}
		if carName.Valid {
			r.CarName = carName.String
		}
		out = append(out, r)
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

type adminReservationOut struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"user_id"`
	CarID      int64  `json:"car_id"`
	StartDate  string `json:"start_date"`
	EndDate    string `json:"end_date"`
	Status     string `json:"status"`
	BuyerEmail string `json:"buyer_email,omitempty"`
	CarName    string `json:"car_name,omitempty"`
}
