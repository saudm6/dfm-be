# DFM historical data endpoint

Findings of spike saudm6/dfm-be#24. All requests below were run with plain `curl` from this host on 2026-10-14/15 (UTC). No cookies, API key, or bot challenge were needed.

## How the pages get their data

The three pages under `https://www.dfm.ae/the-exchange/statistics-reports/historical-data/` are one Nuxt app. The page HTML sets `runtimeConfig.public.apiBaseUrl` to `https://api2.dfm.ae/web/widgets/v1`. Each page component posts a form to `${apiBaseUrl}/data` with a `Command` field:

| Page | Bundle (2026-10-14 build) | Command |
|---|---|---|
| `company-prices` | `/_nuxt/DY2As9wY.js` | `SearchCompanyPrices` |
| `dfmgi` | `/_nuxt/Dz5v5gTR.js` | `SearchDFMIndex` |
| `sector-indices` | `/_nuxt/HIunBtNb.js` | `SearchSectorIndices` |

Bundle names change with each site deploy. The route-to-chunk map is in the entry bundle (`/_nuxt/DCAmwEyZ.js`; search it for `historical-data-company-prices`).

There is no separate export request. The DFMGI and sector-indices pages have a "Download Excel" button (component `DownloadExcel`, `/_nuxt/Bm1ixWid.js`). It builds a UTF-8 CSV with a BOM in the browser from the JSON already fetched. The company-prices page has no export button.

## Common request shape

- Method: `POST`
- URL: `https://api2.dfm.ae/web/widgets/v1/data`
- Body: `application/x-www-form-urlencoded`
- Dates: `YYYY-MM-DD`. `FromDate` and `ToDate` are both inclusive.
- Response: `200`, `Content-Type: text/plain; charset=utf-8`, body is JSON. Clients must parse the body as JSON whatever the content type says.

Headers and parameters actually required (each tested by removing it):

| Item | Required? | Observed effect when missing |
|---|---|---|
| `Accept: application/json` | no | byte-identical response |
| `Content-Type: application/x-www-form-urlencoded` | yes, but curl's `--data` sets it by default | a JSON body gets `302` with `{"Message":"There was an error processing the request.",...}` |
| `type=json` | yes | returns an HTML `<companyPrices><table ...>` fragment instead of JSON |
| `Period=custom` | yes | `FromDate`/`ToDate` are ignored and every total is 0 (company prices) |
| `Language=en` | no | byte-identical response; `Language=ar` also returned English sector names |
| User-Agent, cookies, Referer | no | none sent by the curl commands below |
| `GET` with a query string | not supported | `404 {"statusCode":404,"message":"Resource not found"}` |
| unknown `Command` | n/a | `200` with an empty body |

## Data range and row limit

- **Floor: 2025-01-02.** No endpoint returns trading days before 2025-01-02. The page text says "up to 2 years of historical market information". In practice, for a request made on 2026-10-14, history starts at the first trading day of the previous calendar year. Older data needs the DFM eServices "Historical Data Request" form (login required). It is not reachable from this API.
- Index endpoints with a `FromDate` before the floor are silently clamped. A request for `2020-01-01..2026-10-14` returns 446 points from 2025-01-02 to 2026-10-14. A range that lies wholly before the floor returns empty arrays and zeroed stats.
- **Trap:** `SearchCompanyPrices` for any date or range wholly before the floor does *not* return empty. It returns the same stale 2024-12-31 snapshot every time (`last_trade_price_date: "2024-12-31"`). See `testdata/dfm/company-prices-emaar-day-2024-10-15-before-floor.json`. Callers must reject any row whose `last_trade_price_date` is not the requested day.
- No row cap was observed. A 2-year index request returns every trading day in one response (446 rows, about 50 KB for DFMGI and 179 KB for all 8 sectors).
- Ten sequential single-day company-price calls took 2.5 s in total. No rate limiting or `429` was seen.

## Company prices (`SearchCompanyPrices`)

**The response is one aggregate row per security for the whole range, not daily bars.** A 2-year range for EMAAR returns a single row: `open` is the first day's open, `close` the last day's close, `high`/`low` the range extremes, and `volume`/`trade_count`/`value` the range totals. See `testdata/dfm/company-prices-emaar-range-2024-10-14_2026-10-14.json`.

