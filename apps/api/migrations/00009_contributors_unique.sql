-- +goose Up
CREATE UNIQUE INDEX IF NOT EXISTS ux_contributors_name ON contributors (name);

-- +goose Down
DROP INDEX IF EXISTS ux_contributors_name;
