# ADR 0001: Use the TWS API through IB Gateway for IBKR access

- Status: Accepted, based on documentation. Live measurement is still owed (see the last sections).
- Date: 2026-10-15
- Issue: [saudm6/dfm-be#12](https://github.com/saudm6/dfm-be/issues/12)
- Blocks: [saudm6/dfm-be#13](https://github.com/saudm6/dfm-be/issues/13) (IBKR session lifecycle)

## Decision

The DFM backend talks to Interactive Brokers through the **TWS API over a TCP socket to IB Gateway**. It does not use the Client Portal Web API.

| Item the issue asks for | Value | Basis |
| --- | --- | --- |
| Chosen API | TWS API through IB Gateway. Paper socket port `4002`. Go client [`github.com/scmhub/ibapi`](https://github.com/scmhub/ibapi), kept behind a DFM-owned interface | [D1], [D2], [G1] |
| Market-data line limit | 100 concurrent lines by default, and never fewer than 100. After the first month the limit is max(USD monthly commissions / 8, USD equity x 100 / 1,000,000, 100). The limit belongs to the username, so TWS watchlists and every API connection share it. IBKR lists 68 DFM stocks, so all of them fit inside the default 100 lines if nothing else uses lines. | [D3], [D4], [P2] |
| Session lifetime | IB Gateway with **Auto restart** turned on restarts itself every day and "can potentially run from Sunday to Sunday without re-authenticating". That is about **one week between manual logins**. The socket drops at each daily restart, so the backend must reconnect. | [D5] |
| Reauthentication | A person logs in to IB Gateway by hand once a week with username, password and 2FA. The backend reconnects on its own after every daily restart. The full steps are below. | [D5], [D6], [D7] |

Every number in this ADR comes from IBKR's documentation or from IBKR's public website. **Each one is documented, not measured.** No IBKR account or credentials existed on the build machine, so the paper-account runs the issue asks for did not happen. The list of measurements to take is in "Live measurement still owed" below.

## Context

The backend is a Go service. It needs level 1 quotes for DFM equities, limit order placement and cancellation, and positions and executions. One individual IBKR Pro user operates it. IBKR offers two retail APIs that fit:

- The **Client Portal Web API (CP)** uses HTTPS and a WebSocket. Individual accounts reach it through the Java Client Portal Gateway (CPGW) on `localhost:5000`. IBKR offers OAuth 1.0a and OAuth 2.0 only to advisors, brokers, prop groups, funds and third-party vendors, not to individual accounts [W1].
- The **TWS API** is a binary TCP socket protocol. It connects to a running Trader Workstation or IB Gateway session [D1].

All documentation was read on 2026-10-14 and 2026-10-15 from `https://www.interactivebrokers.com/docs/`. The legacy URL `https://www.interactivebrokers.com/campus/ibkr-api-page/twsapi-doc/` now redirects there. The legacy `.../campus/ibkr-api-page/cpapi-v1/` URL now returns 404, and its content lives under `/docs/web-api/`. Appending `.md` to any of the cited doc URLs returns the same page as Markdown, and that is how the pages were read.

## Comparison (all values documented, not measured)

| Dimension | Client Portal Web API (CPGW) | TWS API through IB Gateway |
| --- | --- | --- |
| Transport | HTTPS REST on `https://localhost:5000/v1/api` and a WebSocket on `wss://localhost:5000/v1/api/ws`, authenticated with the `api=<session>` cookie [W2], [W3] | TCP socket to IB Gateway. IB Gateway paper port is `4002` and live port is `4001` [D2]. Messages are end-of-line delimited, and IBKR says it has recently migrated the protocol to Protobuf [D1], [D8]. Up to 32 clients can connect to one Gateway [D1] |
| Official Go support | None. The service would use its own `net/http` client and a WebSocket library | None. IBKR ships Python, Java, C++, C# and VB [D9] and says a library in any other language only has to send and receive the same messages [D9] |
| Go client options and maintenance | [`tomlister/ibclient`](https://github.com/tomlister/ibclient): GPL-3.0, last push 2021-01-14, 7 stars. Effectively unmaintained, so the client would be written in-house [G2] | [`scmhub/ibapi`](https://github.com/scmhub/ibapi): MIT, release `v0.10.50` on 2026-09-17, last push 2026-09-17, mirrors the official API, Protobuf decode path, `go 1.26`, uses `zerolog`. [`scmhub/ibsync`](https://github.com/scmhub/ibsync) is a synchronous wrapper over it (`v0.10.50`, 2026-09-17). [`hadrianl/ibapi`](https://github.com/hadrianl/ibapi) (last release 2021, last push 2023) and [`gofinance/ib`](https://github.com/gofinance/ib) (last push 2021) are stale [G1] |
| Login | Browser login on the **same machine** as CPGW. Every API call must also come from that machine [W4]. "Interactive Brokers does not support an automated authentication process for Client Portal Gateway" [W5] | Username and password typed into the Gateway login window. "A headless session of TWS or IBGW without a GUI is not supported" [D5] |
| 2FA | Required. API-supported methods are IB Key, Handy Key, SMS and DSC+. Code cards are not supported [W6] | Same list [D10] |
| Reauthentication cadence | "Clients must reauthenticate using the Client Portal Gateway at daily" [W5] | Weekly, as long as Auto restart is on [D5], [D6] |
| Session lifetime | "Up to 24 hours, resetting at midnight for New York, U.S.; Zug, Switzerland; or Hong Kong time" [W7] | About one week between manual logins, with an automatic restart every day [D5] |
| Idle timeout and keepalive | The brokerage session ends after 5 minutes unused [W8] ("approximately 6 minutes" in the FAQ [W7]). Call `POST /tickle` about every 60 s [W9]. `/tickle` is paced at 1 req/s [W10]. On the WebSocket, send `tic` at least once a minute and still call `/tickle` every few minutes [W11]. The server sends a `system` heartbeat every 10 s [W12] | No API-level keepalive is documented. The socket stays up while the Gateway runs. Breakage shows up as `connectionClosed` or as error 502 (cannot open socket) or 507 (bad message / EOF) [D11], [D12] |
| Daily reset | `/iserver` is briefly unavailable around 01:00 local by region (01:00 US/Eastern, CEST or HKT) [W13]. Another page says the disconnect is at 00:00 local [W14] | The Gateway is "designed to be restarted daily". Auto restart handles it [D5] |
| Session-start ritual | `POST /iserver/auth/ssodh/init` with `{"publish":true,"compete":true}`, wait 2 s, then call `GET /iserver/accounts` until the response is non-empty. Suppressing order reply messages is recommended [W8], [W15] | Connect, then wait for the `nextValidId` callback before sending anything, because "function calls made prior to this time could be dropped" [D11] |
| One session per username | Yes, across TWS, Client Portal and mobile [W16] | Yes. A Gateway whose username is also used to log in to Client Portal "will not be able to automatically reconnect to the server after the next disconnection" [D7] |
| Market-data line limit | The `smd` subscription count is bounded by market-data lines [W17], which is the same pool for both APIs [D3], [D4] | Same pool [D3], [D4]. Tick-by-tick is capped at 5% of lines (5 at 100 lines). Level II depth is capped at 3 at 100 lines [D13] |
| Streaming mechanism | WebSocket topic `smd+<conid>+{"fields":[...]}`. Cancel with `umd+<conid>+{}` [W18]. **Streams end after 15 minutes, and the client must resubscribe after 10 minutes** [W17] | `reqMktData` streams until it is cancelled or the session disconnects. It delivers "aggregate snapshots taken several times per second", not tick-by-tick data [D14] |
| Update rate | "All Products: 500ms" [W17], [W19] | "All Other Products: 250ms". DFM falls in this row, because the table names only US stocks/futures/bonds/indices (250 ms), US options (100 ms) and Forex (5 ms) [D14] |
| Request pacing | Global limit of 10 req/s. `/iserver/marketdata/snapshot` 10 req/s. `/iserver/account/orders` and `/iserver/account/trades` 1 req per 5 s. `/portfolio/accounts` 1 req per 5 s. A 429 response puts the IP in a penalty box for 10 minutes [W20] (15 minutes on another page [W10]) | (market-data lines / 2) req/s, which is **50 req/s** at 100 lines. Only the initial request of a streaming subscription counts. Overruns are either paced by the Gateway or rejected with error 100, and three rejections disconnect the session [D15], [D16] |
| Order placement | `POST /iserver/account/{accountId}/orders`. Do not place the next order until the previous one is fully acknowledged [W21] | `placeOrder(orderId, contract, order)`, with results delivered through `openOrder` and `orderStatus` [D17] |
| Confirmation ("reply") flow | The response can be an order reply message `{id, message[], messageIds[]}` that must be confirmed with `POST /iserver/reply/{id}` and `{"confirmed":true}`. Confirming can produce further replies. Sending any other request first cancels the order, and the reply then fails with 503. Message types can be suppressed for the session with `POST /iserver/questions/suppress` [W22], [W23], [W24] | No in-band confirmation step exists. Precautionary prompts are bypassed in Gateway settings under Global Configuration > API > Precautions, for example "Bypass Order Precautions for API orders" [D18] |
| Cancel | `DELETE /iserver/account/{accountId}/order/{orderId}`. The response means the cancel request was received, not that the order is cancelled [W25] | `cancelOrder(orderId, OrderCancel)`. A client can cancel only orders placed by its own client ID, or call `reqGlobalCancel` [D19] |
| Positions | `GET /portfolio2/{accountId}/positions` ("near-real time", no cache) or the paged `GET /portfolio/{accountId}/positions/{pageId}` (100 per page). Either one needs `/portfolio/accounts` first [W26], [W27] | `reqPositions` returns a snapshot, then `positionEnd`, then pushed updates [D20] |
| Executions | `GET /iserver/account/trades` covers the current day and 6 previous days at 1 req per 5 s [W28]. The WebSocket topic `str` streams executions [W29] | `reqExecutions(filter)`. "IB Gateway is limited to only executions from the current trading day since midnight" [D21] |
| Contract lookup | `GET /trsrv/stocks?symbols=EMAAR` [W30] or `GET /iserver/secdef/search` [W31]. Conids are stable for the life of an instrument [W32]. A specific exchange is selected with `conid@EXCHANGE` (SMART is the default) [W17] | `reqContractDetails` with Symbol, SecType, Exchange and Currency [D22]. A contract needs a conId and exchange, or symbol, secType, exchange, primaryExchange and currency [D23] |

## DFM and EMAAR contract

IBKR's public Products and Exchanges search ([P1], queried on 2026-10-14 through the page's own `POST /webrest/search/products-by-filters` backend, with no login) returns:

| Field | Value |
| --- | --- |
| symbol / localSymbol | `EMAAR` |
| description | `EMAAR PROPERTIES PJSC` |
| secType | `STK` |
| IBKR exchange code | `DFM` (primary listing, `isPrimeExchId: "T"`) |
| currency | `AED` |
| conid | `792310341` |
| ISIN | `AEE000301011` |

The same search lists **68 `STK` products with exchange `DFM`**, all primary and all priced in AED [P2]. For the TWS API that gives `Contract{Symbol: "EMAAR", SecType: "STK", Exchange: "DFM", PrimaryExchange: "DFM", Currency: "AED"}`, or `Contract{ConID: 792310341, Exchange: "DFM"}`. For CP it gives `smd+792310341@DFM+{...}`.

DFM level 1 is **Fee Waived** for both non-professional and professional users on IBKR's market data pricing page ("Dubai Financial Market (L1), United Arab Emirates, Fee Waived, Fee Waived") [P3]. API market data still requires an open IBKR Pro account, $500 USD in the account on top of subscription costs, the subscription itself, and the Market Data API acknowledgement in Client Portal [D24]. A paper username receives data by sharing the live username's subscriptions. Each live user can share with one paper account [D25].

## Why the TWS API through IB Gateway

1. **Weekly human login instead of daily.** CPGW needs a browser login every day on the machine that runs it, and IBKR does not support automating that login [W4], [W5]. IB Gateway with Auto restart needs a manual login about once a week [D5]. Both APIs need a person for 2FA, but the TWS API needs one about five times less often on a trading week.
2. **Faster quotes and no forced resubscription.** CP watchlist data updates every 500 ms, and every `smd` stream dies after 15 minutes, so the backend would have to resubscribe every 10 minutes [W17]. `reqMktData` updates every 250 ms for DFM's product class and runs until it is cancelled [D14].
3. **More pacing headroom.** The TWS API allows 50 req/s at the default 100 lines and scales with lines [D15]. CP has a fixed global 10 req/s with a 10 to 15 minute IP penalty box [W10], [W20].
4. **Simpler order path.** CP's reply flow is stateful and fragile: any other request sent before the reply cancels the order [W23]. With IB Gateway, precautions are set once in configuration [D18].
5. **A maintained Go client exists.** `scmhub/ibapi` releases in step with the official API and already decodes Protobuf [G1]. CP has no maintained Go client [G2]. CP itself would be easy to hand-write, but that advantage does not outweigh points 1 to 4.

The Go service never imports a vendor type outside one adapter package. BE-13 defines a small DFM-owned interface (connect, contract lookup, quotes, orders, positions, executions) and implements it with `scmhub/ibapi`. That keeps the third-party client replaceable.

## Reauthentication steps (IB Gateway)

One-time setup:

1. Create a **dedicated IBKR username** for the backend. Only one brokerage session per username can exist across all IBKR platforms [W16]. A Gateway whose username also logs in to Client Portal cannot auto-reconnect after the next server reset [D7]. Market data subscriptions are per username, so subscribe DFM L1 (fee waived) on that username as well [D7], [P3].
2. In IB Gateway, go to Global Configuration > Lock and Exit and turn on **Auto restart** [D6].
3. In Global Configuration > API > Settings, turn on "ActiveX and Socket Clients", turn off "Read-Only API", and check the socket port (`4002` for paper) [D26], [D2]. Choose the pacing behavior: let the Gateway pace requests rather than reject them [D16].
4. In Global Configuration > API > Precautions, turn on "Bypass Order Precautions for API orders" [D18].
5. Make sure the username uses an API-supported 2FA method (IB Key, Handy Key, SMS or DSC+) [D10].

Weekly, by a person:

6. After the weekly reset, log in to the IB Gateway window by hand with username, password and 2FA. The reset is described as following "the nightly server reset on Saturday night" [D5], and another page says "the weekly authentication cycle starts on every Monday" [D6]. The log symptom of a missed weekly login is `Login failed = Soft token=0 received instead of expected permanent ...` [D6].

Automatic, in the backend (BE-13):

7. On `connectionClosed`, or on error 502, 507 or -1, mark the IBKR session as down and reconnect to the Gateway with backoff, reusing the same client ID [D11], [D12].
8. Treat `nextValidId` as "ready". Only then, resubscribe every `reqMktData` line, call `reqPositions` again, and call `reqExecutions` again for the day [D11], [D20], [D21].
9. If reconnection keeps failing past the daily restart window, report "IBKR login required" to the operator, because step 6 is due.

## Consequences

- IB Gateway is a GUI Java application, and IBKR does not support running it headless [D5]. Deployment has to give it a display, and the operator has to be able to reach its login window for the weekly login. *Engineering inference:* in a container that means a virtual display. IBKR does not document this.
- The service depends on a community library with no IBKR affiliation [G1]. The adapter interface limits the impact if it lags or breaks. The IBKR minimum Gateway version rises over time (10.30 from 2025-03-20) [D8], so the library and the Gateway must be upgraded together.
- TWS API 10.49 and later is published under the GPL [D8]. `scmhub/ibapi` is MIT and mirrors the official API [G1]. Whether a port of GPL source affects this project's licensing has not been assessed here. It is listed as a follow-up.
- The third-party login automator [IBC](https://github.com/IbcAlpha/IBC) is archived as of 2026-09 [G3], and IBKR does not support automated login [D5], [W5]. This ADR plans for a human doing the weekly login.

## Documentation inconsistencies found

- CP idle timeout: "5 minutes" [W8] versus "approximately 6 minutes" [W7].
- CP daily reset: 00:00 local, with a UTC column that reads 20:00 UTC for US/Eastern [W14], versus 01:00 local [W13].
- CP 429 penalty box: 15 minutes [W10] versus 10 minutes [W20].
- IB Gateway weekly login: after the "Saturday night" reset [D5] versus a cycle that "starts on every Monday" [D6].
- The TWS quick-start code comments list TWS ports as `7946`/`7947` [D2]. IB Gateway's `4001`/`4002` are listed consistently. DFM uses only `4002` (paper) and `4001` (live).

## Live measurement still owed

These are the Build items of #12 that need a funded IBKR Pro account with a paper username. Run each one for **both** APIs unless it says otherwise. Use a throwaway Go program on branch `be-12-spike-scratch`, as the issue says.

1. **Account setup (Build item 1).** In Client Portal, enable DFM (UAE) stock trading permission, declare non-professional market-data status, subscribe "Dubai Financial Market (L1)", sign the Market Data API acknowledgement, and share market data with the paper username. Record the subscription line as shown, and confirm it costs AED/USD 0 [P3], [D24], [D25].
2. **Contract resolution.** Resolve `EMAAR` through `reqContractDetails` (TWS) and `/trsrv/stocks?symbols=EMAAR` (CP). Record conid (expected `792310341`), exchange (expected `DFM`), primaryExchange, currency (expected `AED`), minTick, trading hours and whether SMART routing is offered. Paste the raw responses.
3. **L1 streaming during a DFM session.** Stream EMAAR during DFM trading hours. Record the availability code (CP field `6509`, which must start with `R` [W33]; for TWS, which `reqMarketDataType` is in effect). Record which fields arrive (bid, ask, last, sizes, volume) and capture a 10-minute raw log.
4. **Update rate.** From that log, compute the median and p95 interval between updates per field for EMAAR and two other liquid DFM names. Compare against the documented 250 ms (TWS) and 500 ms (CP).
5. **Line limit.** Subscribe DFM stocks one at a time, all 68 if the account allows, and then go past 100 using ADX names. Record the count at which IBKR refuses and the exact error code and text. Repeat with a TWS watchlist open on the same username to confirm the shared pool.
6. **Session lifetime.** IB Gateway: leave it running with Auto restart for 7 days. Log every disconnect with its UTC time and cause, and the time of the forced manual login. CP: leave a session idle with no `/tickle` and record when `authenticated` turns false. Then keep it tickled and record when it dies (expected near the midnight or 01:00 regional reset).
7. **Reauthentication.** Write down the exact human and API steps that restored service after each disconnect in item 6. Include whether `ssodh/init` alone was enough for CP and whether the Gateway recovered without a person after its daily restart.
8. **CP stream expiry.** Confirm the 15-minute `smd` expiry and that resubscribing at 10 minutes has no gap.
9. **Orders.** Place a paper limit BUY for EMAAR at least 20% below the market. Record the full confirmation path (CP reply messages and their `messageIds`, or TWS `openOrder`/`orderStatus` sequence), then cancel and record the time to `Cancelled`.
10. **Positions and executions.** Read positions (`reqPositions`, or `/portfolio2/{acct}/positions`) and executions (`reqExecutions`, or `/iserver/account/trades`) after a small paper fill. Check that the AED amounts and the conid match item 2.
11. **Pacing.** Confirm 50 req/s on TWS and 10 req/s on CP by observing the first error, without triggering the IP penalty box more than once.

If any measurement contradicts the comparison table in a way that changes the reasoning (for example, the Gateway needs a daily login on DFM's region, or `reqMktData` for DFM is slower than 500 ms), supersede this ADR with 0002.

## Throwaway code

None was written for this ADR. Without an account there was nothing to run against, and the unit was decided from documentation only. The live-measurement program in "Live measurement still owed" goes on branch [`be-12-spike-scratch`](https://github.com/saudm6/dfm-be/tree/be-12-spike-scratch) and never on `main`. That branch does not exist yet, so the link will resolve only once the paper-account spike runs.

## Sources

IBKR API documentation (base `https://www.interactivebrokers.com/docs`):

- [D1] TWS API Architecture: https://www.interactivebrokers.com/docs/tws-api/doc/architecture/introduction
- [D2] TWS API Quick Start, Placing Orders (default ports): https://www.interactivebrokers.com/docs/tws-api/doc/quick-start/placing-orders
- [D3] How Market Data is Allocated: https://www.interactivebrokers.com/docs/general/market-data-subscriptions/market-data-lines/how-market-data-is-allocated
- [D4] Market Data Lines: https://www.interactivebrokers.com/docs/general/market-data-subscriptions/market-data-lines/introduction
- [D5] The IB Gateway: https://www.interactivebrokers.com/docs/tws-api/doc/architecture/the-trader-workstation/the-ib-gateway
- [D6] Daily & Weekly Reauthentication: https://www.interactivebrokers.com/docs/tws-api/doc/tws-settings/daily-weekly-reauthentication
- [D7] Logging into multiple applications: https://www.interactivebrokers.com/docs/tws-api/doc/connectivity/logging-into-multiple-applications
- [D8] TWS API Limitations (Protobuf) and TWS API Changelog (10.49 GPL, minimum version 10.30): https://www.interactivebrokers.com/docs/tws-api/doc/notes-limitations/tws-api-limitations and https://www.interactivebrokers.com/docs/tws-api/changelog
- [D9] TWS API Introduction: https://www.interactivebrokers.com/docs/tws-api/doc/introduction
- [D10] TWS API Supported 2FA: https://www.interactivebrokers.com/docs/tws-api/doc/notes-limitations/supported-two-factor-authentication-2-fa
- [D11] Establishing an API connection: https://www.interactivebrokers.com/docs/tws-api/doc/connectivity/establishing-an-api-connection
- [D12] Broken API socket connection: https://www.interactivebrokers.com/docs/tws-api/doc/connectivity/broken-api-socket-connection
- [D13] Specialized Market Data Lines: https://www.interactivebrokers.com/docs/general/market-data-subscriptions/market-data-lines/specialized-market-data-lines
- [D14] TWS API Streaming Data (update frequency table): https://www.interactivebrokers.com/docs/tws-api/doc/md/streaming-data
- [D15] TWS API Pacing Limitations: https://www.interactivebrokers.com/docs/tws-api/doc/pacing-limitations/introduction
- [D16] TWS API Pacing Behavior: https://www.interactivebrokers.com/docs/tws-api/doc/pacing-limitations/pacing-behavior
- [D17] TWS API Quick Start, Placing Orders: https://www.interactivebrokers.com/docs/tws-api/doc/quick-start/placing-orders
- [D18] Order Precautions: https://www.interactivebrokers.com/docs/tws-api/doc/tws-settings/order-precautions
- [D19] Cancel Individual Order: https://www.interactivebrokers.com/docs/tws-api/doc/orders/cancelling-an-order/cancel-individual-order
- [D20] Positions: https://www.interactivebrokers.com/docs/tws-api/doc/account-portfolio-data/positions/introduction
- [D21] Execution Details: https://www.interactivebrokers.com/docs/tws-api/doc/order-management/execution-details/introduction
- [D22] Request Contract Details: https://www.interactivebrokers.com/docs/tws-api/doc/contracts-financial-instruments/contract-details/request-contract-details
- [D23] The Contract Object: https://www.interactivebrokers.com/docs/tws-api/doc/contracts-financial-instruments/the-contract-object
- [D24] Market Data Requirements: https://www.interactivebrokers.com/docs/general/market-data-subscriptions/market-data-requirements
- [D25] Market Data Sharing: https://www.interactivebrokers.com/docs/general/market-data-subscriptions/market-data-users/market-data-sharing
- [D26] TWS Configuration for API use: https://www.interactivebrokers.com/docs/tws-api/doc/tws-settings/tws-configuration-for-api-use/introduction
- [W1] Web API Authentication: https://www.interactivebrokers.com/docs/web-api/authentication/introduction
- [W2] CPGW Request Structure: https://www.interactivebrokers.com/docs/web-api/authentication/cpgw/request-requirements
- [W3] Establishing the Websocket with Client Portal Gateway: https://www.interactivebrokers.com/docs/web-api/v1/ws/connection-guide/establishing-the-websocket-with-client-portal-gateway
- [W4] Limitations of the Client Portal Gateway: https://www.interactivebrokers.com/docs/web-api/authentication/cpgw/limitations-of-the-client-portal-gateway
- [W5] Client Portal Gateway FAQ: https://www.interactivebrokers.com/docs/web-api/authentication/cpgw/client-portal-gateway-faq
- [W6] Web API Supported 2FA: https://www.interactivebrokers.com/docs/web-api/v1/requirements-limitations/supported-two-factor-authentication-2-fa
- [W7] Authentication FAQ: https://www.interactivebrokers.com/docs/web-api/authentication/faq
- [W8] Trading Sessions in the Web API: https://www.interactivebrokers.com/docs/web-api/authentication/trading
- [W9] Ping the server (`/tickle`): https://www.interactivebrokers.com/docs/web-api/v1/endpoints/session/ping-the-server
- [W10] Web API v1 Pacing Limitations: https://www.interactivebrokers.com/docs/web-api/v1/pacing-limitations
- [W11] Maintain Session (Ping): https://www.interactivebrokers.com/docs/web-api/v1/ws/session/maintain-session-ping
- [W12] System Connection Messages: https://www.interactivebrokers.com/docs/web-api/v1/ws/unsolicited-messages/system-connection-messages
- [W13] Scheduled Server Maintenance: https://www.interactivebrokers.com/docs/web-api/trading/usage-and-availability/scheduled-server-maintenance
- [W14] Regular Server Maintenance: https://www.interactivebrokers.com/docs/web-api/v1/regular-server-maintenance
- [W15] Initialize Brokerage Session: https://www.interactivebrokers.com/docs/web-api/v1/endpoints/session/initialize-brokerage-session
- [W16] Managing Multiple Sessions: https://www.interactivebrokers.com/docs/web-api/authentication/multiple-sessions
- [W17] WebSocket Market Data Request (`smd`): https://www.interactivebrokers.com/docs/web-api/v1/ws/market-data/market-data-request
- [W18] Streaming Top-of-Book Data: https://www.interactivebrokers.com/docs/web-api/trading/market-data/streaming-top-of-book-data
- [W19] Market Data Update Frequency: https://www.interactivebrokers.com/docs/web-api/v1/endpoints/market-data/market-data-update-frequency
- [W20] Trading Pacing Limitations: https://www.interactivebrokers.com/docs/web-api/trading/usage-and-availability/pacing-limitations
- [W21] Place Order: https://www.interactivebrokers.com/docs/web-api/v1/endpoints/orders/place-order
- [W22] Order Reply Messages: https://www.interactivebrokers.com/docs/web-api/trading/orders/order-reply-messages
- [W23] Place Order Reply Confirmation: https://www.interactivebrokers.com/docs/web-api/v1/endpoints/orders/place-order-reply-confirmation
- [W24] Order Reply Suppression: https://www.interactivebrokers.com/docs/web-api/trading/orders/order-reply-suppression
- [W25] Canceling Orders: https://www.interactivebrokers.com/docs/web-api/trading/orders/canceling-orders
- [W26] Positions (NEW): https://www.interactivebrokers.com/docs/web-api/v1/endpoints/portfolio/positions-new
- [W27] Positions: https://www.interactivebrokers.com/docs/web-api/v1/endpoints/portfolio/positions
- [W28] Trades: https://www.interactivebrokers.com/docs/web-api/v1/endpoints/order-monitoring/trades
- [W29] Request Trades data (`str`): https://www.interactivebrokers.com/docs/web-api/v1/ws/order-position-operations/request-trades-data
- [W30] Security Stocks by Symbol: https://www.interactivebrokers.com/docs/web-api/v1/endpoints/contract/security-stocks-by-symbol
- [W31] Search Contract by Symbol: https://www.interactivebrokers.com/docs/web-api/v1/endpoints/contract/search-contract-by-symbol
- [W32] Instrument Discovery: https://www.interactivebrokers.com/docs/web-api/trading/instrument-discovery/introduction
- [W33] Market Data Availability: https://www.interactivebrokers.com/docs/web-api/v1/endpoints/market-data/market-data-availability

IBKR public website:

- [P1] Products and Exchanges search (EMAAR): https://www.interactivebrokers.com/en/trading/products-exchanges.php, backed by `POST https://www.interactivebrokers.com/webrest/search/products-by-filters` with `{"productCountry":["AE"],"productSymbol":"EMAAR","productType":["STK"]}`
- [P2] The same endpoint with `{"productCountry":["AE"],"productSymbol":"","productType":["STK"],"pageSize":"500"}`: 167 products, of which 68 have `exchangeId: "DFM"` and 99 have `"ADX"`
- [P3] Market Data Pricing: https://www.interactivebrokers.com/en/pricing/market-data-pricing.php

GitHub (repository metadata read through the GitHub API on 2026-10-14):

- [G1] https://github.com/scmhub/ibapi (README, `go.mod`, releases), https://github.com/scmhub/ibsync, https://github.com/hadrianl/ibapi, https://github.com/gofinance/ib
- [G2] https://github.com/tomlister/ibclient
- [G3] https://github.com/IbcAlpha/IBC
