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

## Envelope

Every message has a `type`. The other fields depend on it.

### Client to server

Subscribe to one or more topics. Subscribing to a topic the connection already
has is a no-op.

```json
{"type": "subscribe", "topics": ["quote.EMAAR", "market.summary"]}
```

Unsubscribe. After the server reads this message it sends no more `data` for
those topics.

```json
{"type": "unsubscribe", "topics": ["quote.EMAAR"]}
```

| Field    | Type     | Notes                                |
|----------|----------|--------------------------------------|
| `type`   | string   | `subscribe` or `unsubscribe`         |
| `topics` | string[] | One or more topic names, see below.  |

### Server to client

An update on a subscribed topic:

```json
{"type": "data", "topic": "quote.EMAAR", "seq": 1042, "data": {"last": "11.40", "freshness": "realtime"}}
```

| Field   | Type    | Notes |
|---------|---------|-------|
| `type`  | string  | `data` |
| `topic` | string  | The topic this update belongs to. |
| `seq`   | integer | Per topic, strictly increasing on this connection. A gap means the server coalesced updates for a slow client (below). |
| `data`  | object  | The topic's payload, see the topic table. |

A refused subscription or a bad message:

```json
{"type": "error", "topic": "quote.EMAAR", "code": "forbidden"}
```

| Field     | Type   | Notes |
|-----------|--------|-------|
| `type`    | string | `error` |
| `topic`   | string | The topic concerned. Omitted when the error is not about one topic, for example a message that is not valid JSON. |
| `code`    | string | Machine-readable reason, from the same snake_case vocabulary as the HTTP `Error.code`: `forbidden` (the user's role may not read the topic), `unknown_topic`, `bad_request`. |
| `message` | string | Optional human-readable detail. |

An `error` does not close the connection.

## Slow clients

Each connection has a bounded send queue. When it fills, the server stops
sending every update and sends only the latest value per topic. Clients must
treat each `data` message as the current value for its topic, and must not
assume they saw every intermediate update. `seq` shows where updates were
skipped.

## Topics

`{symbol}` is a `Symbol` as defined in openapi.yaml, for example `EMAAR` or
`NBD GULF`. Each topic is added by the feature PR that publishes it; that PR
fills in the payload column and links its schema in openapi.yaml.

| Topic             | Payload (`data`)                                 | Added by |
|-------------------|--------------------------------------------------|----------|
| `quote.{symbol}`  | `Quote`                                          | F02 Live quotes |
| `market.summary`  | Defined by its feature PR                        | F03 Movers and heat map |
| `movers`          | Defined by its feature PR                        | F03 Movers and heat map |
| `disclosures`     | Defined by its feature PR                        | F04 News and disclosures |
| `alerts`          | Defined by its feature PR                        | F05 Alerts |
| `orders`          | Defined by its feature PR                        | F10 Order entry |
| `positions`       | Defined by its feature PR                        | F11 Positions and live P&L |
| `status`          | Defined by its feature PR                        | F00 Source health ([dfm-be#9](https://github.com/saudm6/dfm-be/issues/9)) |
| `chat.{room}`     | Defined by its feature PR                        | F12 Chat |

Topic access is checked with the role check from
[dfm-be#5](https://github.com/saudm6/dfm-be/issues/5). A subscription the
user's role does not allow gets an `error` with code `forbidden`.
