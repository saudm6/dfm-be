-- name: InsertUser :one
INSERT INTO users (username, password_hash, role, created_at)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: UserByUsername :one
SELECT id, username, password_hash, role FROM users WHERE username = $1;

-- name: InsertSession :exec
INSERT INTO sessions (token_hash, user_id, csrf_token, created_at, idle_expires_at, absolute_expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- TouchSession returns the live session with token_hash and extends its idle
-- expiry. It returns no row for an unknown or expired session.
-- name: TouchSession :one
UPDATE sessions
SET idle_expires_at = LEAST(sqlc.arg(idle_expires_at)::timestamptz, sessions.absolute_expires_at)
FROM users
WHERE sessions.token_hash = sqlc.arg(token_hash)
  AND sessions.idle_expires_at > sqlc.arg(now)::timestamptz
  AND users.id = sessions.user_id
RETURNING users.id, users.username, users.role, sessions.csrf_token;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = $1;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE idle_expires_at <= $1;
