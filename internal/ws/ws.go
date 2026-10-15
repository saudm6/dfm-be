// Package ws is the WebSocket gateway. It pushes live data to browsers over
// one socket per tab, speaking the protocol in api/websocket.md.
//
// Publishers call Gateway.Publish with a topic and its payload, or bridge a
// bus topic with Forward. Each connection has its own send queue and writer,
// so a slow client never slows a publisher or another client.
package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/saudm6/dfm-be/internal/auth"
)

// Options tunes a Gateway. Zero fields take the defaults.
type Options struct {
	// PingInterval is how often the server pings each client. A client that
	// misses two pongs in a row is disconnected. Default 20s.
	PingInterval time.Duration
	// SendQueue caps the data and error messages queued for one
	// connection. When it is full, a Latest topic's new value replaces its
	// last queued one, and an Every topic's event is dropped and a resync
	// queued. Default 256.
	SendQueue int
	// MaxTopics caps the subscriptions of one connection. Default 256.
	MaxTopics int
	// WriteTimeout closes a connection whose client takes longer than this
	// to accept one message. Default 10s.
	WriteTimeout time.Duration
}

func (o Options) withDefaults() Options {
	if o.PingInterval <= 0 {
		o.PingInterval = 20 * time.Second
	}
	if o.SendQueue <= 0 {
		o.SendQueue = 256
	}
	if o.MaxTopics <= 0 {
		o.MaxTopics = 256
	}
	if o.WriteTimeout <= 0 {
		o.WriteTimeout = 10 * time.Second
	}
	return o
}

// Gateway serves WebSocket connections and fans published messages out to
// their subscribers. It is safe for concurrent use.
type Gateway struct {
	logger *slog.Logger
	topics map[string]Topic
	opts   Options

	// mu guards the fields below. It is taken before any conn.mu.
	mu     sync.Mutex
	conns  map[*conn]struct{}
	subs   map[string]map[*conn]*subscription
	latest map[string]json.RawMessage
	closed bool
	wg     sync.WaitGroup
}

// New returns a Gateway serving topics.
func New(logger *slog.Logger, topics []Topic, opts Options) *Gateway {
	g := &Gateway{
		logger: logger,
		topics: make(map[string]Topic, len(topics)),
		opts:   opts.withDefaults(),
		conns:  map[*conn]struct{}{},
		subs:   map[string]map[*conn]*subscription{},
		latest: map[string]json.RawMessage{},
	}
	for _, t := range topics {
		g.topics[t.Name] = t
	}
	return g
}

// Publish sends data, marshalled to JSON, to every subscriber of topic, a
// full topic name such as "quote:EMAAR". On a Latest topic it also becomes
// the snapshot that new subscribers get.
func (g *Gateway) Publish(topic string, data any) error {
	t, err := lookup(g.topics, topic)
	if err != nil {
		return fmt.Errorf("ws: publish: %w", err)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("ws: publish %s: %w", topic, err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if t.Delivery == Latest {
		g.latest[topic] = raw
	}
	for c, sub := range g.subs[topic] {
		c.push(sub, raw)
	}
	return nil
}

// Resync tells every subscriber of the topic called name, keyed or not,
// that messages may have been lost upstream, for example because a bus
// subscription dropped events. Clients then recover as api/websocket.md
// describes.
func (g *Gateway) Resync(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for c := range g.conns {
		c.resyncAll(name)
	}
}

// Close closes every connection with status 1001 (going away), waits for
// them to finish, and refuses later connections.
func (g *Gateway) Close() {
	g.mu.Lock()
	g.closed = true
	conns := make([]*conn, 0, len(g.conns))
	for c := range g.conns {
		conns = append(conns, c)
	}
	g.mu.Unlock()
	for _, c := range conns {
		go func() { _ = c.ws.Close(websocket.StatusGoingAway, "server shutting down") }()
	}
	g.wg.Wait()
}

// Serve upgrades the request to a WebSocket and serves it for sess until
// the connection ends. The caller has authenticated the request and checked
// its Origin.
func (g *Gateway) Serve(w http.ResponseWriter, r *http.Request, sess auth.Session) {
	wsc, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept has answered the request.
	}
	ctx, cancel := context.WithCancel(auth.WithSession(r.Context(), sess))
	defer cancel()
	c := &conn{
		g:    g,
		ws:   wsc,
		ctx:  ctx,
		subs: map[string]*subscription{},
		wake: make(chan struct{}, 1),
	}

	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		_ = wsc.Close(websocket.StatusGoingAway, "server shutting down")
		return
	}
	g.conns[c] = struct{}{}
	g.wg.Add(1)
	g.mu.Unlock()
	defer g.wg.Done()
	defer g.remove(c)

	var done sync.WaitGroup
	done.Go(func() { c.writeLoop(cancel) })
	done.Go(func() { c.pingLoop(cancel) })
	c.readLoop()
	cancel()
	_ = wsc.CloseNow()
	done.Wait()
}

