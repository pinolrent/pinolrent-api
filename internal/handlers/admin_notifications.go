package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/pinolrent/pinolrent-api/internal/models"
)

// AdminListNotifications returns every notification on the platform, newest
// first, so administrators can audit what each account was told.
func (a *API) AdminListNotifications(w http.ResponseWriter, r *http.Request) {
	limit, offset, errMsg := paginate(r)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	clauses := []string{"1=1"}
	args := []any{}
	if s := r.URL.Query().Get("user_id"); s != "" {
		uid, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid user_id")
			return
		}
		clauses = append(clauses, "user_id = ?")
		args = append(args, uid)
	}
	if s := strings.TrimSpace(r.URL.Query().Get("kind")); s != "" {
		if !validNotifyKind(s) {
			writeError(w, http.StatusBadRequest, "invalid kind")
			return
		}
		clauses = append(clauses, "kind = ?")
		args = append(args, s)
	}
	var ok bool
	if clauses, args, ok = unreadClause(clauses, args, r.URL.Query().Get("unread")); !ok {
		writeError(w, http.StatusBadRequest, "invalid unread")
		return
	}

	var total int64
	// #nosec G202 G701 -- clauses are built here from fixed fragments with placeholders;
	// every value from the query string is bound as a parameter.
	if err := a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM notifications WHERE `+strings.Join(clauses, " AND "), args...).Scan(&total); err != nil {
		serverError(w, err)
		return
	}

	// #nosec G202 G701 -- clauses are built here from fixed fragments with placeholders;
	// every value from the query string is bound as a parameter.
	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT id, user_id, kind, reservation_id, read_at, created_at FROM notifications
		 WHERE `+strings.Join(clauses, " AND ")+`
		 ORDER BY id DESC
		 LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		serverError(w, err)
		return
	}
	defer func() { _ = rows.Close() }()

	items := []models.Notification{}
	for rows.Next() {
		var n models.Notification
		if err := scanNotification(rows, &n); err != nil {
			serverError(w, err)
			return
		}
		items = append(items, n)
	}
	if err := rows.Err(); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}
