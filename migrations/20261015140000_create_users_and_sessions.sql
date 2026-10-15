-- +goose Up
CREATE TABLE users (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    username      text NOT NULL UNIQUE CHECK (username ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    password_hash text NOT NULL CHECK (password_hash LIKE '$argon2id$%'),
    role          text NOT NULL CHECK (role IN ('owner', 'member')),
    created_at    timestamptz NOT NULL
);

-- token_hash is the SHA-256 of the cookie's random token, so reading this
-- table does not yield a usable session. idle_expires_at moves forward on
-- each request but never past absolute_expires_at.
CREATE TABLE sessions (
    token_hash          bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    user_id             bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    csrf_token          text NOT NULL,
    created_at          timestamptz NOT NULL,
    idle_expires_at     timestamptz NOT NULL,
    absolute_expires_at timestamptz NOT NULL,
    CHECK (idle_expires_at <= absolute_expires_at)
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_idle_expires_at_idx ON sessions (idle_expires_at);

-- +goose Down
DROP TABLE sessions;
DROP TABLE users;
