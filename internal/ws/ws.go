// Package ws is the WebSocket gateway. It pushes live data to browsers over
// one socket per tab, speaking the protocol in api/websocket.md.
//
// Publishers call Gateway.Publish with a topic and its payload, or bridge a
// bus topic with Forward. Each connection has its own send queue and writer,
// so a slow client never slows a publisher or another client.
package ws

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
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
	// RecheckInterval is how often each connection revalidates its session
	// (Sessions.Revalidate). Default 20s, the ping interval.
	RecheckInterval time.Duration
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
	if o.RecheckInterval <= 0 {
		o.RecheckInterval = 20 * time.Second
	}
	return o
}

// StatusSessionEnded is the close status of a connection whose session was
// logged out or expired.
const StatusSessionEnded websocket.StatusCode = 4401

// Sessions revalidates the session of an open connection. *auth.Service
// implements it.
type Sessions interface {
	// Revalidate returns sess with its user's current role, counting the
	// call as activity, or an error wrapping auth.ErrUnauthenticated when
	// the session has ended.
	Revalidate(ctx context.Context, sess auth.Session) (auth.Session, error)
}

// Gateway serves WebSocket connections and fans published messages out to
// their subscribers. It is safe for concurrent use.
type Gateway struct {
	logger   *slog.Logger
	topics   map[string]Topic
	sessions Sessions
	opts     Options

	// mu guards the fields below. It is taken before any conn.mu.
	mu     sync.Mutex
	conns  map[*conn]struct{}
	subs   map[string]map[*conn]*subscription
	latest map[string]json.RawMessage
	// bySession holds the connections of each session, by auth.Session.ID.
	bySession map[string]map[*conn]struct{}
	closed    bool
	wg        sync.WaitGroup
}

// New returns a Gateway serving topics. Each connection revalidates its
// session with sessions every Options.RecheckInterval.
func New(logger *slog.Logger, topics []Topic, sessions Sessions, opts Options) *Gateway {
	g := &Gateway{
		logger:    logger,
		topics:    make(map[string]Topic, len(topics)),
		sessions:  sessions,
		opts:      opts.withDefaults(),
		conns:     map[*conn]struct{}{},
		subs:      map[string]map[*conn]*subscription{},
		latest:    map[string]json.RawMessage{},
		bySession: map[string]map[*conn]struct{}{},
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
	t, _, err := lookup(g.topics, topic)
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

// EndSession closes every connection of sess with StatusSessionEnded,
// without waiting for the closing handshakes. Logout calls it. It does
// nothing on a nil Gateway.
func (g *Gateway) EndSession(sess auth.Session) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for c := range g.bySession[sess.ID()] {
		go c.end()
	}
}

// Serve upgrades the request to a WebSocket and serves it for sess until
// the connection ends. The caller has authenticated the request and checked
// its Origin.
func (g *Gateway) Serve(w http.ResponseWriter, r *http.Request, sess auth.Session) {
	wsc, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept has answered the request.
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	c := &conn{
		g:    g,
		ws:   wsc,
		ctx:  ctx,
		id:   sess.ID(),
		sess: sess,
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
	if g.bySession[c.id] == nil {
		g.bySession[c.id] = map[*conn]struct{}{}
	}
	g.bySession[c.id][c] = struct{}{}
	g.wg.Add(1)
	g.mu.Unlock()
	defer g.wg.Done()
	defer g.remove(c)

	var done sync.WaitGroup
	done.Go(func() { c.writeLoop(cancel) })
	done.Go(func() { c.pingLoop(cancel) })
	done.Go(func() { c.recheckLoop(cancel) })
	c.readLoop()
	cancel()
	_ = wsc.CloseNow()
	done.Wait()
}

func (g *Gateway) remove(c *conn) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.conns, c)
	if m := g.bySession[c.id]; m != nil {
		delete(m, c)
		if len(m) == 0 {
			delete(g.bySession, c.id)
		}
	}
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
	key   string
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
	ctx context.Context
	id  string // the session's ID

	// mu guards the fields below.
	mu sync.Mutex
	// sess is the session as last revalidated; gen counts its changes.
	sess    auth.Session
	gen     uint64
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
	t, key, err := lookup(c.g.topics, topic)
	if err != nil {
		c.sendError(topic, codeUnknownTopic, err.Error())
		return
	}
	for {
		sess, gen := c.session()
		if reason := denied(t, sess, key); reason != "" {
			c.sendError(topic, codeForbidden, reason)
			return
		}
		if c.add(topic, t, key, gen) {
			return
		}
	}
}

