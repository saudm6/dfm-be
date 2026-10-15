package ws_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/saudm6/dfm-be/internal/auth"
	"github.com/saudm6/dfm-be/internal/bus"
	"github.com/saudm6/dfm-be/internal/ws"
)

var (
	owner  = auth.Session{User: auth.User{ID: 1, Username: "saud", Role: auth.Owner}}
	member = auth.Session{User: auth.User{ID: 2, Username: "amal", Role: auth.Member}}
)

// liveSessions revalidates every session unchanged.
type liveSessions struct{}

func (liveSessions) Revalidate(_ context.Context, sess auth.Session) (auth.Session, error) {
	return sess, nil
}

// serve returns a gateway over ws.Topics and the URL of a server that
// serves it as sess, whose session stays valid.
func serve(t *testing.T, sess auth.Session, opts ws.Options) (*ws.Gateway, string) {
	t.Helper()
	return serveWith(t, sess, ws.Topics, liveSessions{}, opts)
}

func serveWith(t *testing.T, sess auth.Session, topics []ws.Topic, sessions ws.Sessions, opts ws.Options) (*ws.Gateway, string) {
	t.Helper()
	g := ws.New(slog.New(slog.DiscardHandler), topics, sessions, opts)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Serve(w, r, sess)
	}))
	t.Cleanup(func() {
		g.Close()
		srv.Close()
	})
	return g, "ws" + strings.TrimPrefix(srv.URL, "http")
}

type msg struct {
	Type    string          `json:"type"`
	Topic   string          `json:"topic"`
	Seq     int64           `json:"seq"`
	Data    json.RawMessage `json:"data"`
	Code    string          `json:"code"`
	Message string          `json:"message"`
}

type client struct {
	t *testing.T
	c *websocket.Conn
}

func dial(t *testing.T, url string) *client {
	t.Helper()
	c, _, err := websocket.Dial(t.Context(), url, nil) //nolint:bodyclose // Dial documents that the body needs no closing.
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadLimit(1 << 20)
	t.Cleanup(func() { _ = c.CloseNow() })
	return &client{t: t, c: c}
}

func (cl *client) send(s string) {
	cl.t.Helper()
	if err := cl.c.Write(cl.t.Context(), websocket.MessageText, []byte(s)); err != nil {
		cl.t.Fatal(err)
	}
}

// read returns the next message as raw JSON.
func (cl *client) read() string {
	cl.t.Helper()
	ctx, cancel := context.WithTimeout(cl.t.Context(), 5*time.Second)
	defer cancel()
	_, b, err := cl.c.Read(ctx)
	if err != nil {
		cl.t.Fatalf("read: %v", err)
	}
	return string(b)
}

func (cl *client) readMsg() msg {
	cl.t.Helper()
	var m msg
	if err := json.Unmarshal([]byte(cl.read()), &m); err != nil {
		cl.t.Fatal(err)
	}
	return m
}

func (cl *client) expect(want ...string) {
	cl.t.Helper()
	for _, w := range want {
		if got := cl.read(); got != w {
			cl.t.Errorf("message = %s\nwant      %s", got, w)
		}
	}
}

func publish(t *testing.T, g *ws.Gateway, topic string, data any) {
	t.Helper()
	if err := g.Publish(topic, data); err != nil {
		t.Fatal(err)
	}
}

func TestSubscribeReceiveUnsubscribe(t *testing.T) {
	g, url := serve(t, owner, ws.Options{})
	cl := dial(t, url)

	cl.send(`{"type":"subscribe","topics":["quote:EMAAR"]}`)
	cl.expect(`{"type":"snapshot","topic":"quote:EMAAR","seq":1,"data":null}`)
	publish(t, g, "quote:EMAAR", map[string]string{"last": "11.4"})
	publish(t, g, "quote:EMAAR", map[string]string{"last": "11.45"})
	publish(t, g, "quote:DEWA", map[string]string{"last": "2.5"})
	cl.expect(
		`{"type":"data","topic":"quote:EMAAR","seq":2,"data":{"last":"11.4"}}`,
		`{"type":"data","topic":"quote:EMAAR","seq":3,"data":{"last":"11.45"}}`,
	)

	cl.send(`{"type":"unsubscribe","topics":["quote:EMAAR"]}`)
	// The server handles messages in order, so once status's snapshot
	// arrives it has handled the unsubscribe.
	cl.send(`{"type":"subscribe","topics":["status"]}`)
	cl.expect(`{"type":"snapshot","topic":"status","seq":1,"data":null}`)
	publish(t, g, "quote:EMAAR", map[string]string{"last": "11.5"})
	publish(t, g, "status", map[string]string{"ibkr": "up"})
	cl.expect(`{"type":"data","topic":"status","seq":2,"data":{"ibkr":"up"}}`)

	// Subscribing again starts a new sequence with the latest value.
	cl.send(`{"type":"subscribe","topics":["quote:EMAAR"]}`)
	cl.expect(`{"type":"snapshot","topic":"quote:EMAAR","seq":1,"data":{"last":"11.5"}}`)
}

