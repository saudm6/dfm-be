# WebSocket protocol

The server pushes live data to the browser over one WebSocket per tab. This
document is the contract; [dfm-be#7](https://github.com/saudm6/dfm-be/issues/7)
implements it. The HTTP contract is [openapi.yaml](openapi.yaml), and payload
schemas named below (`Quote`, `Error`, ...) are the schemas defined there.

## Connection

- Endpoint: `GET /ws`, upgraded to a WebSocket.
- Authentication: the session cookie from
  [dfm-be#5](https://github.com/saudm6/dfm-be/issues/5). Without a valid
  session the upgrade is refused with `401` and the `Error` body.
- Every frame is a text frame holding one JSON object.
- The server pings every 20 seconds and closes the connection after two
  missed pongs. Browsers answer pings automatically.
- A connection starts with no subscriptions.
- Decimals in payloads use the HTTP wire form: strings without trailing
  zeros ("11.4", "0").

## Envelope

Every message has a `type`. The other fields depend on it.

### Client to server

Subscribe to one or more topics. Subscribing to a topic the connection already
has is a no-op.

```json
{"type": "subscribe", "topics": ["quote:EMAAR", "market.summary"]}
```

Unsubscribe. After the server reads this message it sends no more `data` for
those topics.

```json
{"type": "unsubscribe", "topics": ["quote:EMAAR"]}
```

| Field    | Type     | Notes                                |
|----------|----------|--------------------------------------|
| `type`   | string   | `subscribe` or `unsubscribe`         |
| `topics` | string[] | One or more topic names, see below.  |

### Server to client

After a `subscribe`, the first message on each newly subscribed topic is a
`snapshot` with the topic's current state. `data` messages follow, one per
update:

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
| `seq`   | integer | Per (connection, topic): the snapshot has `seq` 1 and each later message adds 1. Unsubscribing and subscribing again starts a new sequence with a new snapshot at 1. A gap is handled as described under Slow clients. |
| `data`  | object  | The topic's payload, see the topic table. A `snapshot` and a `data` message on the same topic carry the same payload type. |

A refused subscription or a bad message:

```json
{"type": "error", "topic": "quote:EMAAR", "code": "forbidden"}
```

| Field     | Type   | Notes |
|-----------|--------|-------|
| `type`    | string | `error` |
| `topic`   | string | The topic concerned. Omitted when the error is not about one topic, for example a message that is not valid JSON. |
| `code`    | string | Machine-readable reason, from the same snake_case vocabulary as the HTTP `Error.code`: `forbidden` (the user's role may not read the topic), `unknown_topic`, `bad_request`. |
| `message` | string | Optional human-readable detail. |

An `error` does not close the connection.

## Slow clients

Each connection has a bounded send queue. What happens when it fills depends
on the topic's delivery mode in the topic table:

- `latest` topics carry state, where a newer value replaces an older one
  (quotes, summaries, positions, source status). The server keeps only the
  latest value per key: the topic itself for `quote:{symbol}`, or the key
  named by the topic's feature PR. Each message is the full current value
  for its key, so a client that sees a `seq` gap needs to do nothing.
- `every` topics carry events, where each message matters (chat, alerts,
  disclosures, orders). The server never coalesces them. If the queue
  overflows, the events that do not fit are dropped and the next delivered
  message shows a `seq` gap. A client that sees a gap on an `every` topic
  must refetch the topic's HTTP snapshot endpoint, named in the topic's
  feature PR, and then apply only messages with a higher `seq`.

## Topics

A topic is a name, optionally followed by `:` and a key: `quote:EMAAR`,
`chat:desk-1`, `market.summary`. Split a topic at its first `:`. Everything
before it is the name, which never contains `:`; everything after it is the
key, which may contain `.`, spaces, and any other character. Symbols contain
`.` (and spaces), so `.` is never a separator.

`{symbol}` is a `Symbol` as defined in openapi.yaml, for example `EMAAR` or
`NBD GULF` (topic `quote:NBD GULF`). Each topic is added by the feature PR
that publishes it; that PR fills in the payload column, links its schema in
openapi.yaml, and names its HTTP snapshot endpoint.

| Topic             | Payload (`data`)          | Delivery | Added by |
|-------------------|---------------------------|----------|----------|
| `quote:{symbol}`  | `Quote`                   | latest   | F02 Live quotes |
| `market.summary`  | Defined by its feature PR | latest   | F03 Movers and heat map |
| `movers`          | Defined by its feature PR | latest   | F03 Movers and heat map |
| `disclosures`     | Defined by its feature PR | every    | F04 News and disclosures |
| `alerts`          | Defined by its feature PR | every    | F05 Alerts |
| `orders`          | Defined by its feature PR | every    | F10 Order entry |
| `positions`       | Defined by its feature PR | latest   | F11 Positions and live P&L |
| `status`          | Defined by its feature PR | latest   | F00 Source health ([dfm-be#9](https://github.com/saudm6/dfm-be/issues/9)) |
| `chat:{room}`     | Defined by its feature PR | every    | F12 Chat |

Topic access is checked with the role check from
[dfm-be#5](https://github.com/saudm6/dfm-be/issues/5). A subscription the
user's role does not allow gets an `error` with code `forbidden`.
