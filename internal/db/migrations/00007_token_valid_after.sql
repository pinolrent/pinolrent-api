-- +goose Up
-- Unix instant after which only newly issued tokens validate; NULL = all
-- tokens valid. Stamped on password change and on refresh replay to revoke
-- every session of the user at once.
ALTER TABLE users ADD COLUMN token_valid_after INTEGER;

-- +goose Down
ALTER TABLE users DROP COLUMN token_valid_after;