To get daily OHLCV bars, set `FromDate = ToDate = <day>` and make one call per trading day. With `Companies=` left empty, one call returns every security that traded that day (53 securities on 2026-10-13). Securities that did not trade that day are left out (ACICO on 2026-10-13 returned no row). Weekends and holidays return all sectors with empty `securities`. A 2-year backfill is therefore about 446 calls, one per DFMGI trading day.

Parameters: `Command=SearchCompanyPrices`, `Period=custom`, `FromDate`, `ToDate`, `Companies` (comma-separated symbols, for example `EMAAR,DFM,DIB`; empty means all), `Language`, `type=json`.

Working curl (one symbol, one day):

```sh
curl -sS -X POST https://api2.dfm.ae/web/widgets/v1/data \
  -H 'Accept: application/json' \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data 'Command=SearchCompanyPrices&Period=custom&FromDate=2026-10-14&ToDate=2026-10-14&Companies=EMAAR&Language=en&type=json'
```

Response shape (fixture `testdata/dfm/company-prices-emaar-day-2026-10-14.json`):

```
{ total_securities, total_trades, total_volume, total_value,
  sectors: [ { name, total_securities, total_trades, total_volume, total_value,
               securities: [ { symbol, name, prev_close, close, open,
                               last_trade_price, last_trade_price_date ("YYYY-MM-DD"),
                               change, change_percentage, high, low,
                               trade_count, volume, value, page_url } ] } ] }
```

The response always lists all 18 sectors, including empty ones: the 18 entries of `GET https://api2.dfm.ae/web/sf/v1/sectors?sf_culture=en`. EMAAR on 2026-10-14: open 11.32, high 11.40, low 11.26, close 11.40, prev_close 11.30, volume 14 463 845, trades 2110. Consecutive days chain correctly: the 2026-10-13 close of 11.30 equals the 2026-10-14 `prev_close`.

Prices come in the response at up to 4 decimals (`high: 17.2500`). Parse them as decimals, not floats, if exact tick values matter.

The symbol universe comes from `Command=LiteSecuritiesLists&Language=en`. It returns `{Equities, Bonds, Sukuks, Funds, ETFs, REITs}`, each a list of `{ID, SecuritySymbol, FullName, Exchange, Sector, SecurityType}` (131 equities on 2026-10-14).

## DFM General Index (`SearchDFMIndex`)

Parameters: `Command=SearchDFMIndex`, `Period=custom`, `FromDate`, `ToDate`, `Language`, `type=json`.

Working curl (two years; clamped to the 2025-01-02 floor):

```sh
curl -sS -X POST https://api2.dfm.ae/web/widgets/v1/data \
  -H 'Accept: application/json' \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data 'Command=SearchDFMIndex&Period=custom&FromDate=2024-10-14&ToDate=2026-10-14&Language=en&type=json'
```

Response shape (fixture `testdata/dfm/dfmgi-2026-10-01_2026-10-14.json`):

```
{ stats: { previous_index, high, high_date, low, low_date,
           current_index, change, change_percentage },
  chart: { title: "DFM Index", points: [ { Item1: "YYYY-MM-DDT00:00:00", Item2: close } ] },  // oldest first
  yearly_closing: [ { year, value } ],   // the 4 calendar years before FromDate's year
  daily_closing: [ { d: "YYYY-MM-DDT00:00:00", i: close, c: change, cp: change_pct } ] }  // newest first
```

