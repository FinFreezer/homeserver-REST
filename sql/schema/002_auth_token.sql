-- +goose Up
ALTER TABLE users
ADD COLUMN authtoken TEXT UNIQUE;

-- +goose Down
ALTER TABLE users
DROP COLUMN authtoken;
