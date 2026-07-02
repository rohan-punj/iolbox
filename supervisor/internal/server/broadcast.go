package server

import (
	"sync"

	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
)

// broadcaster fans supervisor events out to every subscribed client encoder.
type broadcaster struct {
	mu   sync.Mutex
	subs map[*protocol.Encoder]struct{}
}

func newBroadcaster() *broadcaster {
	return &broadcaster{subs: make(map[*protocol.Encoder]struct{})}
}

// subscribe registers an encoder and returns an unsubscribe function.
func (b *broadcaster) subscribe(enc *protocol.Encoder) func() {
	b.mu.Lock()
	b.subs[enc] = struct{}{}
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		delete(b.subs, enc)
		b.mu.Unlock()
	}
}

// publish writes an event to all current subscribers. Write failures are
// ignored here; the connection's own read loop will drop it.
func (b *broadcaster) publish(ev *protocol.Event) {
	b.mu.Lock()
	subs := make([]*protocol.Encoder, 0, len(b.subs))
	for e := range b.subs {
		subs = append(subs, e)
	}
	b.mu.Unlock()
	for _, e := range subs {
		_ = e.WriteEvent(ev)
	}
}
