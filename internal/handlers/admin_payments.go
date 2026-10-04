package handlers

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/pinolrent/pinolrent-api/internal/db"
	"github.com/pinolrent/pinolrent-api/internal/models"
)

// AdminListPayments returns all payments in the platform.
func (a *API) AdminListPayments(w http.ResponseWriter, r *http.Request) {
	limit, offset, errMsg := paginate(r)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	var f filter
	if s := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status"))); s != "" {
		if !db.ValidPaymentStatus(s) {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		f.add("p.status = ?", s)
	}
	if rid, present, errMsg := queryID(r, "reservation_id"); errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	} else if present {
		f.add("p.reservation_id = ?", rid)
	}

	var total int64
	if err := a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM payments p WHERE `+f.where(), f.params()...).Scan(&total); err != nil {
		serverError(w, err)
		return
	}

	// #nosec G202 -- clauses are built here from fixed fragments with placeholders;
	// every value from the query string is bound as a parameter.
	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT p.id, p.reservation_id, p.method, p.status, p.proof_url, r.user_id, r.car_id, u.email
		 FROM payments p
		 LEFT JOIN reservations r ON r.id = p.reservation_id
		 LEFT JOIN users u ON u.id = r.user_id
		 WHERE `+f.where()+`
		 ORDER BY p.id ASC
		 LIMIT ? OFFSET ?`, f.page(limit, offset)...)
	if err != nil {
		serverError(w, err)
		return
	}
	defer func() { _ = rows.Close() }()

	// outPay is the payment with its reservation context: the embedded
	// models.Payment keeps the payment shape in one place, the extras come
	// from the joins.
	type outPay struct {
		models.Payment
		UserID     int64  `json:"user_id,omitempty"`
		CarID      int64  `json:"car_id,omitempty"`
		BuyerEmail string `json:"buyer_email,omitempty"`
	}
	out := make([]outPay, 0, limit)
	for rows.Next() {
		var po outPay
		var buyerEmail sql.NullString
		if err := rows.Scan(&po.ID, &po.ReservationID, &po.Method, &po.Status, &po.ProofURL, &po.UserID, &po.CarID, &buyerEmail); err != nil {
			serverError(w, err)
			return
		}
		if buyerEmail.Valid {
			po.BuyerEmail = buyerEmail.String
		}
		out = append(out, po)
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
