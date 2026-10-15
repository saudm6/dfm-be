-- +goose Up
-- The latest DFM market-watch quote of each instrument: one row per symbol,
-- rewritten only when DFM's modification time (as_of) changes. Prices and
-- sizes are 0 when DFM does not publish them; last_trade_at is null before
-- the first trade.
CREATE TABLE market_watch_quotes (
    symbol        text PRIMARY KEY REFERENCES instruments (symbol),
    last          numeric NOT NULL CHECK (last >= 0),
    bid           numeric NOT NULL CHECK (bid >= 0),
    ask           numeric NOT NULL CHECK (ask >= 0),
    bid_size      bigint NOT NULL CHECK (bid_size >= 0),
    ask_size      bigint NOT NULL CHECK (ask_size >= 0),
    volume        bigint NOT NULL CHECK (volume >= 0),
    trades        bigint NOT NULL CHECK (trades >= 0),
    value         numeric NOT NULL CHECK (value >= 0),
    prev_close    numeric NOT NULL CHECK (prev_close >= 0),
    last_trade_at timestamptz,
    as_of         timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE market_watch_quotes;
