-- +goose Up
CREATE TABLE IF NOT EXISTS revoked_tokens (
	jti TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id),
	expires_at INTEGER NOT NULL
);

-- Used by the periodic GC that drops rows whose token has already
-- expired. Keeping the table small bounds memory and scan time.
CREATE INDEX IF NOT EXISTS idx_revoked_tokens_expires_at
	ON revoked_tokens (expires_at);
-- +goose Down
DROP INDEX IF EXISTS idx_revoked_tokens_expires_at;
DROP TABLE IF EXISTS revoked_tokens;
