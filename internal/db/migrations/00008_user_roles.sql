-- +goose Up
-- +goose NO TRANSACTION
-- Role membership moves from a single users.role column to a user_roles
-- table: every account is a buyer and may also be a seller. Sellers keep
-- both roles, buyers keep just buyer.
PRAGMA foreign_keys=OFF;
CREATE TABLE user_roles (
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	role TEXT NOT NULL CHECK (role IN ('buyer','seller')),
	PRIMARY KEY (user_id, role)
);
INSERT INTO user_roles (user_id, role) SELECT id, 'buyer' FROM users;
INSERT OR IGNORE INTO user_roles (user_id, role) SELECT id, 'seller' FROM users WHERE role = 'seller';
BEGIN TRANSACTION;
CREATE TABLE _users_new (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	email TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	phone TEXT NOT NULL DEFAULT '' CHECK (phone = '' OR (substr(phone, 1, 1) = '+' AND length(phone) BETWEEN 9 AND 16)),
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	token_valid_after INTEGER
);
INSERT INTO _users_new(id, email, password_hash, phone, created_at, token_valid_after)
	SELECT id, email, password_hash, phone, created_at, token_valid_after FROM users;
DROP TABLE users;
ALTER TABLE _users_new RENAME TO users;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_lower ON users (lower(email));
COMMIT;
PRAGMA foreign_keys=ON;

-- +goose Down
-- +goose NO TRANSACTION
PRAGMA foreign_keys=OFF;
BEGIN TRANSACTION;
CREATE TABLE _users_old (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	email TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	role TEXT NOT NULL DEFAULT 'buyer' CHECK (role IN ('buyer','seller')),
	phone TEXT NOT NULL DEFAULT '' CHECK (phone = '' OR (substr(phone, 1, 1) = '+' AND length(phone) BETWEEN 9 AND 16)),
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	token_valid_after INTEGER
);
INSERT INTO _users_old(id, email, password_hash, role, phone, created_at, token_valid_after)
	SELECT u.id, u.email, u.password_hash,
		CASE WHEN EXISTS (SELECT 1 FROM user_roles r WHERE r.user_id = u.id AND r.role = 'seller') THEN 'seller' ELSE 'buyer' END,
		u.phone, u.created_at, u.token_valid_after FROM users u;
DROP TABLE users;
ALTER TABLE _users_old RENAME TO users;
DROP TABLE user_roles;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_lower ON users (lower(email));
COMMIT;
PRAGMA foreign_keys=ON;