Only daily **closes** are available for the index: no open, high, low or volume per day. `stats.high`/`low` are intraday extremes over the range (5984.71 on 2026-10-01, above that day's 5930.40 close). `stats.previous_index` is the close of the day before `FromDate`. A 2-year request returned 446 `daily_closing` rows (2025-01-02 to 2026-10-14). `chart.points` holds the same values in the opposite order.

Export columns (DFMGI page "Download Excel", field map `Ve` in `Dz5v5gTR.js`), one row per `daily_closing` entry:

| CSV column | JSON field |
|---|---|
| `Date` | `d` |
| `DFM Index` | `i` |
| `Change` | `c` |
| `Chage %` (sic) | `cp` |

## Sector indices (`SearchSectorIndices`)

Parameters: `Command=SearchSectorIndices`, `Period=custom`, `FromDate`, `ToDate`, `Sector` (a `SectorID`; empty or unrecognised returns all 8 sectors), `Language`, `type=json`.

Sector IDs come from `GET https://api2.dfm.ae/web/sf/v1/sectors?sf_culture=en` (`value[].SectorID`/`SectorName`; the page lists those with `ShowOnIndices: true`). The endpoint returns these 8 sectors: 11 Financials, 22 Industrials, 33 Real Estate, 44 Utilities, 55 Communication Services, 66 Materials, 77 Consumer Staples, 88 Consumer Discretionary. `8888` DFMSI (Shari'a) and `9999` DFM Index are not served here; they fall back to all 8.

Working curl (Real Estate, two years):

```sh
curl -sS -X POST https://api2.dfm.ae/web/widgets/v1/data \
  -H 'Accept: application/json' \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data 'Command=SearchSectorIndices&Period=custom&FromDate=2024-10-14&ToDate=2026-10-14&Sector=33&Language=en&type=json'
```

Response shape (fixture `testdata/dfm/sector-indices-33-2026-10-01_2026-10-14.json`): a JSON array, one element per sector:

```
[ { name, id, open_price, high, high_date, low, low_date, current_index,
    change, change_percentage, year_closing (null observed),
    points: [ { Item1: "YYYY-MM-DDT00:00:00.000Z", Item2: close } ] } ]   // oldest first
```

- `points[].Item2` is the daily close **truncated to an integer** (Real Estate 2026-10-14: `current_index` 11289.84, last point 11289). Every one of the 3,568 points in a 2-year, all-sector response is a whole number. Exact sector closes are therefore not available per day from this endpoint.
- `open_price` is the close before `FromDate`: `change = current_index - open_price`.
- A 2-year request returned 446 points per sector (2025-01-02 to 2026-10-14).

Export columns (sector page "Download Excel", field map `De` in `HIunBtNb.js`), one row per sector. It is a range summary; the per-day points are not exported:

`Index` (name), `Open Price` (open_price), `High` (high), `High (Date)` (high_date), `Low` (low), `Low (Date)` (low_date), `Current Index` (current_index), `Change` (change), `Change %` (change_percentage, with `%` appended).

## Implications for the backfill (#25)

- Daily OHLCV for every security: one `SearchCompanyPrices` call per trading day with `Companies=` empty and `FromDate = ToDate`. Take the trading-day calendar from the DFMGI `daily_closing` dates. Drop rows whose `last_trade_price_date` is not the requested day.
- DFMGI: one `SearchDFMIndex` call for the whole range gives daily closes only.
- Sector indices: one `SearchSectorIndices` call gives integer-truncated daily closes only.
- History is limited to 2025-01-02 onward (as of 2026-10-14), not a full rolling 2 years.

## Fixtures

All fixtures are raw response bodies saved with `curl -o` on 2026-10-14/15 using the commands above, with the dates in the file names.

| File | Request |
|---|---|
| `testdata/dfm/company-prices-emaar-day-2026-10-14.json` | `SearchCompanyPrices`, EMAAR, 2026-10-14..2026-10-14 |
| `testdata/dfm/company-prices-emaar-range-2024-10-14_2026-10-14.json` | `SearchCompanyPrices`, EMAAR, 2024-10-14..2026-10-14 (one aggregate row) |
| `testdata/dfm/company-prices-emaar-day-2024-10-15-before-floor.json` | `SearchCompanyPrices`, EMAAR, 2024-10-15 (stale 2024-12-31 snapshot) |
| `testdata/dfm/dfmgi-2026-10-01_2026-10-14.json` | `SearchDFMIndex`, 2026-10-01..2026-10-14 |
| `testdata/dfm/sector-indices-33-2026-10-01_2026-10-14.json` | `SearchSectorIndices`, Sector=33, 2026-10-01..2026-10-14 |

## Not verified

- The network panel was not observed in a real browser: the T3 browser preview tool was unavailable in this session. The request shapes come from the page's own JS bundles (quoted above) and were reproduced exactly with curl.
- Whether the 2025-01-02 floor moves forward on 2027-01-01, as a "current year plus previous year" window would, or slides daily.
