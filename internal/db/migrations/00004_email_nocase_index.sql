-- +goose Up
-- The application already normalizes email to lowercase before INSERT
-- and SELECT, so the existing UNIQUE constraint on users.email works
-- correctly in practice. This index adds a safety net at the database
-- level: if a row is ever inserted without going through the app (e.g.
-- a manual INSERT or a future migration), the case-insensitive unique
-- constraint still holds. SQLite's lower() is ASCII-only, which is
-- fine because the app's email regex also rejects non-ASCII local parts.
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_lower
	ON users (lower(email));
-- +goose Down
DROP INDEX IF EXISTS idx_users_email_lower;