func TestSubscribeTwiceIsNoOp(t *testing.T) {
	g, url := serve(t, owner, ws.Options{})
	cl := dial(t, url)

	cl.send(`{"type":"subscribe","topics":["chat:desk-1","chat:desk-1"]}`)
	cl.send(`{"type":"subscribe","topics":["chat:desk-1"]}`)
	cl.expect(`{"type":"snapshot","topic":"chat:desk-1","seq":1,"data":null}`)
	publish(t, g, "chat:desk-1", map[string]string{"id": "m1"})
	cl.expect(`{"type":"data","topic":"chat:desk-1","seq":2,"data":{"id":"m1"}}`)
}

func TestEveryTopicSnapshotIsEmptyMarker(t *testing.T) {
	g, url := serve(t, owner, ws.Options{})
	publish(t, g, "orders", map[string]string{"id": "o1"})
	cl := dial(t, url)

	cl.send(`{"type":"subscribe","topics":["orders"]}`)

	cl.expect(`{"type":"snapshot","topic":"orders","seq":1,"data":null}`)
}

func TestTopicAccess(t *testing.T) {
	tests := []struct {
		sess  auth.Session
		topic string
		want  string
	}{
		{member, "quote:EMAAR", `{"type":"error","topic":"quote:EMAAR","code":"forbidden","message":"your role may not read quote"}`},
		{member, "market.summary", `{"type":"error","topic":"market.summary","code":"forbidden","message":"your role may not read market.summary"}`},
		{member, "movers", `{"type":"error","topic":"movers","code":"forbidden","message":"your role may not read movers"}`},
		{member, "disclosures", `{"type":"error","topic":"disclosures","code":"forbidden","message":"your role may not read disclosures"}`},
		{member, "alerts", `{"type":"error","topic":"alerts","code":"forbidden","message":"your role may not read alerts"}`},
		{member, "orders", `{"type":"error","topic":"orders","code":"forbidden","message":"your role may not read orders"}`},
		{member, "positions", `{"type":"error","topic":"positions","code":"forbidden","message":"your role may not read positions"}`},
		{member, "status", `{"type":"snapshot","topic":"status","seq":1,"data":null}`},
		{member, "chat:desk-1", `{"type":"snapshot","topic":"chat:desk-1","seq":1,"data":null}`},
		{owner, "quote:NBD GULF", `{"type":"snapshot","topic":"quote:NBD GULF","seq":1,"data":null}`},
		{owner, "market.summary", `{"type":"snapshot","topic":"market.summary","seq":1,"data":null}`},
		{owner, "movers", `{"type":"snapshot","topic":"movers","seq":1,"data":null}`},
		{owner, "disclosures", `{"type":"snapshot","topic":"disclosures","seq":1,"data":null}`},
		{owner, "alerts", `{"type":"snapshot","topic":"alerts","seq":1,"data":null}`},
		{owner, "orders", `{"type":"snapshot","topic":"orders","seq":1,"data":null}`},
		{owner, "positions", `{"type":"snapshot","topic":"positions","seq":1,"data":null}`},
		{owner, "status", `{"type":"snapshot","topic":"status","seq":1,"data":null}`},
		{owner, "chat:desk-1", `{"type":"snapshot","topic":"chat:desk-1","seq":1,"data":null}`},
	}
	for _, tt := range tests {
		t.Run(tt.sess.User.Username+" "+tt.topic, func(t *testing.T) {
			_, url := serve(t, tt.sess, ws.Options{})
			cl := dial(t, url)

			cl.send(`{"type":"subscribe","topics":["` + tt.topic + `"]}`)

			cl.expect(tt.want)
		})
	}
}

