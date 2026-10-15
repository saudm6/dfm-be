package ws

import (
	"fmt"
	"strings"

	"github.com/saudm6/dfm-be/internal/auth"
	"github.com/saudm6/dfm-be/internal/domain"
)

// Delivery says what the gateway does with a topic's messages when a
// connection's send queue backs up.
type Delivery int

const (
	// Latest topics carry state. A newer message replaces an unsent older
	// one, so a slow client gets the latest value with a seq gap.
	Latest Delivery = iota + 1
	// Every topics carry events. They are never coalesced; those that do not
	// fit the send queue are dropped and the client is sent a resync.
	Every
)

// Access says which roles may subscribe to a topic.
type Access int

const (
	// AnyUser lets every signed-in account subscribe.
	AnyUser Access = iota + 1
	// MarketData lets only roles with market data subscribe
	// (auth.Role.MarketData).
	MarketData
)

// maxKeyLen caps the key of a keyed topic.
const maxKeyLen = 64

// Topic describes one topic, or one family of keyed topics such as
// quote:{symbol}.
type Topic struct {
	// Name is the topic name, the part before the first ':'.
	Name string
	// Keyed topics need a key after the ':'; others take none.
	Keyed bool
	// ValidKey, when set, reports whether a key names something real, such
	// as a well-formed symbol. Keys are 1 to 64 bytes whatever it says.
	ValidKey func(key string) bool
	Delivery Delivery
	Access   Access
	// Allow, when set, is checked after Access on each subscribe and each
	// session revalidation. It returns an error, whose text the client is
	// sent, when sess may not read the topic with key ("" for a topic
	// without a key). It must not block or call the Gateway.
	Allow func(sess auth.Session, key string) error
}

// Topics are the topics of api/websocket.md. Each feature PR feeds its topic
// with Gateway.Publish or Forward.
var Topics = []Topic{
	{Name: "quote", Keyed: true, ValidKey: func(k string) bool { return domain.Symbol(k).Valid() }, Delivery: Latest, Access: MarketData},
	{Name: "market.summary", Delivery: Latest, Access: MarketData},
	{Name: "movers", Delivery: Latest, Access: MarketData},
	{Name: "disclosures", Delivery: Every, Access: MarketData},
	{Name: "alerts", Delivery: Every, Access: MarketData},
	{Name: "orders", Delivery: Every, Access: MarketData},
	{Name: "positions", Delivery: Latest, Access: MarketData},
	{Name: "status", Delivery: Latest, Access: AnyUser},
	// Any signed-in user may read any chat room until BE-48 restricts rooms
	// and direct messages with an Allow hook.
	{Name: "chat", Keyed: true, Delivery: Every, Access: AnyUser},
}

// lookup returns the Topic that the full topic name s belongs to and its
// key, such as the quote Topic and "EMAAR" for "quote:EMAAR".
func lookup(topics map[string]Topic, s string) (Topic, string, error) {
	name, key, hasKey := strings.Cut(s, ":")
	t, ok := topics[name]
	switch {
	case !ok:
		return Topic{}, "", fmt.Errorf("no topic named %q", name)
	case t.Keyed && !hasKey:
		return Topic{}, "", fmt.Errorf("topic %s needs a key: %s:{key}", name, name)
	case !t.Keyed && hasKey:
		return Topic{}, "", fmt.Errorf("topic %s takes no key", name)
	case t.Keyed && (key == "" || len(key) > maxKeyLen || (t.ValidKey != nil && !t.ValidKey(key))):
		return Topic{}, "", fmt.Errorf("%q is not a valid key for topic %s", key, name)
	}
	return t, key, nil
}
