-- name: UpsertInstrument :execrows
INSERT INTO instruments (symbol, exchange, isin, name_en, name_ar, class, currency, sector, board, status, listed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (symbol) DO UPDATE SET
    exchange = EXCLUDED.exchange, isin = EXCLUDED.isin, name_en = EXCLUDED.name_en,
    name_ar = EXCLUDED.name_ar, class = EXCLUDED.class, currency = EXCLUDED.currency,
    sector = EXCLUDED.sector, board = EXCLUDED.board, status = EXCLUDED.status,
    listed_at = EXCLUDED.listed_at, updated_at = now()
WHERE (instruments.exchange, instruments.isin, instruments.name_en, instruments.name_ar, instruments.class,
       instruments.currency, instruments.sector, instruments.board, instruments.status, instruments.listed_at)
    IS DISTINCT FROM
      (EXCLUDED.exchange, EXCLUDED.isin, EXCLUDED.name_en, EXCLUDED.name_ar, EXCLUDED.class,
       EXCLUDED.currency, EXCLUDED.sector, EXCLUDED.board, EXCLUDED.status, EXCLUDED.listed_at);

-- name: DelistMissingInstruments :execrows
UPDATE instruments SET status = 'delisted', updated_at = now()
WHERE status IN ('listed', 'suspended') AND class <> 'index' AND NOT (symbol = ANY(@present::text[]));

-- name: GetInstrument :one
SELECT * FROM instruments WHERE symbol = $1;

-- name: UpsertBoard :execrows
INSERT INTO boards (code, exchanges, classes, lines)
VALUES ($1, $2, $3, $4)
ON CONFLICT (code) DO UPDATE SET
    exchanges = EXCLUDED.exchanges, classes = EXCLUDED.classes, lines = EXCLUDED.lines, updated_at = now()
WHERE (boards.exchanges, boards.classes, boards.lines) IS DISTINCT FROM (EXCLUDED.exchanges, EXCLUDED.classes, EXCLUDED.lines);

-- name: ListBoards :many
SELECT * FROM boards ORDER BY code;
