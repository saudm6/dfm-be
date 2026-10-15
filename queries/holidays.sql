-- name: ListHolidays :many
SELECT day FROM holidays ORDER BY day;

-- name: AddClosure :execrows
INSERT INTO holidays (day, reason, kind) VALUES ($1, $2, 'unscheduled')
ON CONFLICT (day) DO NOTHING;