// add subscribes to topic and queues its snapshot. It does nothing and
// reports false when the session has changed since generation gen, so
// that access is checked again against the new session.
func (c *conn) add(topic string, t Topic, key string, gen uint64) bool {
	g := c.g
	g.mu.Lock()
	defer g.mu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen != gen {
		return false
	}
	if _, ok := c.subs[topic]; ok {
		return true
	}
	if len(c.subs) >= g.opts.MaxTopics {
		c.enqueueError(topic, codeBadRequest, fmt.Sprintf("a connection may subscribe to at most %d topics", g.opts.MaxTopics))
		return true
	}
	sub := &subscription{topic: topic, t: t, key: key, seq: 1}
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
	return true
}

// denied returns why sess may not read the topic of t with key, or "".
func denied(t Topic, sess auth.Session, key string) string {
	if t.Access == MarketData && !sess.User.Role.MarketData() {
		return "your role may not read " + t.Name
	}
	if t.Allow != nil {
		if err := t.Allow(sess, key); err != nil {
			return err.Error()
		}
	}
	return ""
}

func (c *conn) session() (auth.Session, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sess, c.gen
}

func (c *conn) unsubscribe(topic string) {
	g := c.g
	g.mu.Lock()
	defer g.mu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if sub, ok := c.subs[topic]; ok {
		c.dropLocked(sub)
	}
}

// dropLocked ends sub and removes its queued messages. g.mu and c.mu are
// held.
func (c *conn) dropLocked(sub *subscription) {
	c.g.unsubscribeLocked(c, sub.topic)
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

// recheckLoop revalidates the session every RecheckInterval. It closes the
// connection with StatusSessionEnded once the session has ended, and drops
// the subscriptions the session may no longer read.
func (c *conn) recheckLoop(stop context.CancelFunc) {
	defer stop()
	t := time.NewTicker(c.g.opts.RecheckInterval)
	defer t.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-t.C:
		}
		old, _ := c.session()
		sess, err := c.g.sessions.Revalidate(c.ctx, old)
		switch {
		case errors.Is(err, auth.ErrUnauthenticated):
			c.end()
			return
		case c.ctx.Err() != nil:
			return
		case err != nil:
			c.g.logger.LogAttrs(c.ctx, slog.LevelError, "ws: revalidate session",
				slog.Int64("user_id", old.User.ID),
				slog.String("error", err.Error()),
			)
			continue
		}
		c.setSession(sess)
	}
}

// setSession makes sess the connection's session and ends, with a
// forbidden error each, the subscriptions it may no longer read.
func (c *conn) setSession(sess auth.Session) {
	c.mu.Lock()
	c.sess = sess
	c.gen++
	subs := make([]*subscription, 0, len(c.subs))
	for _, sub := range c.subs {
		subs = append(subs, sub)
	}
	c.mu.Unlock()
	slices.SortFunc(subs, func(a, b *subscription) int { return cmp.Compare(a.topic, b.topic) })
	for _, sub := range subs {
		if reason := denied(sub.t, sess, sub.key); reason != "" {
			c.revoke(sub, reason)
		}
	}
}

// revoke ends sub, unless it has already ended, and queues a forbidden
// error giving reason.
func (c *conn) revoke(sub *subscription, reason string) {
	c.g.mu.Lock()
	defer c.g.mu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.subs[sub.topic] != sub {
		return
	}
	c.dropLocked(sub)
	c.enqueueError(sub.topic, codeForbidden, reason)
}

// end closes the connection because its session has ended.
func (c *conn) end() {
	_ = c.ws.Close(StatusSessionEnded, "session_ended")
}
