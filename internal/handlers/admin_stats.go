package handlers

import (
	"database/sql"
	"net/http"
)

// AdminStats returns high-level platform metrics for administrators.
func (a *API) AdminStats(w http.ResponseWriter, r *http.Request) {
	var totalUsers, totalSellers, totalAdmins, suspended int64
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users`).Scan(&totalUsers); err != nil {
		serverError(w, err)
		return
	}
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(DISTINCT user_id) FROM user_roles WHERE role='seller'`).Scan(&totalSellers); err != nil {
		serverError(w, err)
		return
	}
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(DISTINCT user_id) FROM user_roles WHERE role='admin'`).Scan(&totalAdmins); err != nil {
		serverError(w, err)
		return
	}
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE suspended_at IS NOT NULL`).Scan(&suspended); err != nil {
		serverError(w, err)
		return
	}

	var activeCars int64
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM cars WHERE active=1`).Scan(&activeCars); err != nil {
		serverError(w, err)
		return
	}

	var pendingRes, confirmedRes, cancelledRes int64
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM reservations WHERE status='pending'`).Scan(&pendingRes); err != nil {
		serverError(w, err)
		return
	}
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM reservations WHERE status='confirmed'`).Scan(&confirmedRes); err != nil {
		serverError(w, err)
		return
	}
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM reservations WHERE status='cancelled'`).Scan(&cancelledRes); err != nil {
		serverError(w, err)
		return
	}

	var pendingPay, approvedPay, rejectedPay int64
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM payments WHERE status='pending'`).Scan(&pendingPay); err != nil {
		serverError(w, err)
		return
	}
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM payments WHERE status='approved'`).Scan(&approvedPay); err != nil {
		serverError(w, err)
		return
	}
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM payments WHERE status='rejected'`).Scan(&rejectedPay); err != nil {
		serverError(w, err)
		return
	}
	var approvedAmount sql.NullInt64
	if err := a.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(SUM(c.price_per_day * (julianday(r.end_date) - julianday(r.start_date))), 0)
		 FROM payments p
		 JOIN reservations r ON r.id = p.reservation_id
		 JOIN cars c ON c.id = r.car_id
		 WHERE p.status='approved'`).Scan(&approvedAmount); err != nil {
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
			"confirmed": confirmedRes,
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
