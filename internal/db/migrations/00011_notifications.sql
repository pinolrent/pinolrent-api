-- +goose Up
-- In-app notifications: every reservation transition fans out a row per
-- interested user (owner, buyer, administrators), read back with
-- GET /notifications and GET /admin/notifications. There is no external
-- delivery; the frontend polls. read_at NULL means unread.
CREATE TABLE notifications (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id),
	kind TEXT NOT NULL CHECK (kind IN ('reservation.requested','reservation.accepted','reservation.rejected','reservation.confirmed','reservation.correction_requested')),
	reservation_id INTEGER NOT NULL REFERENCES reservations(id),
	read_at INTEGER,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_notifications_user ON notifications (user_id, id);

-- +goose Down
DROP INDEX IF EXISTS idx_notifications_user;
DROP TABLE IF EXISTS notifications;
