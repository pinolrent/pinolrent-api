-- +goose Up
-- Additive: existing rows get an empty phone, and the CHECK only accepts
-- canonical E.164 (a leading + plus 8 to 15 digits) or the empty string.
ALTER TABLE users ADD COLUMN phone TEXT NOT NULL DEFAULT ''
	CHECK (phone = '' OR (substr(phone, 1, 1) = '+' AND length(phone) BETWEEN 9 AND 16));

-- +goose Down
ALTER TABLE users DROP COLUMN phone;
