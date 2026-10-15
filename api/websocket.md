# WebSocket protocol

The server pushes live data to the browser over one WebSocket per tab. This
document is the contract; [dfm-be#7](https://github.com/saudm6/dfm-be/issues/7)
implements it. The HTTP contract is [openapi.yaml](openapi.yaml), and payload
schemas named below (`Quote`, `Error`, ...) are the schemas defined there.

## Connection

- Endpoint: `GET /ws`, upgraded to a WebSocket.
- Origin: a request whose `Origin` header names another host than the one
  it was sent to is refused with `403` and the `Error` body, before the
  session is looked at. The session cookie is `SameSite=Strict`, so
  browsers do not send it on cross-site upgrades. A same-site page on
  another origin (another port or subdomain of the same site) does get the
  cookie; the Origin check is what stops it from opening a socket as the
  user. Requests without `Origin` (non-browser clients) pass.
- Proxies: a reverse proxy in front of `/ws`, such as the Angular dev
  server's proxy, must pass the browser's `Host` header through unchanged.
  With `changeOrigin: true` the proxy rewrites `Host` to the backend's, the
  browser's `Origin` no longer matches it, and the upgrade gets `403`.
- Authentication: the session cookie from
  [dfm-be#5](https://github.com/saudm6/dfm-be/issues/5). Without a valid
  session the upgrade is refused with `401` and the `Error` body. A `GET /ws`
  that is not a WebSocket upgrade gets `426` with code `upgrade_required`.
- Every frame is a text frame holding one JSON object.
- The server pings every 20 seconds. A ping not answered within 20 seconds
  is missed; after two missed pings in a row the server closes the
  connection with status 1008. Browsers answer pings automatically.
- A client that does not accept a message within 10 seconds is
  disconnected without a close frame; the browser reports status 1006.
- A client message larger than 32 KiB closes the connection with status
  1009.
- When the server shuts down it closes connections with status 1001; the
  client reconnects and subscribes again.
- A connection starts with no subscriptions and may hold at most 256.
- Decimals in payloads use the HTTP wire form: strings without trailing
  zeros ("11.4", "0").

### Session checks

Every 20 seconds, the ping interval, each socket looks its session up again
under the same idle and absolute expiry rules as an HTTP request
([dfm-be#5](https://github.com/saudm6/dfm-be/issues/5)).

- A successful check counts as activity: like an HTTP request, it extends
  the session's idle expiry, for the socket and for HTTP. An open socket
  therefore keeps its session alive, but never past the session's maximum
  age (7 days by default).
- When the session has been logged out or has expired, the server closes
  the socket with status `4401` and reason `session_ended`.
  `POST /auth/logout` closes all of that session's sockets at once, without
  waiting for the next check. The client treats `4401` as signed out and
  does not reconnect until the user signs in again.
- Each check also reads the user's role again. Topics the role may no
  longer read are unsubscribed, each with an `error` of code `forbidden`;
  the socket and its other topics stay.

### Close statuses

| Status | Meaning |
|--------|---------|
| 1001   | Server shutting down; reconnect. |
| 1006   | No close frame, for example after the 10 second write timeout; reconnect. |
| 1008   | Two pings in a row went unanswered; reconnect. |
| 1009   | A client message exceeded 32 KiB. |
| 4401   | The session ended (`session_ended`); sign in again. |

## Envelope

Every message has a `type`. The other fields depend on it.

### Client to server

Subscribe to one or more topics. Subscribing to a topic the connection already
has is a no-op. Each topic is checked on its own: a refused topic gets an
`error` and the others are subscribed.

```json
{"type": "subscribe", "topics": ["quote:EMAAR", "market.summary"]}
```

Unsubscribe. After the server reads this message it queues no more
messages for those topics. A message it had already taken from the queue to
send may still arrive after the unsubscribe, so the client ignores messages
for topics it no longer holds.

```json
{"type": "unsubscribe", "topics": ["quote:EMAAR"]}
```

| Field    | Type     | Notes                                |
|----------|----------|--------------------------------------|
| `type`   | string   | `subscribe` or `unsubscribe`         |
| `topics` | string[] | One or more topic names, see below.  |

### Server to client

After a `subscribe`, the first message on each newly subscribed topic is a
`snapshot`. `data` messages follow, one per update. What the snapshot holds
depends on the topic's delivery mode (see Slow clients):

- `latest` topics: the last value published on the topic, or `null` when
  nothing has been published since the server started.
- `every` topics: always `null`. The snapshot only marks the start of the
  sequence; the topic's history comes from its HTTP snapshot endpoint (see
  Recovering from a gap).

```json
{"type": "snapshot", "topic": "quote:EMAAR", "seq": 1, "data": {"symbol": "EMAAR", "last": "11.4", "bid": "11.35", "ask": "11.4", "bidSize": 1000, "askSize": 2500, "volume": 5000000, "trades": 1210, "value": "56500000.5", "prevClose": "11.3", "lastTradeAt": "2026-10-15T10:00:00+04:00", "asOf": "2026-10-15T10:00:01+04:00", "source": "ibkr", "freshness": "realtime"}}
```

```json
{"type": "data", "topic": "quote:EMAAR", "seq": 2, "data": {"symbol": "EMAAR", "last": "11.45", "bid": "11.4", "ask": "11.45", "bidSize": 3000, "askSize": 800, "volume": 5012000, "trades": 1214, "value": "56637200.5", "prevClose": "11.3", "lastTradeAt": "2026-10-15T10:00:04+04:00", "asOf": "2026-10-15T10:00:04+04:00", "source": "ibkr", "freshness": "realtime"}}
```

| Field   | Type    | Notes |
|---------|---------|-------|
| `type`  | string  | `snapshot` or `data` |
| `topic` | string  | The topic this message belongs to. |
| `seq`   | integer | Per (connection, topic): the snapshot has `seq` 1 and each later update adds 1, whether or not its message is delivered. Unsubscribing and subscribing again starts a new sequence with a new snapshot at 1. A gap is handled as described under Slow clients. |
| `data`  | object or null | The topic's payload, see the topic table. A `data` message and a non-null `snapshot` on the same topic carry the same payload type. |

The server warns that messages on a topic may have been lost:

```json
{"type": "resync", "topic": "orders"}
```

It sends `resync` when its own feed for the topic lost events (the internal
event bus overflowed) and when an `every` event did not fit the
connection's send queue. It sends it at once, without waiting for the next
event, and at most one is queued per topic at a time. The client recovers
as described under Recovering from a gap.

A refused subscription or a bad message:

```json
{"type": "error", "topic": "quote:EMAAR", "code": "forbidden"}
```

| Field     | Type   | Notes |
|-----------|--------|-------|
| `type`    | string | `error` |
| `topic`   | string | The topic concerned. Omitted when the error is not about one topic, for example a message that is not valid JSON. |
| `code`    | string | Machine-readable reason, from the same snake_case vocabulary as the HTTP `Error.code`: `forbidden` (the user's role, or the topic's per-key check, does not allow the topic; see Topics and Session checks), `unknown_topic` (no such name, a missing or unexpected key, or a key that is not valid, such as a symbol that is not upper case), `bad_request` (the message is not a JSON object, has an unknown `type` or no `topics`, or would exceed the subscription limit). |
| `message` | string | Optional human-readable detail. |

An `error` does not close the connection. When the connection's send queue
(see Slow clients) is full, an `error` is dropped rather than queued.

## Slow clients

Each connection has a bounded send queue (256 messages). While it has room
every update is sent. What happens when it is full depends on the topic's
delivery mode in the topic table:

- `latest` topics carry state, where a newer value replaces an older one
  (quotes, summaries, positions, source status). The server keeps only the
  latest unsent value per topic: a new value overwrites the topic's last
  queued one. Each message is the full current value of its topic (for
  `positions`, all positions), so a client that sees a `seq` gap needs to
  do nothing.
- `every` topics carry events, where each message matters (chat, alerts,
  disclosures, orders). The server never coalesces them. Events that do
  not fit are dropped, a `resync` is queued, and the next delivered event
  shows a `seq` gap.

### Recovering from a gap

On a `seq` gap or a `resync` on an `every` topic, and on a `resync` on a
`latest` topic, the client:

1. Sends `unsubscribe` and then `subscribe` for the topic. The server
   answers with a fresh `snapshot` at `seq` 1; the client keeps the `data`
   messages that follow it.
2. Refetches the topic's HTTP snapshot endpoint, named in the topic's
   feature PR.
3. Applies the kept and later `data` messages on top of the HTTP result,
   skipping events it already has. Every `every` topic's payload has an
   `id` unique within the topic for this purpose.

A `latest` topic needs no deduplication: a `data` message is the full
value and replaces whatever the HTTP result said.

## Topics

A topic is a name, optionally followed by `:` and a key of 1 to 64 bytes: `quote:EMAAR`,
`chat:desk-1`, `market.summary`. Split a topic at its first `:`. Everything
before it is the name, which never contains `:`; everything after it is the
key, which may contain `.`, spaces, and any other character. Symbols contain
`.` (and spaces), so `.` is never a separator.

`{symbol}` is a `Symbol` as defined in openapi.yaml, for example `EMAAR` or
`NBD GULF` (topic `quote:NBD GULF`). Each topic is fed by the feature PR
that publishes it; that PR fills in the payload column, links its schema in
openapi.yaml, and names its HTTP snapshot endpoint. The gateway
(`internal/ws`) already accepts subscriptions to every topic below; until
its feature PR publishes, a topic only sends its snapshot.

| Topic             | Payload (`data`)          | Delivery | Access     | Added by |
|-------------------|---------------------------|----------|------------|----------|
| `quote:{symbol}`  | `Quote`                   | latest   | marketData | F02 Live quotes |
| `market.summary`  | Defined by its feature PR | latest   | marketData | F03 Movers and heat map |
| `movers`          | Defined by its feature PR | latest   | marketData | F03 Movers and heat map |
| `disclosures`     | Defined by its feature PR | every    | marketData | F04 News and disclosures |
| `alerts`          | Defined by its feature PR | every    | marketData | F05 Alerts |
| `orders`          | Defined by its feature PR | every    | marketData | F10 Order entry |
| `positions`       | Defined by its feature PR | latest   | marketData | F11 Positions and live P&L |
| `status`          | Defined by its feature PR | latest   | any user   | F00 Source health ([dfm-be#9](https://github.com/saudm6/dfm-be/issues/9)) |
| `chat:{room}`     | Defined by its feature PR | every    | any user   | F12 Chat |

Topic access is checked with the role check from
[dfm-be#5](https://github.com/saudm6/dfm-be/issues/5): `marketData` topics
need a role with market data (`owner`), as the HTTP `marketData` scope
does. A topic may also have a per-key access check (`ws.Topic.Allow`),
run after the role check. Both run on each `subscribe` and again on each
session check. A subscription the user may not read gets an `error` with
code `forbidden`. Today any signed-in user may read any `chat:{room}`;
[dfm-be#48](https://github.com/saudm6/dfm-be/issues/48) restricts chat rooms
and direct messages through the per-key check.
