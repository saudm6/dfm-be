-- +goose Up
-- client_order_id is a UUIDv7 the client creates before submitting, so a
-- retried submission finds the same row instead of creating a second order.
CREATE TABLE orders (
    client_order_id uuid PRIMARY KEY CHECK (uuid_extract_version(client_order_id) = 7),
    symbol          text NOT NULL CHECK (symbol <> ''),
    side            text NOT NULL CHECK (side IN ('buy', 'sell')),
    quantity        numeric NOT NULL CHECK (quantity > 0),
    limit_price     numeric CHECK (limit_price > 0),
    state           text NOT NULL CHECK (state IN ('draft', 'submitted', 'working', 'part_filled', 'filled', 'cancelled', 'rejected')),
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL
);

-- order_events is the append-only audit log of every order and the record of
-- truth for its history. to_state is set when the event changed the order's
-- state (from_state is NULL only for the event that created the order); both
-- are NULL for a broker message or user action that left the state alone.
CREATE TABLE order_events (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    client_order_id uuid NOT NULL REFERENCES orders (client_order_id),
    source          text NOT NULL CHECK (source IN ('user', 'broker', 'system')),
    action          text NOT NULL CHECK (action <> ''),
    from_state      text CHECK (from_state IN ('draft', 'submitted', 'working', 'part_filled', 'filled', 'cancelled', 'rejected')),
    to_state        text CHECK (to_state IN ('draft', 'submitted', 'working', 'part_filled', 'filled', 'cancelled', 'rejected')),
    detail          jsonb NOT NULL DEFAULT '{}'::jsonb,
    recorded_at     timestamptz NOT NULL,
    CHECK (from_state IS NULL OR to_state IS NOT NULL)
);

CREATE INDEX order_events_client_order_id_idx ON order_events (client_order_id, id);

-- +goose StatementBegin
CREATE FUNCTION order_events_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'order_events is append-only: % is not allowed', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER order_events_no_update_or_delete
    BEFORE UPDATE OR DELETE ON order_events
    FOR EACH ROW EXECUTE FUNCTION order_events_append_only();

CREATE TRIGGER order_events_no_truncate
    BEFORE TRUNCATE ON order_events
    FOR EACH STATEMENT EXECUTE FUNCTION order_events_append_only();

-- +goose Down
DROP TABLE order_events;
DROP FUNCTION order_events_append_only();
DROP TABLE orders;
