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
| Market-data line limit | 100 concurrent lines by default, and never fewer than 100. After the first month the limit is max(USD monthly commissions / 8, USD equity x 100 / 1,000,000, 100). The limit belongs to the username, so TWS watchlists and every API connection share it. IBKR lists 68 DFM stocks, so all of them fit inside the default 100 lines if nothing else uses lines. Going past the limit is refused with error 101, "Max number of tickers has been reached." | [D3], [D4], [P2], [D28] |
| Session lifetime | IB Gateway with **Auto restart** turned on restarts itself every day and "can potentially run from Sunday to Sunday without re-authenticating". That is about **one week between manual logins**. The socket drops at each daily restart, so the backend must reconnect. Separately, the Gateway loses its own link to IBKR's servers "at least once a day" during server maintenance while the API socket stays open. That shows up only as system messages 1100, 1101, 1102 and 2110, not as a closed socket. | [D5], [D27], [D28] |
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
| Idle timeout and keepalive | The brokerage session ends after 5 minutes unused [W8] ("approximately 6 minutes" in the FAQ [W7]). Call `POST /tickle` about every 60 s [W9]. `/tickle` is paced at 1 req/s [W10]. On the WebSocket, send `tic` at least once a minute and still call `/tickle` every few minutes [W11]. The server sends a `system` heartbeat every 10 s [W12] | No API-level keepalive is documented. The socket stays up while the Gateway runs. There are two kinds of breakage. (1) The API socket breaks: `connectionClosed`, or error 502 (cannot open socket), 507 (bad message / EOF) or -1 [D11], [D12]. Reconnecting while the old connection still holds the client ID gives error 326, "Unable to connect as the client id is already in use" [D12], [D28]. (2) The Gateway loses IBKR's servers while the API socket stays open: 1100 "Connectivity between IB and the TWS has been lost" (internet, nightly reset or a competing session), then 1101 (restored, market data lost and must be re-submitted) or 1102 (restored, market data kept) [D27]. Error 2110 "Connectivity between TWS and server is broken. It will be restored automatically" usually occurs during the nightly reset [D28]. These arrive through `EWrapper.error`, which carries notifications as well as true errors [D29] |
| Daily reset | `/iserver` is briefly unavailable around 01:00 local by region (01:00 US/Eastern, CEST or HKT) [W13]. Another page says the disconnect is at 00:00 local [W14] | The Gateway is "designed to be restarted daily". Auto restart handles it [D5] |
| Session-start ritual | `POST /iserver/auth/ssodh/init` with `{"publish":true,"compete":true}`, wait 2 s, then call `GET /iserver/accounts` until the response is non-empty. Suppressing order reply messages is recommended [W8], [W15] | Connect, then wait for the `nextValidId` callback before sending anything, because "function calls made prior to this time could be dropped" [D11] |
| One session per username | Yes, across TWS, Client Portal and mobile [W16] | Yes. A Gateway whose username is also used to log in to Client Portal "will not be able to automatically reconnect to the server after the next disconnection" [D7]. A Gateway logged in to a paper account will not receive market data if the live username it shares data from logs in to Client Portal [D7]. Requesting live data from the paper and live accounts at the same time gives error 10197, "No market data during competing session", and the live account wins [D28] |
| Market-data line limit | The `smd` subscription count is bounded by market-data lines [W17], which is the same pool for both APIs [D3], [D4] | Same pool [D3], [D4]. Over the limit, IBKR answers error 101 "Max number of tickers has been reached" [D28]. Tick-by-tick is capped at 5% of lines (5 at 100 lines). Level II depth is capped at 3 at 100 lines [D13] |
| Streaming mechanism | WebSocket topic `smd+<conid>+{"fields":[...]}`. Cancel with `umd+<conid>+{}` [W18]. **Streams end after 15 minutes, and the client must resubscribe after 10 minutes** [W17] | `reqMktData` streams until it is cancelled or the session disconnects [D15]. It delivers "aggregate snapshots taken several times per second", not tick-by-tick data [D14]. After a 1101 the requests are lost and must be re-submitted [D27] |
| Update rate | "All Products: 500ms" [W17], [W19] | "All Other Products: 250ms". DFM falls in this row, because the table names only US stocks/futures/bonds/indices (250 ms), US options (100 ms) and Forex (5 ms) [D14] |
| Request pacing | Global limit of 10 req/s. `/iserver/marketdata/snapshot` 10 req/s. `/iserver/account/orders` and `/iserver/account/trades` 1 req per 5 s [W10] (listed as `/iserver/orders` and `/iserver/trades` in [W20]). `/portfolio/accounts` 1 req per 5 s. A 429 response puts the IP in a penalty box for 10 minutes [W20] (15 minutes on another page [W10]) | (market-data lines / 2) req/s, which is **50 req/s** at 100 lines. Only the initial request of a streaming subscription counts. Overruns are either paced by the Gateway or rejected with error 100, and three rejections disconnect the session [D15], [D16] |
| Order placement | `POST /iserver/account/{accountId}/orders`. Do not place the next order until the previous one is fully acknowledged [W21] | `placeOrder(orderId, contract, order)`, with results delivered through `openOrder` and `orderStatus` [D17]. Modify by calling `placeOrder` again with the same order ID from the same client ID, and IBKR recommends changing only price, size or TIF [D32]. `reqOpenOrders` returns the active orders placed by this client ID, and `reqAllOpenOrders` returns all open orders once [D31] |
| Confirmation ("reply") flow | The response can be an order reply message `{id, message[], messageIds[]}` that must be confirmed with `POST /iserver/reply/{id}` and `{"confirmed":true}`. Confirming can produce further replies. Sending any other request first cancels the order, and the reply then fails with 503. Message types can be suppressed for the session with `POST /iserver/questions/suppress` [W22], [W23], [W24] | No in-band confirmation step exists. Precautionary prompts are bypassed in Gateway settings under Global Configuration > API > Precautions, for example "Bypass Order Precautions for API orders" [D18]. IBKR still reports problems after the fact: error 201 "Order rejected – Reason:", error 202 with a cancelled status for price checks, warning text in `openOrder` [D30], error 399 "Order message error" [D28], and an `advancedOrderRejectJson` string on `EWrapper.error` [D29] |
| Cancel | `DELETE /iserver/account/{accountId}/order/{orderId}`. The response means the cancel request was received, not that the order is cancelled [W25] | `cancelOrder(orderId, OrderCancel)`. A client can cancel only orders placed by its own client ID, or call `reqGlobalCancel` [D19] |
| Positions | `GET /portfolio2/{accountId}/positions` ("near-real time", no cache) or the paged `GET /portfolio/{accountId}/positions/{pageId}` (100 per page). Either one needs `/portfolio/accounts` first [W26], [W27] | `reqPositions` returns a snapshot, then `positionEnd`, then pushed updates [D20]. Cash and buying power come from `reqAccountSummary` with tags such as `TotalCashValue`, `AvailableFunds` and `BuyingPower`. Changed values are re-sent every three minutes, and only two summary subscriptions may be active at once [D33] |
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
4. **Order path: roughly even, not a deciding factor.** CP's reply flow is stateful: any other request sent before the reply cancels the order [W23]. CP can suppress reply message types by `messageId` for the rest of the brokerage session, after which valid orders are accepted at once [W24], so the flow is manageable, but the suppression has to be re-sent for every new session, which lasts at most 24 hours [W7]. With IB Gateway, precautions are bypassed once in configuration [D18], and warnings and rejections arrive asynchronously through `error` and `openOrder` [D29], [D30]. Either way DFM has to surface IBKR's warnings itself (see Consequences). The choice rests on points 1, 2, 3 and 5.
5. **A maintained Go client exists.** `scmhub/ibapi` releases in step with the official API and already decodes Protobuf [G1]. CP has no maintained Go client [G2]. CP itself would be easy to hand-write, but that advantage does not outweigh points 1 to 4.

