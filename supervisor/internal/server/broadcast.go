package server

import (
	"sync"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
)

// broadcastQueueSize is the per-subscriber event backlog. It is sized to
// absorb a whole-lab start burst (every node emits several state events, plus
// link/fabric events) without dropping a client that is merely mid-render, and
// small enough that a genuinely dead peer cannot pin unbounded memory.
const broadcastQueueSize = 256

// broadcastWriteTimeout bounds a single event frame write. Writing a short
// NDJSON line to a peer whose TCP receive window is open takes microseconds;
// exceeding this means the peer has stopped reading (suspended laptop, wedged
// tab), which without a deadline would block until the kernel's retransmit
// timeout — minutes.
const broadcastWriteTimeout = 5 * time.Second

// broadcastEnqueueGrace is how long publish will wait for a subscriber whose
// queue is momentarily full before dropping it. A pure non-blocking send is
// too trigger-happy: a burst emitted in a tight loop can outrun a perfectly
// healthy writer goroutine simply because the scheduler has not run it yet,
// and that dropped a live GUI in testing. This grace is short enough to keep
// publish's worst case bounded and small (it is paid once per subscriber, on
// the publish that finally evicts it) and long enough that only a peer that
// has genuinely stopped reading can exhaust it.
const broadcastEnqueueGrace = 250 * time.Millisecond

// broadcastSub is one subscribed control connection: its encoder, a bounded
// event queue, and the writer goroutine's exit signal.
type broadcastSub struct {
	enc    *protocol.Encoder
	events chan *protocol.Event
	done   chan struct{}
	once   sync.Once
}

// broadcaster fans supervisor events out to every subscribed client.
//
// publish never performs a socket write and never blocks: it does a bounded,
// non-blocking channel send per subscriber, and one dedicated writer goroutine
// per subscriber drains that queue onto the wire under a write deadline.
// Before this split, publish wrote synchronously from whichever goroutine
// emitted the event, so a single wedged subscriber stalled every later
// emission — i.e. lab.start hung mid-loop behind a stale browser tab.
//
// A subscriber is dropped (queue overflow, or a failed/timed-out write) by
// closing its underlying stream. Closing rather than merely unregistering is
// deliberate: the stream is the same io.ReadWriteCloser ServeConn is blocked
// reading, so the close unblocks that read loop, ends the connection, and lets
// the GUI notice and reconnect — a silently unsubscribed but still-open
// connection would look permanently frozen instead.
type broadcaster struct {
	mu   sync.Mutex
	subs map[*broadcastSub]struct{}
}

func newBroadcaster() *broadcaster {
	return &broadcaster{subs: make(map[*broadcastSub]struct{})}
}

// subscribe registers an encoder, starts its writer goroutine, and returns an
// unsubscribe function. The returned function performs a clean unsubscribe: it
// stops the writer but does NOT close the stream, because the caller
// (ServeConn) owns that stream and is already unwinding.
func (b *broadcaster) subscribe(enc *protocol.Encoder) func() {
	sub := &broadcastSub{
		enc:    enc,
		events: make(chan *protocol.Event, broadcastQueueSize),
		done:   make(chan struct{}),
	}
	b.mu.Lock()
	b.subs[sub] = struct{}{}
	b.mu.Unlock()
	go b.writeLoop(sub)
	return func() { b.remove(sub) }
}

// writeLoop is the only goroutine that writes events to this subscriber, so
// event ordering is preserved and no two events interleave on the wire. It
// checks done first on every iteration so an unsubscribed connection stops
// writing promptly instead of draining a stale backlog.
func (b *broadcaster) writeLoop(sub *broadcastSub) {
	for {
		select {
		case <-sub.done:
			return
		default:
		}
		select {
		case <-sub.done:
			return
		case ev := <-sub.events:
			if err := sub.enc.WriteEventWithDeadline(ev, broadcastWriteTimeout); err != nil {
				// The write deadline may have expired mid-frame, leaving the
				// stream with a partial line; it is not recoverable, so drop.
				b.drop(sub)
				return
			}
		}
	}
}

// remove unregisters a subscriber and stops its writer goroutine. Idempotent.
func (b *broadcaster) remove(sub *broadcastSub) {
	sub.once.Do(func() {
		b.mu.Lock()
		delete(b.subs, sub)
		b.mu.Unlock()
		close(sub.done)
	})
}

// drop unregisters a subscriber AND closes its stream. Closing from the
// publisher's goroutine is what unblocks a writer goroutine currently wedged
// inside a socket write; net.Conn.Close is safe to call concurrently with an
// in-flight Write.
func (b *broadcaster) drop(sub *broadcastSub) {
	b.remove(sub)
	_ = sub.enc.Close()
}

// publish queues an event for every current subscriber. A subscriber whose
// bounded queue is already full is dropped immediately; the publisher never
// waits for a socket write or for a slow client to catch up.
func (b *broadcaster) publish(ev *protocol.Event) {
	b.mu.Lock()
	subs := make([]*broadcastSub, 0, len(b.subs))
	for sub := range b.subs {
		subs = append(subs, sub)
	}
	b.mu.Unlock()
	for _, sub := range subs {
		select {
		case <-sub.done:
			// Already unsubscribed between the snapshot and now.
			continue
		default:
		}
		select {
		case sub.events <- ev:
			continue
		default:
		}
		timer := time.NewTimer(broadcastEnqueueGrace)
		select {
		case sub.events <- ev:
			timer.Stop()
		case <-sub.done:
			timer.Stop()
		case <-timer.C:
			b.drop(sub)
		}
	}
}
