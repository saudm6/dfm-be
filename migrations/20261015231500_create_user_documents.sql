-- +goose Up
-- body is json, not jsonb, so a document comes back with the keys and
-- numbers its client wrote. etag is the quoted strong entity tag of
-- (schema_version, body), set by the server on every write.
CREATE TABLE user_documents (
    user_id        bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind           text NOT NULL CHECK (kind IN ('layout', 'chart_template')),
    name           text NOT NULL CHECK (name ~ '^[A-Za-z0-9]([A-Za-z0-9 _.-]{0,62}[A-Za-z0-9_.-])?$'),
    body           json NOT NULL CHECK (json_typeof(body) = 'object'),
    schema_version integer NOT NULL CHECK (schema_version > 0),
    etag           text NOT NULL,
    created_at     timestamptz NOT NULL,
    updated_at     timestamptz NOT NULL,
    PRIMARY KEY (user_id, kind, name)
);

-- +goose Down
DROP TABLE user_documents;
