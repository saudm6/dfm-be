-- name: UpsertMarketWatchQuote :execrows
INSERT INTO market_watch_quotes (symbol, last, bid, ask, bid_size, ask_size, volume, trades, value, prev_close, last_trade_at, as_of)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (symbol) DO UPDATE SET
    last = EXCLUDED.last, bid = EXCLUDED.bid, ask = EXCLUDED.ask,
    bid_size = EXCLUDED.bid_size, ask_size = EXCLUDED.ask_size, volume = EXCLUDED.volume,
    trades = EXCLUDED.trades, value = EXCLUDED.value, prev_close = EXCLUDED.prev_close,
    last_trade_at = EXCLUDED.last_trade_at, as_of = EXCLUDED.as_of, updated_at = now()
WHERE market_watch_quotes.as_of IS DISTINCT FROM EXCLUDED.as_of;

-- name: ListMarketWatchQuotes :many
SELECT * FROM market_watch_quotes ORDER BY symbol;

-- name: ListInstrumentSymbols :many
SELECT symbol FROM instruments ORDER BY symbol;
