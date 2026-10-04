-- +goose Up
-- +goose NO TRANSACTION
-- Cancellations notify the owner: the buyer giving up pending dates frees the
-- car, and the owner held those dates. This widens the notification kind
-- CHECK with 'reservation.cancelled'; no data changes, only the CHECK widens.
PRAGMA foreign_keys=OFF;
BEGIN TRANSACTION;
CREATE TABLE _notifications_new (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id),
	kind TEXT NOT NULL CHECK (kind IN ('reservation.requested','reservation.accepted','reservation.rejected','reservation.confirmed','reservation.correction_requested','reservation.cancelled')),
	reservation_id INTEGER NOT NULL REFERENCES reservations(id),
	read_at INTEGER,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO _notifications_new(id, user_id, kind, reservation_id, read_at, created_at)
	SELECT id, user_id, kind, reservation_id, read_at, created_at FROM notifications;
DROP TABLE notifications;
ALTER TABLE _notifications_new RENAME TO notifications;
CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications (user_id, id);
COMMIT;
PRAGMA foreign_keys=ON;

-- +goose Down
-- +goose NO TRANSACTION
PRAGMA foreign_keys=OFF;
BEGIN TRANSACTION;
DELETE FROM notifications WHERE kind = 'reservation.cancelled';
CREATE TABLE _notifications_old (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id),
	kind TEXT NOT NULL CHECK (kind IN ('reservation.requested','reservation.accepted','reservation.rejected','reservation.confirmed','reservation.correction_requested')),
	reservation_id INTEGER NOT NULL REFERENCES reservations(id),
	read_at INTEGER,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO _notifications_old(id, user_id, kind, reservation_id, read_at, created_at)
	SELECT id, user_id, kind, reservation_id, read_at, created_at FROM notifications;
DROP TABLE notifications;
ALTER TABLE _notifications_old RENAME TO notifications;
CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications (user_id, id);
COMMIT;
PRAGMA foreign_keys=ON;
