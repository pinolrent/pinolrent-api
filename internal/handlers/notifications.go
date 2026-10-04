package handlers

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/db"
	"github.com/pinolrent/pinolrent-api/internal/models"
)

// Notification kinds fan out on every reservation transition: the owner learns
// about new requests and about cancellations that free its dates, the buyer
// about decisions, and the administrators about the requests that need their
// review.
const (
	notifyRequested           = "reservation.requested"
	notifyAccepted            = "reservation.accepted"
	notifyRejected            = "reservation.rejected"
	notifyConfirmed           = "reservation.confirmed"
	notifyCorrectionRequested = "reservation.correction_requested"
	notifyCancelled           = "reservation.cancelled"
)

func validNotifyKind(s string) bool {
	switch s {
	case notifyRequested, notifyAccepted, notifyRejected, notifyConfirmed, notifyCorrectionRequested, notifyCancelled:
		return true
	}
	return false
}

// notifyUsers records one notification per user inside the caller's
// transaction, so the note and the transition it announces commit together.
func notifyUsers(ctx context.Context, conn *sql.Conn, userIDs []int64, kind string, reservationID int64) error {
	for _, uid := range userIDs {
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO notifications (user_id, kind, reservation_id) VALUES (?, ?, ?)`,
			uid, kind, reservationID); err != nil {
			return err
		}
	}
	return nil
}

// adminIDs returns every administrator account, the audience for the review
// steps of the reservation flow.
func adminIDs(ctx context.Context, conn *sql.Conn) ([]int64, error) {
	rows, err := conn.QueryContext(ctx, `SELECT user_id FROM user_roles WHERE role = ?`, db.RoleAdmin)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func scanNotification(row rowScanner, n *models.Notification) error {
	var readAt sql.NullInt64
	if err := row.Scan(&n.ID, &n.UserID, &n.Kind, &n.ReservationID, &readAt, &n.CreatedAt); err != nil {
		return err
	}
	n.Read = readAt.Valid
	if readAt.Valid {
		n.ReadAt = readAt.Int64
	}
	return nil
}

// ListNotifications returns the authenticated user's notifications, newest
// first, optionally filtered to unread only.
func (a *API) ListNotifications(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	limit, offset, errMsg := paginate(r)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	var f filter
	f.add("user_id = ?", u.ID)
	if errMsg := f.unread(r.URL.Query().Get("unread")); errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	// #nosec G202 G701 -- clauses are built here from fixed fragments with placeholders;
	// every value from the query string is bound as a parameter.
	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT id, user_id, kind, reservation_id, read_at, created_at FROM notifications
		 WHERE `+f.where()+`
		 ORDER BY id DESC LIMIT ? OFFSET ?`, f.page(limit, offset)...)
	if err != nil {
		serverError(w, err)
		return
	}
	defer func() { _ = rows.Close() }()

	out := []models.Notification{}
	for rows.Next() {
		var n models.Notification
		if err := scanNotification(rows, &n); err != nil {
			serverError(w, err)
			return
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		serverError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, out)
}

// MarkNotificationRead marks one of the authenticated user's notifications as
// read. It is idempotent: reading twice still answers 200.
func (a *API) MarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.CurrentUser(r.Context())

	id, errMsg := pathID(r, "notification")
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	read := false
	err := db.WithImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()

		res, err := conn.ExecContext(ctx,
			`UPDATE notifications SET read_at = COALESCE(read_at, strftime('%s','now')) WHERE id = ? AND user_id = ?`, id, u.ID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return &statusError{http.StatusNotFound, "notification not found"}
		}
		read = true
		return nil
	})
	if writeTxErr(w, err) {
		return
	}
	if !read {
		return
	}

	var n models.Notification
	if err := scanNotification(a.DB.QueryRowContext(r.Context(),
		`SELECT id, user_id, kind, reservation_id, read_at, created_at FROM notifications WHERE id = ?`, id), &n); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}