func (g *Gateway) remove(c *conn) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.conns, c)
	c.mu.Lock()
	defer c.mu.Unlock()
	for topic := range c.subs {
		g.unsubscribeLocked(c, topic)
	}
}

func (g *Gateway) unsubscribeLocked(c *conn, topic string) {
	delete(c.subs, topic)
	if m := g.subs[topic]; m != nil {
		delete(m, c)
		if len(m) == 0 {
			delete(g.subs, topic)
		}
	}
}

// Error codes of error messages; see api/websocket.md.
const (
	codeBadRequest   = "bad_request"
	codeForbidden    = "forbidden"
	codeUnknownTopic = "unknown_topic"
)

// clientMessage is a message from the client.
type clientMessage struct {
	Type   string   `json:"type"`
	Topics []string `json:"topics"`
}

// message is a message to the client.
type message struct {
	Type    string          `json:"type"`
	Topic   string          `json:"topic,omitempty"`
	Seq     int64           `json:"seq,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Code    string          `json:"code,omitempty"`
	Message string          `json:"message,omitempty"`
}

var null = json.RawMessage("null")

// subscription is one topic of one connection.
type subscription struct {
	topic string
	t     Topic
	seq   int64
	// pending is the last queued, unsent data message of a Latest topic.
	// When the send queue is full, newer values overwrite it.
	pending *frame
	// resyncQueued is set while a resync message for the topic is queued.
	resyncQueued bool
}

// frame is a queued message. sub is nil for messages about no subscription.
type frame struct {
	sub *subscription
	msg message
	// bounded frames count against Options.SendQueue.
	bounded bool
}

// conn is one client connection.
type conn struct {
	g   *Gateway
	ws  *websocket.Conn
	ctx context.Context // carries the session

	// mu guards the fields below.
	mu      sync.Mutex
	subs    map[string]*subscription
	queue   []*frame
	bounded int
	wake    chan struct{}
}

func (c *conn) readLoop() {
	for {
		typ, b, err := c.ws.Read(c.ctx)
		if err != nil {
			return
		}
		var m clientMessage
		switch {
		case typ != websocket.MessageText:
			c.sendError("", codeBadRequest, "send text messages")
			continue
		case json.Unmarshal(b, &m) != nil:
			c.sendError("", codeBadRequest, "message is not a JSON object")
			continue
		case len(m.Topics) == 0:
			c.sendError("", codeBadRequest, "topics must list at least one topic")
			continue
		}
		switch m.Type {
		case "subscribe":
			for _, topic := range m.Topics {
				c.subscribe(topic)
			}
		case "unsubscribe":
			for _, topic := range m.Topics {
				c.unsubscribe(topic)
			}
		default:
			c.sendError("", codeBadRequest, fmt.Sprintf("unknown message type %q", m.Type))
		}
	}
}

func (c *conn) subscribe(topic string) {
	g := c.g
	t, err := lookup(g.topics, topic)
	if err != nil {
		c.sendError(topic, codeUnknownTopic, err.Error())
		return
	}
	if t.Access == MarketData {
		if err := auth.RequireMarketData(c.ctx); err != nil {
			c.sendError(topic, codeForbidden, "your role may not read "+t.Name)
			return
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.subs[topic]; ok {
		return
	}
	if len(c.subs) >= g.opts.MaxTopics {
		c.enqueueError(topic, codeBadRequest, fmt.Sprintf("a connection may subscribe to at most %d topics", g.opts.MaxTopics))
		return
	}
	sub := &subscription{topic: topic, t: t, seq: 1}
	c.subs[topic] = sub
	if g.subs[topic] == nil {
		g.subs[topic] = map[*conn]*subscription{}
	}
	g.subs[topic][c] = sub
	data := null
	if t.Delivery == Latest && g.latest[topic] != nil {
		data = g.latest[topic]
	}
	c.enqueue(&frame{sub: sub, msg: message{Type: "snapshot", Topic: topic, Seq: 1, Data: data}})
}

func (c *conn) unsubscribe(topic string) {
	g := c.g
	g.mu.Lock()
	defer g.mu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	sub, ok := c.subs[topic]
	if !ok {
		return
	}
	g.unsubscribeLocked(c, topic)
	kept := c.queue[:0]
	for _, f := range c.queue {
		if f.sub == sub {
			if f.bounded {
				c.bounded--
			}
			continue
		}
		kept = append(kept, f)
	}
	clear(c.queue[len(kept):])
	c.queue = kept
}

// push queues raw as the next data message of sub. g.mu is held.
func (c *conn) push(sub *subscription, raw json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	sub.seq++
	f := &frame{sub: sub, msg: message{Type: "data", Topic: sub.topic, Seq: sub.seq, Data: raw}}
	full := c.bounded >= c.g.opts.SendQueue
	switch {
	case !full:
		f.bounded = true
	case sub.t.Delivery == Every:
		c.queueResync(sub)
		return
	case sub.pending != nil:
		sub.pending.msg.Seq = sub.seq
		sub.pending.msg.Data = raw
		return
	}
	// A Latest topic with nothing queued gets its value through even when
	// the queue is full; there is at most one such message per topic.
	if sub.t.Delivery == Latest {
		sub.pending = f
	}
	c.enqueue(f)
}

// resyncAll queues a resync message on every subscription to the topic
// called name. g.mu is held.
func (c *conn) resyncAll(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, sub := range c.subs {
		if sub.t.Name == name {
			c.queueResync(sub)
		}
	}
}

func (c *conn) queueResync(sub *subscription) {
	if sub.resyncQueued {
		return
	}
	sub.resyncQueued = true
	c.enqueue(&frame{sub: sub, msg: message{Type: "resync", Topic: sub.topic}})
}

func (c *conn) sendError(topic, code, msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enqueueError(topic, code, msg)
}

func (c *conn) enqueueError(topic, code, msg string) {
	if c.bounded >= c.g.opts.SendQueue {
		return
	}
	c.enqueue(&frame{msg: message{Type: "error", Topic: topic, Code: code, Message: msg}, bounded: true})
}

// enqueue appends f to the queue and wakes the writer. c.mu is held.
func (c *conn) enqueue(f *frame) {
	if f.bounded {
		c.bounded++
	}
	c.queue = append(c.queue, f)
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// pop removes the first queued message and returns it encoded.
func (c *conn) pop() ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.queue) == 0 {
		return nil, false
	}
	f := c.queue[0]
	c.queue[0] = nil
	c.queue = c.queue[1:]
	if f.bounded {
		c.bounded--
	}
	if f.sub != nil {
		switch {
		case f.sub.pending == f:
			f.sub.pending = nil
		case f.msg.Type == "resync":
			f.sub.resyncQueued = false
		}
	}
	b, err := json.Marshal(f.msg)
	if err != nil {
		panic(err) // message holds only strings, numbers, and valid JSON.
	}
	return b, true
}

func (c *conn) writeLoop(stop context.CancelFunc) {
	defer stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.wake:
		}
		for {
			b, ok := c.pop()
			if !ok {
				break
			}
			ctx, cancel := context.WithTimeout(c.ctx, c.g.opts.WriteTimeout)
			err := c.ws.Write(ctx, websocket.MessageText, b)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// pingLoop pings the client every PingInterval and closes the connection
// when two pings in a row get no pong within the interval.
func (c *conn) pingLoop(stop context.CancelFunc) {
	defer stop()
	interval := c.g.opts.PingInterval
	t := time.NewTicker(interval)
	defer t.Stop()
	missed := 0
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-t.C:
		}
		ctx, cancel := context.WithTimeout(c.ctx, interval)
		err := c.ws.Ping(ctx)
		cancel()
		switch {
		case err == nil:
			missed = 0
			continue
		case c.ctx.Err() != nil:
			return
		case !errors.Is(err, context.DeadlineExceeded):
			return
		}
		missed++
		if missed >= 2 {
			_ = c.ws.Close(websocket.StatusPolicyViolation, "missed two pongs")
			return
		}
	}
}
