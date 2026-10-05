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
	// outPay is the payment with its reservation context: the embedded
	// models.Payment keeps the payment shape in one place, the extras come
	// from the joins.
	type outPay struct {
		models.Payment
		UserID     int64  `json:"user_id,omitempty"`
		CarID      int64  `json:"car_id,omitempty"`
		BuyerEmail string `json:"buyer_email,omitempty"`
	}
	listPage(a, w, r,
		`SELECT COUNT(*) FROM payments p`,
		`SELECT p.id, p.reservation_id, p.method, p.status, p.proof_url, r.user_id, r.car_id, u.email
		 FROM payments p
		 LEFT JOIN reservations r ON r.id = p.reservation_id
		 LEFT JOIN users u ON u.id = r.user_id`,
		`ORDER BY p.id ASC`,
		func(f *filter) string {
			if s := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status"))); s != "" {
				if !db.ValidPaymentStatus(s) {
					return "invalid status"
				}
				f.add("p.status = ?", s)
			}
			if rid, present, errMsg := queryID(r, "reservation_id"); errMsg != "" {
				return errMsg
			} else if present {
				f.add("p.reservation_id = ?", rid)
			}
			return ""
		},
		func(row rowScanner) (outPay, error) {
			var po outPay
			var buyerEmail sql.NullString
			if err := row.Scan(&po.ID, &po.ReservationID, &po.Method, &po.Status, &po.ProofURL, &po.UserID, &po.CarID, &buyerEmail); err != nil {
				return po, err
			}
			if buyerEmail.Valid {
				po.BuyerEmail = buyerEmail.String
			}
			return po, nil
		})
}
