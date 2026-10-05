package handlers

import (
	"net/http"
	"strings"

	"github.com/pinolrent/pinolrent-api/internal/models"
)

// AdminListNotifications returns every notification on the platform, newest
// first, so administrators can audit what each account was told.
func (a *API) AdminListNotifications(w http.ResponseWriter, r *http.Request) {
	listPage(a, w, r,
		`SELECT COUNT(*) FROM notifications`,
		`SELECT id, user_id, kind, reservation_id, read_at, created_at FROM notifications`,
		`ORDER BY id DESC`,
		func(f *filter) string {
			if uid, present, errMsg := queryID(r, "user_id"); errMsg != "" {
				return errMsg
			} else if present {
				f.add("user_id = ?", uid)
			}
			if s := strings.TrimSpace(r.URL.Query().Get("kind")); s != "" {
				if !validNotifyKind(s) {
					return "invalid kind"
				}
				f.add("kind = ?", s)
			}
			return f.unread(r.URL.Query().Get("unread"))
		},
		func(row rowScanner) (models.Notification, error) {
			var n models.Notification
			err := scanNotification(row, &n)
			return n, err
		})
}
