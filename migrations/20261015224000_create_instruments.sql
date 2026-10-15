-- +goose Up
-- The security master: one row per DFM trading code, kept after the code is
-- delisted or matures so its history still resolves. Empty text means DFM
-- does not publish the value.
CREATE TABLE instruments (
    symbol     text PRIMARY KEY CHECK (symbol <> ''),
    exchange   text NOT NULL CHECK (exchange IN ('DFM', 'NASDAQ_DUBAI')),
    isin       text NOT NULL,
    name_en    text NOT NULL CHECK (name_en <> ''),
    name_ar    text NOT NULL,
    class      text NOT NULL CHECK (class IN ('equity', 'etf', 'reit', 'fund', 'bond', 'sukuk', 'future', 'index')),
    currency   text NOT NULL CHECK (currency IN ('AED', 'USD', 'EUR', 'GBP', 'CNY', 'AUD', 'HKD', 'NZD')),
    sector     text NOT NULL,
    board      text NOT NULL,
    status     text NOT NULL CHECK (status IN ('listed', 'suspended', 'delisted', 'matured')),
    listed_at  timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX instruments_isin ON instruments (isin) WHERE isin <> '';

-- DFM board codes (the market-watch "market" field) and what trades on each,
-- derived from the instruments seen on the board at the last sync. A board
-- absent from the market watch keeps its row with lines 0, the exchanges and
-- classes it last had, and missing_since set to the first sync without it;
-- missing_since is null while the board is in the market watch.
CREATE TABLE boards (
    code          text PRIMARY KEY CHECK (code <> ''),
    exchanges     text[] NOT NULL,
    classes       text[] NOT NULL,
    lines         integer NOT NULL CHECK (lines >= 0),
    missing_since timestamptz,
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE boards;
DROP TABLE instruments;
