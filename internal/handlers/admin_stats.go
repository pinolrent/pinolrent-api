package handlers

import (
	"database/sql"
	"net/http"

	"github.com/pinolrent/pinolrent-api/internal/db"
)

// AdminStats returns high-level platform metrics for administrators.
func (a *API) AdminStats(w http.ResponseWriter, r *http.Request) {
	var totalUsers, totalSellers, totalAdmins, suspended int64
	var activeCars int64
	var pendingRes, acceptedRes, confirmedRes, rejectedRes, cancelledRes int64
	var pendingPay, approvedPay, rejectedPay int64
	var approvedAmount sql.NullInt64

	// One round trip: every metric is a scalar subquery, so the whole
	// dashboard reads in a single statement. Statuses and roles travel as
	// bound params from the db constants, in SELECT order.
	err := a.DB.QueryRowContext(r.Context(),
		`SELECT
			(SELECT COUNT(*) FROM users),
			(SELECT COUNT(DISTINCT user_id) FROM user_roles WHERE role = ?),
			(SELECT COUNT(DISTINCT user_id) FROM user_roles WHERE role = ?),
			(SELECT COUNT(*) FROM users WHERE suspended_at IS NOT NULL),
			(SELECT COUNT(*) FROM cars WHERE active = 1),
			(SELECT COUNT(*) FROM reservations WHERE status = ?),
			(SELECT COUNT(*) FROM reservations WHERE status = ?),
			(SELECT COUNT(*) FROM reservations WHERE status = ?),
			(SELECT COUNT(*) FROM reservations WHERE status = ?),
			(SELECT COUNT(*) FROM reservations WHERE status = ?),
			(SELECT COUNT(*) FROM payments WHERE status = ?),
			(SELECT COUNT(*) FROM payments WHERE status = ?),
			(SELECT COUNT(*) FROM payments WHERE status = ?),
			(SELECT COALESCE(SUM(c.price_per_day * (julianday(r.end_date) - julianday(r.start_date))), 0)
			 FROM payments p
			 JOIN reservations r ON r.id = p.reservation_id
			 JOIN cars c ON c.id = r.car_id
			 WHERE p.status = ?)`,
		db.RoleSeller, db.RoleAdmin,
		db.ReservationPending, db.ReservationAccepted, db.ReservationConfirmed,
		db.ReservationRejected, db.ReservationCancelled,
		db.PaymentPending, db.PaymentApproved, db.PaymentRejected,
		db.PaymentApproved).Scan(
		&totalUsers, &totalSellers, &totalAdmins, &suspended,
		&activeCars,
		&pendingRes, &acceptedRes, &confirmedRes, &rejectedRes, &cancelledRes,
		&pendingPay, &approvedPay, &rejectedPay,
		&approvedAmount)
	if err != nil {
		serverError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"users": map[string]int64{
			"total":     totalUsers,
			"sellers":   totalSellers,
			"admins":    totalAdmins,
			"suspended": suspended,
		},
		"cars": map[string]int64{
			"active": activeCars,
		},
		"reservations": map[string]int64{
			"pending":   pendingRes,
			"accepted":  acceptedRes,
			"confirmed": confirmedRes,
			"rejected":  rejectedRes,
			"cancelled": cancelledRes,
		},
		"payments": map[string]any{
			"pending":        pendingPay,
			"approved":       approvedPay,
			"rejected":       rejectedPay,
			"approved_total": approvedAmount.Int64,
		},
	})
}
