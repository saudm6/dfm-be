-- name: InsertOrder :one
INSERT INTO orders (client_order_id, symbol, side, quantity, limit_price, state, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
ON CONFLICT (client_order_id) DO NOTHING
RETURNING *;

-- name: GetOrder :one
SELECT * FROM orders WHERE client_order_id = $1;

-- name: LockOrder :one
SELECT * FROM orders WHERE client_order_id = $1 FOR UPDATE;

-- name: SetOrderState :one
UPDATE orders SET state = $2, updated_at = $3
WHERE client_order_id = $1
RETURNING *;

-- name: InsertOrderEvent :one
INSERT INTO order_events (client_order_id, source, action, from_state, to_state, detail, recorded_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListOrderEvents :many
SELECT * FROM order_events WHERE client_order_id = $1 ORDER BY id;
