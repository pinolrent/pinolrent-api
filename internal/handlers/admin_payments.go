package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/pinolrent/pinolrent-api/internal/db"
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
	if s := r.URL.Query().Get("reservation_id"); s != "" {
		rid, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid reservation_id")
			return
		}
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

	type outPay struct {
		ID            int64  `json:"id"`
		ReservationID int64  `json:"reservation_id"`
		Method        string `json:"method"`
		Status        string `json:"status"`
		ProofURL      string `json:"proof_url,omitempty"`
		UserID        int64  `json:"user_id,omitempty"`
		CarID         int64  `json:"car_id,omitempty"`
		BuyerEmail    string `json:"buyer_email,omitempty"`
	}
	out := make([]outPay, 0, limit)
	for rows.Next() {
		var id, rid, uid, cid int64
		var method, status, proof string
		var buyerEmail sql.NullString
		if err := rows.Scan(&id, &rid, &method, &status, &proof, &uid, &cid, &buyerEmail); err != nil {
			serverError(w, err)
			return
		}
		po := outPay{ID: id, ReservationID: rid, Method: method, Status: status, ProofURL: proof, UserID: uid, CarID: cid}
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
