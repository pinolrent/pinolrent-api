-- +goose Up
-- +goose NO TRANSACTION
-- Two-step confirmation needs two more reservation states: 'accepted' (the
-- owner accepted, the administrator still has to validate the payment) and
-- 'rejected' (the owner or the administrator turned the request down, kept
-- apart from buyer-initiated 'cancelled'). No data changes: existing rows keep
-- their status, only the CHECK widens.
PRAGMA foreign_keys=OFF;
BEGIN TRANSACTION;
CREATE TABLE _reservations_new (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id),
	car_id INTEGER NOT NULL REFERENCES cars(id),
	start_date TEXT NOT NULL,
	end_date TEXT NOT NULL CHECK (end_date >= start_date),
	status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','confirmed','rejected','cancelled')),
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO _reservations_new(id, user_id, car_id, start_date, end_date, status, created_at)
	SELECT id, user_id, car_id, start_date, end_date, status, created_at FROM reservations;
DROP TABLE reservations;
ALTER TABLE _reservations_new RENAME TO reservations;
CREATE INDEX IF NOT EXISTS idx_reservations_status ON reservations (status, id);
CREATE INDEX IF NOT EXISTS idx_reservations_user ON reservations (user_id, id);
CREATE INDEX IF NOT EXISTS idx_reservations_car_dates ON reservations (car_id, start_date, end_date);
COMMIT;
PRAGMA foreign_keys=ON;

-- +goose Down
-- +goose NO TRANSACTION
PRAGMA foreign_keys=OFF;
BEGIN TRANSACTION;
UPDATE reservations SET status = 'pending' WHERE status = 'accepted';
UPDATE reservations SET status = 'cancelled' WHERE status = 'rejected';
CREATE TABLE _reservations_old (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id),
	car_id INTEGER NOT NULL REFERENCES cars(id),
	start_date TEXT NOT NULL,
	end_date TEXT NOT NULL CHECK (end_date >= start_date),
	status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','confirmed','cancelled')),
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO _reservations_old(id, user_id, car_id, start_date, end_date, status, created_at)
	SELECT id, user_id, car_id, start_date, end_date, status, created_at FROM reservations;
DROP TABLE reservations;
ALTER TABLE _reservations_old RENAME TO reservations;
CREATE INDEX IF NOT EXISTS idx_reservations_status ON reservations (status, id);
CREATE INDEX IF NOT EXISTS idx_reservations_user ON reservations (user_id, id);
CREATE INDEX IF NOT EXISTS idx_reservations_car_dates ON reservations (car_id, start_date, end_date);
COMMIT;
PRAGMA foreign_keys=ON;
