-- name: GetDocument :one
SELECT body, schema_version, etag
FROM user_documents
WHERE user_id = $1 AND kind = $2 AND name = $3;

-- name: ListDocuments :many
SELECT name, etag, updated_at
FROM user_documents
WHERE user_id = $1 AND kind = $2
ORDER BY name COLLATE "C";

-- InsertDocument returns no row when the document already exists.
-- name: InsertDocument :one
INSERT INTO user_documents (user_id, kind, name, body, schema_version, etag, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
ON CONFLICT (user_id, kind, name) DO NOTHING
RETURNING updated_at;

-- UpdateDocument returns no row when the document does not exist or its
-- etag is not one of if_match (any_etag accepts every etag).
-- name: UpdateDocument :one
UPDATE user_documents
SET body = sqlc.arg(body), schema_version = sqlc.arg(schema_version), etag = sqlc.arg(etag), updated_at = sqlc.arg(updated_at)
WHERE user_id = sqlc.arg(user_id) AND kind = sqlc.arg(kind) AND name = sqlc.arg(name)
  AND (sqlc.arg(any_etag)::boolean OR user_documents.etag = ANY(sqlc.arg(if_match)::text[]))
RETURNING updated_at;

-- DeleteDocument deletes no row when the document does not exist or its
-- etag is not one of if_match (any_etag accepts every etag).
-- name: DeleteDocument :execrows
DELETE FROM user_documents
WHERE user_id = sqlc.arg(user_id) AND kind = sqlc.arg(kind) AND name = sqlc.arg(name)
  AND (sqlc.arg(any_etag)::boolean OR etag = ANY(sqlc.arg(if_match)::text[]));

-- name: DocumentExists :one
SELECT EXISTS (
    SELECT 1 FROM user_documents WHERE user_id = $1 AND kind = $2 AND name = $3
);