The Go service never imports a vendor type outside one adapter package. BE-13 defines a small DFM-owned interface and implements it with `scmhub/ibapi`. The interface covers connect and session state (including the 1100/1101/1102/2110 states), contract lookup, quotes, order place, modify and cancel [D17], [D19], [D32], open-order reconciliation for BE-42 [D31], positions [D20], account values such as cash and buying power for BE-45 [D33], executions [D21], and order warnings and rejections passed through verbatim [D28], [D29], [D30]. That keeps the third-party client replaceable.

## Reauthentication steps (IB Gateway)

One-time setup:

1. Create a **dedicated IBKR username** for the backend. Only one brokerage session per username can exist across all IBKR platforms [W16]. A Gateway whose username also logs in to Client Portal cannot auto-reconnect after the next server reset [D7]. Market data subscriptions are per username, so subscribe DFM L1 (fee waived) on that username as well [D7], [P3]. For the paper Gateway, the live username it shares market data from must not log in to Client Portal either, or the paper Gateway receives no market data [D7]. Requesting live data on the paper and live accounts at once gives error 10197 and the live account wins [D28].
2. In IB Gateway, go to Global Configuration > Lock and Exit and turn on **Auto restart**. The same page sets the time of day the Gateway shuts down [D6]. Pick a time outside DFM's published sessions, which run from the 09:30 pre-opening to the 15:00 close, UAE time (GMT+4) [X1].
3. In Global Configuration > API > Settings, turn on "ActiveX and Socket Clients", turn off "Read-Only API", and check the socket port (`4002` for paper) [D26], [D2]. Choose the pacing behavior: let the Gateway pace requests rather than reject them [D16].
4. In Global Configuration > API > Precautions, turn on "Bypass Order Precautions for API orders" [D18].
5. Make sure the username uses an API-supported 2FA method (IB Key, Handy Key, SMS or DSC+) [D10].

