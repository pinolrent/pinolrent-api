-- +goose Up
CREATE INDEX IF NOT EXISTS idx_cars_owner ON cars (owner_id, id);
CREATE INDEX IF NOT EXISTS idx_reservations_user ON reservations (user_id, id);
CREATE INDEX IF NOT EXISTS idx_reservations_car_dates ON reservations (car_id, start_date, end_date);
-- +goose Down
DROP INDEX IF EXISTS idx_cars_owner;
DROP INDEX IF EXISTS idx_reservations_user;
DROP INDEX IF EXISTS idx_reservations_car_dates;