func TestBadMessages(t *testing.T) {
	tests := []struct {
		name string
		send string
		want string
	}{
		{"not JSON", `subscribe quote:EMAAR`, `{"type":"error","code":"bad_request","message":"message is not a JSON object"}`},
		{"no topics", `{"type":"subscribe","topics":[]}`, `{"type":"error","code":"bad_request","message":"topics must list at least one topic"}`},
		{"unknown type", `{"type":"publish","topics":["status"]}`, `{"type":"error","code":"bad_request","message":"unknown message type \"publish\""}`},
		{"unknown topic", `{"type":"subscribe","topics":["news"]}`, `{"type":"error","topic":"news","code":"unknown_topic","message":"no topic named \"news\""}`},
		{"dot is not a key separator", `{"type":"subscribe","topics":["quote.EMAAR"]}`, `{"type":"error","topic":"quote.EMAAR","code":"unknown_topic","message":"no topic named \"quote.EMAAR\""}`},
		{"keyed topic without key", `{"type":"subscribe","topics":["quote"]}`, `{"type":"error","topic":"quote","code":"unknown_topic","message":"topic quote needs a key: quote:{key}"}`},
		{"empty key", `{"type":"subscribe","topics":["chat:"]}`, `{"type":"error","topic":"chat:","code":"unknown_topic","message":"\"\" is not a valid key for topic chat"}`},
		{"lower-case symbol", `{"type":"subscribe","topics":["quote:emaar"]}`, `{"type":"error","topic":"quote:emaar","code":"unknown_topic","message":"\"emaar\" is not a valid key for topic quote"}`},
		{"key on unkeyed topic", `{"type":"subscribe","topics":["movers:EMAAR"]}`, `{"type":"error","topic":"movers:EMAAR","code":"unknown_topic","message":"topic movers takes no key"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, url := serve(t, owner, ws.Options{})
			cl := dial(t, url)

			cl.send(tt.send)

			cl.expect(tt.want)
			// An error does not close the connection.
			cl.send(`{"type":"subscribe","topics":["status"]}`)
			cl.expect(`{"type":"snapshot","topic":"status","seq":1,"data":null}`)
		})
	}
}

func TestMaxTopics(t *testing.T) {
	_, url := serve(t, owner, ws.Options{MaxTopics: 2})
	cl := dial(t, url)

	cl.send(`{"type":"subscribe","topics":["status","chat:a","chat:b"]}`)

	cl.expect(
		`{"type":"snapshot","topic":"status","seq":1,"data":null}`,
		`{"type":"snapshot","topic":"chat:a","seq":1,"data":null}`,
		`{"type":"error","topic":"chat:b","code":"bad_request","message":"a connection may subscribe to at most 2 topics"}`,
	)
}

func TestPublishUnknownTopic(t *testing.T) {
	g := ws.New(slog.New(slog.DiscardHandler), ws.Topics, liveSessions{}, ws.Options{})

	err := g.Publish("quote", 1)

	if want := `ws: publish: topic quote needs a key: quote:{key}`; err == nil || err.Error() != want {
		t.Errorf("Publish() error = %v, want %q", err, want)
	}
}

// pad makes each published message about 4 KiB, so that a client that does
// not read fills the TCP buffers and the server's send queue backs up.
var pad = strings.Repeat("x", 4<<10)

const burst = 20000 // about 80 MB of messages

func heapAlloc() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// A client that stops reading gets the latest value of a Latest topic, with
// seq gaps where updates were coalesced, and the server's memory stays
// bounded however many updates it publishes meanwhile.
func TestSlowClientGetsCoalescedLatestValues(t *testing.T) {
	g, url := serve(t, owner, ws.Options{SendQueue: 16, WriteTimeout: time.Minute})
	cl := dial(t, url)
	cl.send(`{"type":"subscribe","topics":["quote:EMAAR"]}`)
	cl.expect(`{"type":"snapshot","topic":"quote:EMAAR","seq":1,"data":null}`)

	before := heapAlloc()
	for i := range burst {
		publish(t, g, "quote:EMAAR", map[string]any{"n": i, "pad": pad})
	}
	grown := int64(heapAlloc()) - int64(before)
	publish(t, g, "quote:EMAAR", map[string]any{"n": burst, "pad": pad})

	if grown > 8<<20 {
		t.Errorf("heap grew by %d bytes while %d messages of 4 KiB were published, want under 8 MiB", grown, burst)
	}
	var received int
	last := int64(1)
	for {
		m := cl.readMsg()
		var data struct{ N int }
		if err := json.Unmarshal(m.Data, &data); err != nil {
			t.Fatal(err)
		}
		if m.Type != "data" || m.Seq <= last || m.Seq != int64(data.N)+2 {
			t.Fatalf("got %s seq %d n %d after seq %d, want data with a higher seq of n+2", m.Type, m.Seq, data.N, last)
		}
		last = m.Seq
		received++
		if data.N == burst {
			break
		}
	}
	if received >= burst/2 {
		t.Errorf("received %d of %d updates, want them coalesced to far fewer", received, burst+1)
	}
	t.Logf("received %d of %d updates", received, burst+1)
}

// A client that stops reading loses Every-topic events that do not fit the
// send queue: it sees a resync and a seq gap, and the server's memory stays
// bounded.
func TestSlowClientLosesEventsWithResync(t *testing.T) {
	g, url := serve(t, owner, ws.Options{SendQueue: 16, WriteTimeout: time.Minute})
	cl := dial(t, url)
	cl.send(`{"type":"subscribe","topics":["alerts"]}`)
	cl.expect(`{"type":"snapshot","topic":"alerts","seq":1,"data":null}`)

	before := heapAlloc()
	for i := range burst {
		publish(t, g, "alerts", map[string]any{"id": i, "pad": pad})
	}
	grown := int64(heapAlloc()) - int64(before)

	if grown > 8<<20 {
		t.Errorf("heap grew by %d bytes while %d messages of 4 KiB were published, want under 8 MiB", grown, burst)
	}
	// Events that fit once the queue has drained get through; the first
	// "final" to do so ends the read loop.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
				_ = g.Publish("alerts", map[string]any{"id": "final"})
			}
		}
	}()
	var received, gaps, resyncs int
	last := int64(1)
	for {
		m := cl.readMsg()
		if m.Type == "resync" {
			if m.Topic != "alerts" {
				t.Errorf("resync topic = %q, want alerts", m.Topic)
			}
			resyncs++
			continue
		}
		if m.Type != "data" || m.Seq <= last {
			t.Fatalf("got %s seq %d after seq %d, want data with a higher seq", m.Type, m.Seq, last)
		}
		if m.Seq > last+1 {
			gaps++
		}
		last = m.Seq
		received++
		if strings.Contains(string(m.Data), `"final"`) {
			break
		}
	}
	if resyncs == 0 || gaps == 0 || received >= burst/2 {
		t.Errorf("resyncs, gaps, received = %d, %d, %d, want at least 1, at least 1, far fewer than %d", resyncs, gaps, received, burst)
	}
	t.Logf("received %d of %d events with %d gaps and %d resyncs", received, burst, gaps, resyncs)
}

// The server waits up to one interval for each pong, and a ping's deadline
// falls one interval after it is sent. The interval is long enough that
// the client's half-interval margins hold on a slow, loaded machine.
func TestMissedPongsCloseConnection(t *testing.T) {
	const interval = time.Second
	_, url := serve(t, owner, ws.Options{PingInterval: interval})

	t.Run("one missed pong is tolerated", func(t *testing.T) {
		t.Parallel()
		cl := dial(t, url)
		// The first ping, sent at 1s, goes unanswered past its 2s deadline;
		// the second, sent at 2s, is answered once the client reads again
		// at 2.5s.
		time.Sleep(2*interval + interval/2)

		cl.send(`{"type":"subscribe","topics":["status"]}`)

		cl.expect(`{"type":"snapshot","topic":"status","seq":1,"data":null}`)
	})

	t.Run("two missed pongs close the connection", func(t *testing.T) {
		t.Parallel()
		cl := dial(t, url)
		// The server closes the connection at 3s; the client reads after.
		time.Sleep(5 * interval)

		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		_, _, err := cl.c.Read(ctx)

		if got := websocket.CloseStatus(err); got != websocket.StatusPolicyViolation {
			t.Errorf("close status = %v (%v), want %v", got, err, websocket.StatusPolicyViolation)
		}
	})

	t.Run("a reading client stays connected", func(t *testing.T) {
		t.Parallel()
		cl := dial(t, url)
		ctx := cl.c.CloseRead(t.Context())
		select {
		case <-ctx.Done():
			t.Fatal("connection closed")
		case <-time.After(5 * interval):
		}
	})
}

// gatedSessions answers each Revalidate with the next reply the test sends,
// so that a test decides when each revalidation happens and how it ends.
type gatedSessions chan reply

type reply struct {
	sess auth.Session
	err  error
}

func (g gatedSessions) Revalidate(ctx context.Context, _ auth.Session) (auth.Session, error) {
	select {
	case r := <-g:
		return r.sess, r.err
	case <-ctx.Done():
		return auth.Session{}, ctx.Err()
	}
}

// recheckOpts revalidates as soon as the test sends a reply.
var recheckOpts = ws.Options{RecheckInterval: time.Millisecond}

func expectClose(t *testing.T, cl *client, wantStatus websocket.StatusCode, wantReason string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		_, b, err := cl.c.Read(ctx)
		if err == nil {
			t.Errorf("message %s, want the connection closed", b)
			continue
		}
		var ce websocket.CloseError
		if !errors.As(err, &ce) || ce.Code != wantStatus || ce.Reason != wantReason {
			t.Errorf("read error = %v, want close status %d reason %q", err, wantStatus, wantReason)
		}
		return
	}
}

func TestEndedSessionClosesConnection(t *testing.T) {
	sessions := gatedSessions(make(chan reply))
	_, url := serveWith(t, owner, ws.Topics, sessions, recheckOpts)
	cl := dial(t, url)
	cl.send(`{"type":"subscribe","topics":["status"]}`)
	cl.expect(`{"type":"snapshot","topic":"status","seq":1,"data":null}`)

	sessions <- reply{owner, nil}
	cl.send(`{"type":"subscribe","topics":["quote:EMAAR"]}`)
	cl.expect(`{"type":"snapshot","topic":"quote:EMAAR","seq":1,"data":null}`)
	sessions <- reply{err: fmt.Errorf("auth: find session: %w", context.DeadlineExceeded)}
	cl.send(`{"type":"subscribe","topics":["movers"]}`)
	cl.expect(`{"type":"snapshot","topic":"movers","seq":1,"data":null}`)
	sessions <- reply{err: auth.ErrUnauthenticated}

	expectClose(t, cl, ws.StatusSessionEnded, "session_ended")
}

func TestDemotedSessionLosesMarketDataTopics(t *testing.T) {
	sessions := gatedSessions(make(chan reply))
	g, url := serveWith(t, owner, ws.Topics, sessions, recheckOpts)
	cl := dial(t, url)
	cl.send(`{"type":"subscribe","topics":["quote:EMAAR","status","positions","chat:desk-1"]}`)
	cl.expect(
		`{"type":"snapshot","topic":"quote:EMAAR","seq":1,"data":null}`,
		`{"type":"snapshot","topic":"status","seq":1,"data":null}`,
		`{"type":"snapshot","topic":"positions","seq":1,"data":null}`,
		`{"type":"snapshot","topic":"chat:desk-1","seq":1,"data":null}`,
	)
	demoted := owner
	demoted.User.Role = auth.Member

	sessions <- reply{demoted, nil}

	cl.expect(
		`{"type":"error","topic":"positions","code":"forbidden","message":"your role may not read positions"}`,
		`{"type":"error","topic":"quote:EMAAR","code":"forbidden","message":"your role may not read quote"}`,
	)
	publish(t, g, "quote:EMAAR", map[string]string{"last": "11.4"})
	publish(t, g, "positions", []string{})
	publish(t, g, "status", map[string]string{"ibkr": "up"})
	publish(t, g, "chat:desk-1", map[string]string{"id": "m1"})
	cl.expect(
		`{"type":"data","topic":"status","seq":2,"data":{"ibkr":"up"}}`,
		`{"type":"data","topic":"chat:desk-1","seq":2,"data":{"id":"m1"}}`,
	)
	cl.send(`{"type":"subscribe","topics":["quote:EMAAR"]}`)
	cl.expect(`{"type":"error","topic":"quote:EMAAR","code":"forbidden","message":"your role may not read quote"}`)
}

func TestTopicAllowHook(t *testing.T) {
	sessions := gatedSessions(make(chan reply))
	topics := []ws.Topic{{
		Name: "room", Keyed: true, Delivery: ws.Every, Access: ws.AnyUser,
		Allow: func(sess auth.Session, key string) error {
			if key != sess.User.Username {
				return fmt.Errorf("room %s is not yours", key)
			}
			return nil
		},
	}}
	_, url := serveWith(t, owner, topics, sessions, recheckOpts)
	cl := dial(t, url)

	cl.send(`{"type":"subscribe","topics":["room:saud","room:amal"]}`)

	cl.expect(
		`{"type":"snapshot","topic":"room:saud","seq":1,"data":null}`,
		`{"type":"error","topic":"room:amal","code":"forbidden","message":"room amal is not yours"}`,
	)

	// The hook runs again on each revalidation.
	renamed := owner
	renamed.User.Username = "saud2"
	sessions <- reply{renamed, nil}

	cl.expect(`{"type":"error","topic":"room:saud","code":"forbidden","message":"room saud is not yours"}`)
}

func TestEndSessionOnNilGateway(t *testing.T) {
	var g *ws.Gateway
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("EndSession() on a nil Gateway panicked: %v", r)
		}
	}()

	g.EndSession(owner)
}

func TestCloseSendsGoingAway(t *testing.T) {
	g, url := serve(t, owner, ws.Options{})
	cl := dial(t, url)
	cl.send(`{"type":"subscribe","topics":["status"]}`)
	cl.expect(`{"type":"snapshot","topic":"status","seq":1,"data":null}`)
	done := make(chan error, 1)
	go func() {
		_, _, err := cl.c.Read(t.Context())
		done <- err
	}()

	g.Close()

	if got := websocket.CloseStatus(<-done); got != websocket.StatusGoingAway {
		t.Errorf("close status = %v, want %v", got, websocket.StatusGoingAway)
	}
}

func TestForward(t *testing.T) {
	g, url := serve(t, owner, ws.Options{})
	b := bus.New()
	entered, release := make(chan struct{}), make(chan struct{})
	var first sync.Once
	ws.Forward(t.Context(), g, b, bus.AlertTriggered, "alerts", 1, func(e bus.AlertTriggeredEvent) (string, any) {
		first.Do(func() {
			close(entered)
			<-release
		})
		return "", map[string]string{"id": e.AlertID}
	})
	cl := dial(t, url)
	cl.send(`{"type":"subscribe","topics":["alerts"]}`)
	cl.expect(`{"type":"snapshot","topic":"alerts","seq":1,"data":null}`)

	// The forwarder holds a1 in route while a2 to a4 arrive; its one-event
	// bus buffer keeps only a4.
	for _, id := range []string{"a1", "a2", "a3", "a4"} {
		bus.Publish(b, bus.AlertTriggered, bus.AlertTriggeredEvent{AlertID: id})
		if id == "a1" {
			<-entered
		}
	}
	close(release)

	cl.expect(
		`{"type":"data","topic":"alerts","seq":2,"data":{"id":"a1"}}`,
		`{"type":"resync","topic":"alerts"}`,
		`{"type":"data","topic":"alerts","seq":3,"data":{"id":"a4"}}`,
	)
}

func TestForwardKeyedTopic(t *testing.T) {
	g, url := serve(t, member, ws.Options{})
	b := bus.New()
	ws.Forward(t.Context(), g, b, bus.SourceHealthChanged, "chat", 8, func(e bus.SourceHealthChangedEvent) (string, any) {
		return "desk-1", map[string]string{"text": e.Detail}
	})
	cl := dial(t, url)
	cl.send(`{"type":"subscribe","topics":["chat:desk-1"]}`)
	cl.expect(`{"type":"snapshot","topic":"chat:desk-1","seq":1,"data":null}`)

	bus.Publish(b, bus.SourceHealthChanged, bus.SourceHealthChangedEvent{Detail: "hello"})

	cl.expect(`{"type":"data","topic":"chat:desk-1","seq":2,"data":{"text":"hello"}}`)
}

func TestForwardUnknownTopicPanics(t *testing.T) {
	g := ws.New(slog.New(slog.DiscardHandler), ws.Topics, liveSessions{}, ws.Options{})
	defer func() {
		if got := fmt.Sprint(recover()); got != "ws: Forward to unknown topic news" {
			t.Errorf("panic = %q, want %q", got, "ws: Forward to unknown topic news")
		}
	}()

	ws.Forward(t.Context(), g, bus.New(), bus.AlertTriggered, "news", 1, func(bus.AlertTriggeredEvent) (string, any) { return "", nil })
}