Weekly, by a person:

6. After the weekly reset, log in to the IB Gateway window by hand with username, password and 2FA. The reset is described as following "the nightly server reset on Saturday night" [D5], and another page says "the weekly authentication cycle starts on every Monday" [D6]. The log symptom of a missed weekly login is `Login failed = Soft token=0 received instead of expected permanent ...` [D6].

Automatic, in the backend (BE-13):

7. **Socket down.** On `connectionClosed`, or on error 502, 507 or -1, mark the IBKR session as down and reconnect to the Gateway with backoff, reusing the same client ID [D11], [D12]. Treat error 326 ("client id is already in use") as retryable and keep backing off, because after a half-open drop the Gateway can still hold the old connection's client ID [D12], [D28]. Keep the same client ID: only that client ID can modify or cancel its orders and see them through `reqOpenOrders` [D31], [D32].
8. **Socket ready.** Treat `nextValidId` as "ready" [D11]. Only then resubscribe every `reqMktData` line, call `reqPositions`, `reqAccountSummary` and `reqOpenOrders` again (open-order reconciliation for BE-42), and call `reqExecutions` again for the day [D20], [D21], [D31], [D33].
9. **Socket up, IBKR servers down.** On 1100 or 2110, mark the session as degraded and every quote as stale. Do not report quotes as real time and do not close the socket. The Gateway reconnects to IBKR on its own [D27], [D28].
   - On 1101, resubscribe every `reqMktData` line, because the market data requests "have been lost and need to be re-submitted" [D27]. Then mark the session ready.
   - On 1102, the requests were kept, so mark the session ready without resubscribing [D27].
   - In both cases call `reqOpenOrders`, `reqPositions` and `reqExecutions` again. IBKR says execution reports are delayed until a reset completes [D27], so they may have arrived late. *Engineering inference; IBKR does not prescribe this step.*
