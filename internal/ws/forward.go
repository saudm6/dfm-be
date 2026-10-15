package ws

import (
	"context"
	"log/slog"

	"github.com/saudm6/dfm-be/internal/bus"
)

// Forward subscribes to the bus topic from with the given buffer and
// publishes each event to the gateway topic called name, keyed by the key
// route returns ("" for a topic without a key), with route's data as the
// payload. It returns at once and forwards until ctx is done, then closes
// the bus subscription.
//
// The bus drops the oldest event when the buffer is full. Forward then
// sends a resync on every subscription to name before the next event, so
// clients know to recover. It panics if g has no topic called name.
func Forward[T any](ctx context.Context, g *Gateway, b *bus.Bus, from *bus.Topic[T], name string, buffer int, route func(T) (key string, data any)) {
	if _, ok := g.topics[name]; !ok {
		panic("ws: Forward to unknown topic " + name)
	}
	sub := bus.Subscribe(b, from, buffer)
	go func() {
		defer sub.Close()
		var dropped uint64
		for {
			var ev T
			select {
			case <-ctx.Done():
				return
			case ev = <-sub.C():
			}
			if d := sub.Dropped(); d != dropped {
				g.logger.LogAttrs(ctx, slog.LevelWarn, "ws: bus subscription dropped events",
					slog.String("bus_topic", from.Name()),
					slog.String("topic", name),
					slog.Uint64("dropped", d-dropped),
				)
				dropped = d
				g.Resync(name)
			}
			topic := name
			key, data := route(ev)
			if key != "" {
				topic += ":" + key
			}
			if err := g.Publish(topic, data); err != nil {
				g.logger.LogAttrs(ctx, slog.LevelError, "ws: forward event",
					slog.String("bus_topic", from.Name()),
					slog.String("error", err.Error()),
				)
			}
		}
	}()
}
