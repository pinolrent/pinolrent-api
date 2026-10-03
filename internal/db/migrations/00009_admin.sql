-- +goose Up
-- +goose NO TRANSACTION
-- Platform administration: admin joins the role memberships and every admin
-- action leaves a record. SQLite cannot relax a CHECK in place, so
-- user_roles is rebuilt with the wider role set and its rows are copied over.
PRAGMA foreign_keys=OFF;
CREATE TABLE _user_roles_new (
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	role TEXT NOT NULL CHECK (role IN ('buyer','seller','admin')),
	PRIMARY KEY (user_id, role)
);
INSERT INTO _user_roles_new (user_id, role) SELECT user_id, role FROM user_roles;
DROP TABLE user_roles;
ALTER TABLE _user_roles_new RENAME TO user_roles;
PRAGMA foreign_keys=ON;

-- Suspension state. NULL is an account in good standing; an admin sets the
-- Unix instant to lock it out of every authenticated route.
ALTER TABLE users ADD COLUMN suspended_at INTEGER;

-- Trail of the admin actions. actor_id is deliberately not cascaded on
-- delete: the record of who did what outlives the account it happened to.
CREATE TABLE admin_audit_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	actor_id INTEGER NOT NULL REFERENCES users(id),
	action TEXT NOT NULL,
	target_type TEXT NOT NULL,
	target_id INTEGER NOT NULL,
	detail TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_admin_audit_created_at ON admin_audit_log (created_at);
CREATE INDEX idx_admin_audit_actor ON admin_audit_log (actor_id, created_at);

-- Admin listings filter and order by status/flag over the whole table, which
-- no existing index covers: they lead with the filter column and end with id
-- so the keyset order matches the result order.
CREATE INDEX idx_cars_active ON cars (active, id);
CREATE INDEX idx_reservations_status ON reservations (status, id);
CREATE INDEX idx_payments_status ON payments (status, id);

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX IF EXISTS idx_payments_status;
DROP INDEX IF EXISTS idx_reservations_status;
DROP INDEX IF EXISTS idx_cars_active;
DROP INDEX IF EXISTS idx_admin_audit_actor;
DROP INDEX IF EXISTS idx_admin_audit_created_at;
DROP TABLE IF EXISTS admin_audit_log;
ALTER TABLE users DROP COLUMN suspended_at;

PRAGMA foreign_keys=OFF;
CREATE TABLE _user_roles_old (
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	role TEXT NOT NULL CHECK (role IN ('buyer','seller')),
	PRIMARY KEY (user_id, role)
);
INSERT INTO _user_roles_old (user_id, role)
	SELECT user_id, role FROM user_roles WHERE role <> 'admin';
DROP TABLE user_roles;
ALTER TABLE _user_roles_old RENAME TO user_roles;
PRAGMA foreign_keys=ON;