10. If reconnection keeps failing past the daily restart window, report "IBKR login required" to the operator, because step 6 is due.

## Consequences

- IB Gateway is a GUI Java application, and IBKR does not support running it headless [D5]. Deployment has to give it a display, and the operator has to be able to reach its login window for the weekly login. *Engineering inference:* in a container that means a virtual display. IBKR does not document this.
- The service depends on a community library with no IBKR affiliation [G1]. The adapter interface limits the impact if it lags or breaks. The IBKR minimum Gateway version rises over time (10.30 from 2025-03-20) [D8], so the library and the Gateway must be upgraded together.
- **Order confirmations: BE-42 is re-specified.** With "Bypass Order Precautions for API orders" on [D18], the TWS API has no confirmation prompt that the backend could answer selectively, so [saudm6/dfm-be#42](https://github.com/saudm6/dfm-be/issues/42) Build item 2 ("Answer IBKR's order confirmation prompts only for warnings the risk checks already cover. Show any other prompt to the user.") cannot be built as written. It is replaced by: **DFM's own pre-trade risk checks are the only pre-trade gate. Every IBKR warning and rejection is shown to the user verbatim**: error 201 "Order rejected – Reason:" and 202 [D30], error 399 "Order message error" [D28], warning text on `openOrder` [D30], and the `advancedOrderRejectJson` string from `EWrapper.error` [D29].
- **No prior-day fills through IB Gateway.** "The IB Gateway is limited to only executions from the current trading day since midnight" [D21]. If the backend is down across midnight, BE-45 cannot backfill the missed fills through `reqExecutions`. Positions still come back through `reqPositions` [D20], but the individual fills need another source. Finding that source is a follow-up for BE-45.
- **Licensing risk.** IBKR's changelog says "TWS API 10.49+ is released under the GNU General Public License (GPL)" [D8]. `scmhub/ibapi` declares MIT [G1], yet its `v0.10.49` and `v0.10.50` releases track TWS API 10.49+ (the `v0.10.49` commit adds `ContractDetails.SettlementMethod`, which the changelog ties to 10.49 [D8], [G1]). Pinning `v0.10.48` or older is no cleaner: IBKR provides its earlier API source "with a Non-Commercial license agreement" [L1], and the library's MIT claim may not hold for code ported from that source either. **Position:** `saudm6/dfm-be` is a public GitHub repository with no LICENSE file (`gh repo view saudm6/dfm-be --json visibility,licenseInfo` returned `{"licenseInfo":null,"visibility":"PUBLIC"}` on 2026-10-15). Its source is published, and it will reference `scmhub/ibapi` through `go.mod` rather than vendoring it. No binaries or container images are distributed: the service runs privately for one operator. *This is an engineering reading, not legal advice:* GPL obligations attach when the covered work is conveyed (a binary or image built with the library, or the library's source). Publishing DFM's own source while running the binary privately does not convey the library, so the project accepts the risk for now. **Owed: a licence review before any distribution of binaries or images**, listed under "Live measurement still owed" below.
- The third-party login automator [IBC](https://github.com/IbcAlpha/IBC) is archived as of 2026-09 [G3], and IBKR does not support automated login [D5], [W5]. This ADR plans for a human doing the weekly login.

## Documentation inconsistencies found

- CP idle timeout: "5 minutes" [W8] versus "approximately 6 minutes" [W7].
- CP daily reset: 00:00 local, with a UTC column that reads 20:00 UTC for US/Eastern [W14], versus 01:00 local [W13].
- CP 429 penalty box: 15 minutes [W10] versus 10 minutes [W20].
- IB Gateway weekly login: after the "Saturday night" reset [D5] versus a cycle that "starts on every Monday" [D6].
- The TWS quick-start code comments list TWS ports as `7946`/`7947` [D2]. IB Gateway's `4001`/`4002` are listed consistently. DFM uses only `4002` (paper) and `4001` (live).

## Live measurement still owed

Items 1 to 11 are the Build items of #12 that need a funded IBKR Pro account with a paper username. Items 12 to 15 were added in review round 1. Run each one for **both** APIs unless it says otherwise. Use a throwaway Go program on branch `be-12-spike-scratch`, as the issue says.

1. **Account setup (Build item 1).** In Client Portal, enable DFM (UAE) stock trading permission, declare non-professional market-data status, subscribe "Dubai Financial Market (L1)", sign the Market Data API acknowledgement, and share market data with the paper username. Record the subscription line as shown, and confirm it costs AED/USD 0 [P3], [D24], [D25].
2. **Contract resolution.** Resolve `EMAAR` through `reqContractDetails` (TWS) and `/trsrv/stocks?symbols=EMAAR` (CP). Record conid (expected `792310341`), exchange (expected `DFM`), primaryExchange, currency (expected `AED`), minTick, trading hours and whether SMART routing is offered. Paste the raw responses.
3. **L1 streaming during a DFM session.** Stream EMAAR during DFM trading hours. Record the availability code (CP field `6509`, which must start with `R` [W33]; for TWS, which `reqMarketDataType` is in effect). Record which fields arrive (bid, ask, last, sizes, volume) and capture a 10-minute raw log.
4. **Update rate.** From that log, compute the median and p95 interval between updates per field for EMAAR and two other liquid DFM names. Compare against the documented 250 ms (TWS) and 500 ms (CP).
5. **Line limit.** Subscribe DFM stocks one at a time, all 68 if the account allows, and then go past 100 using ADX names. Record the count at which IBKR refuses and the exact error code and text. The documented refusal is error 101, "Max number of tickers has been reached" [D28]: confirm that code and text, and whether the refused line consumes anything. Repeat with a TWS watchlist open on the same username to confirm the shared pool.
6. **Session lifetime.** IB Gateway: leave it running with Auto restart for 7 days. Log every disconnect with its UTC time and cause, and the time of the forced manual login. CP: leave a session idle with no `/tickle` and record when `authenticated` turns false. Then keep it tickled and record when it dies (expected near the midnight or 01:00 regional reset).
7. **Reauthentication.** Write down the exact human and API steps that restored service after each disconnect in item 6. Include whether `ssodh/init` alone was enough for CP and whether the Gateway recovered without a person after its daily restart.
8. **CP stream expiry.** Confirm the 15-minute `smd` expiry and that resubscribing at 10 minutes has no gap.
9. **Orders.** Place a paper limit BUY for EMAAR at least 20% below the market. Record the full confirmation path (CP reply messages and their `messageIds`, or TWS `openOrder`/`orderStatus` sequence), then cancel and record the time to `Cancelled`.
10. **Positions and executions.** Read positions (`reqPositions`, or `/portfolio2/{acct}/positions`) and executions (`reqExecutions`, or `/iserver/account/trades`) after a small paper fill. Check that the AED amounts and the conid match item 2.
11. **Pacing.** Confirm 50 req/s on TWS and 10 req/s on CP by observing the first error, without triggering the IP penalty box more than once.
12. **Nightly reset with the socket open (TWS only).** Keep an API client connected with EMAAR and two other DFM lines streaming across at least three nightly resets. Log every `error` callback with its UTC time, code and text, especially 1100, 1101, 1102, 2110, 2103 and 2104 [D27], [D28], and whether `connectionClosed` fired. Record whether recovery came as 1101 (resubscribe needed) or 1102 (subscriptions kept), how long quotes were stale, whether ticks resumed without resubscribing after 1102, and whether the open-order, position and execution re-requests in reauthentication step 9 returned anything new. Repeat once with a competing login (log the same username in to Client Portal) to see the 1100 cause and, on paper, error 10197 [D7], [D28].
13. **Client ID reuse after a half-open drop (TWS only).** Kill the client process without closing the socket, reconnect at once with the same client ID, and record whether error 326 appears and how long until the reconnect succeeds [D12], [D28]. Use the result to set the backoff in step 7.
14. **Order rejections (TWS only).** On paper, place orders that IBKR should warn about or reject (for example a limit far through the market) with precautions bypassed. Record the `error` codes, texts and `advancedOrderRejectJson`, and any `openOrder` warning text [D29], [D30], so BE-42 can show them verbatim.
15. **Licence review (not a measurement).** Before any distribution of binaries or container images built with `scmhub/ibapi`, review the GPL (TWS API 10.49+) and Non-Commercial (earlier) terms against the library's MIT claim [D8], [L1], [G1], and decide whether DFM needs a LICENSE file now that its source is public.

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
- [D27] System Message Codes (1100, 1101, 1102, "at least once a day", execution reports delayed during a reset): https://www.interactivebrokers.com/docs/tws-api/doc/error-handling/system-message-codes
- [D28] Error Codes (101, 201, 326, 399, 2103, 2104, 2110, 10197): https://www.interactivebrokers.com/docs/tws-api/doc/error-handling/error-codes
- [D29] Receiving Error Messages (`EWrapper.error` with `advancedOrderRejectJson`) and Error Handling Introduction (notifications as well as true errors): https://www.interactivebrokers.com/docs/tws-api/doc/error-handling/receiving-error-messages and https://www.interactivebrokers.com/docs/tws-api/doc/error-handling/introduction
- [D30] Order Placement Considerations (201, 202, `openOrder` warning text): https://www.interactivebrokers.com/docs/tws-api/doc/orders/place-order/order-placement-considerations
- [D31] Requesting Currently Active Orders (`reqOpenOrders`, `reqAllOpenOrders`, orders bound to their client ID): https://www.interactivebrokers.com/docs/tws-api/doc/order-management/requesting-currently-active-orders/introduction, https://www.interactivebrokers.com/docs/tws-api/doc/order-management/requesting-currently-active-orders/api-clients-orders and https://www.interactivebrokers.com/docs/tws-api/doc/order-management/requesting-currently-active-orders/all-submitted-orders
- [D32] Modifying Orders: https://www.interactivebrokers.com/docs/tws-api/doc/orders/modifying-orders
- [D33] Account Summary (`reqAccountSummary`, three-minute updates, two subscriptions, tags): https://www.interactivebrokers.com/docs/tws-api/doc/account-portfolio-data/account-summary/introduction, https://www.interactivebrokers.com/docs/tws-api/doc/account-portfolio-data/account-summary/requesting-account-summary and https://www.interactivebrokers.com/docs/tws-api/doc/account-portfolio-data/account-summary/account-summary-tags
- [L1] IBKR Campus, Introduction to the TWS Excel API: Initial Setup ("Our APIs are provided with a Non-Commercial license agreement"): https://www.interactivebrokers.com/campus/trading-lessons/introduction-to-the-tws-excel-api-initial-setup/
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

Dubai Financial Market:

- [X1] DFM, How to Trade, "DFM Trading Hours" (pre-opening 09:30 to 10:00, trading 10:00 to 14:45, pre-closing 14:45 to 14:55, trading at last 14:55 to 15:00): https://www.dfm.ae/investors/how-to-trade

GitHub (repository metadata read through the GitHub API on 2026-10-14, and again on 2026-10-15 for the licence note):

- [G1] https://github.com/scmhub/ibapi (README, `LICENSE` (MIT), `go.mod`, releases, and the `v0.10.48...v0.10.49` compare that adds `ContractDetails.SettlementMethod`), https://github.com/scmhub/ibsync, https://github.com/hadrianl/ibapi, https://github.com/gofinance/ib
- [G2] https://github.com/tomlister/ibclient
- [G3] https://github.com/IbcAlpha/IBC
