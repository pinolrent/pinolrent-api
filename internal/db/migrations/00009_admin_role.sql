-- +goose Up
-- +goose NO TRANSACTION
-- Admin joins buyer/seller as a role membership. The role list lives in the
-- table CHECK, so SQLite needs a full rebuild to widen it.
PRAGMA foreign_keys=OFF;
BEGIN TRANSACTION;
CREATE TABLE _user_roles_new (
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	role TEXT NOT NULL CHECK (role IN ('buyer','seller','admin')),
	PRIMARY KEY (user_id, role)
);
INSERT INTO _user_roles_new (user_id, role) SELECT user_id, role FROM user_roles;
DROP TABLE user_roles;
ALTER TABLE _user_roles_new RENAME TO user_roles;
COMMIT;
PRAGMA foreign_keys=ON;

-- +goose Down
-- +goose NO TRANSACTION
PRAGMA foreign_keys=OFF;
BEGIN TRANSACTION;
DELETE FROM user_roles WHERE role = 'admin';
CREATE TABLE _user_roles_old (
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	role TEXT NOT NULL CHECK (role IN ('buyer','seller')),
	PRIMARY KEY (user_id, role)
);
INSERT INTO _user_roles_old (user_id, role) SELECT user_id, role FROM user_roles;
DROP TABLE user_roles;
ALTER TABLE _user_roles_old RENAME TO user_roles;
COMMIT;
PRAGMA foreign_keys=ON;
