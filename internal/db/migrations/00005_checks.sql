-- +goose Up
CREATE TABLE _cars_new (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	owner_id INTEGER NOT NULL REFERENCES users(id),
	name TEXT NOT NULL,
	photo_url TEXT NOT NULL DEFAULT '',
	price_per_day INTEGER NOT NULL CHECK (price_per_day >= 0 AND price_per_day <= 100000000),
	active INTEGER NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO _cars_new(id, owner_id, name, photo_url, price_per_day, active, created_at)
	SELECT id, owner_id, name, photo_url, price_per_day, active, created_at FROM cars;
DROP TABLE cars;
ALTER TABLE _cars_new RENAME TO cars;

CREATE TABLE _reservations_new (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id),
	car_id INTEGER NOT NULL REFERENCES cars(id),
	start_date TEXT NOT NULL,
	end_date TEXT NOT NULL CHECK (end_date >= start_date),
	status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','confirmed','cancelled')),
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO _reservations_new(id, user_id, car_id, start_date, end_date, status, created_at)
	SELECT id, user_id, car_id, start_date, end_date, status, created_at FROM reservations;
DROP TABLE reservations;
ALTER TABLE _reservations_new RENAME TO reservations;

CREATE INDEX IF NOT EXISTS idx_cars_owner ON cars (owner_id, id);
CREATE INDEX IF NOT EXISTS idx_reservations_user ON reservations (user_id, id);
CREATE INDEX IF NOT EXISTS idx_reservations_car_dates ON reservations (car_id, start_date, end_date);

-- +goose Down
DROP INDEX IF EXISTS idx_cars_owner;
DROP INDEX IF EXISTS idx_reservations_user;
DROP INDEX IF EXISTS idx_reservations_car_dates;
CREATE TABLE _cars_old (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	owner_id INTEGER NOT NULL REFERENCES users(id),
	name TEXT NOT NULL,
	photo_url TEXT NOT NULL DEFAULT '',
	price_per_day INTEGER NOT NULL CHECK (price_per_day >= 0),
	active INTEGER NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO _cars_old(id, owner_id, name, photo_url, price_per_day, active, created_at)
	SELECT id, owner_id, name, photo_url, price_per_day, active, created_at FROM cars;
DROP TABLE cars;
ALTER TABLE _cars_old RENAME TO cars;

CREATE TABLE _reservations_old (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id),
	car_id INTEGER NOT NULL REFERENCES cars(id),
	start_date TEXT NOT NULL,
	end_date TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','confirmed','cancelled')),
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO _reservations_old(id, user_id, car_id, start_date, end_date, status, created_at)
	SELECT id, user_id, car_id, start_date, end_date, status, created_at FROM reservations;
DROP TABLE reservations;
ALTER TABLE _reservations_old RENAME TO reservations;
CREATE INDEX IF NOT EXISTS idx_cars_owner ON cars (owner_id, id);
CREATE INDEX IF NOT EXISTS idx_reservations_user ON reservations (user_id, id);
CREATE INDEX IF NOT EXISTS idx_reservations_car_dates ON reservations (car_id, start_date, end_date);
