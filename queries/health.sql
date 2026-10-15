-- name: TimescaleDBVersion :one
SELECT extversion FROM pg_catalog.pg_extension WHERE extname = 'timescaledb';
