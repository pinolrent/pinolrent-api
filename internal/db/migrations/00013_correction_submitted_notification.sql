-- +goose Up
-- +goose NO TRANSACTION
-- A payment resubmitted after the administrator sent it back notifies the
-- administrators: the buyer answered their request and the proof needs
-- another look, instead of waiting for someone to poll the payment list.
-- This widens the notification kind CHECK with
-- 'reservation.correction_submitted'; no data changes, only the CHECK widens.
PRAGMA foreign_keys=OFF;
BEGIN TRANSACTION;
CREATE TABLE _notifications_new (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id),
	kind TEXT NOT NULL CHECK (kind IN ('reservation.requested','reservation.accepted','reservation.rejected','reservation.confirmed','reservation.correction_requested','reservation.cancelled','reservation.correction_submitted')),
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
DELETE FROM notifications WHERE kind = 'reservation.correction_submitted';
CREATE TABLE _notifications_old (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id),
	kind TEXT NOT NULL CHECK (kind IN ('reservation.requested','reservation.accepted','reservation.rejected','reservation.confirmed','reservation.correction_requested','reservation.cancelled')),
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
