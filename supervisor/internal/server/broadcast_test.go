package server

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
)

// wedgedStream models a control connection whose peer has stopped reading: the
// first Write parks until the write deadline the encoder installed expires (or
// until the stream is closed out from under it, which is how the broadcaster
// evicts a wedged subscriber). It records closes so tests can assert the
// subscriber's connection was actually torn down rather than silently
// unsubscribed.
type wedgedStream struct {
	mu       sync.Mutex
	deadline time.Time
	closed   bool
	closes   int
	release  chan struct{}
	entered  chan struct{}
	once     sync.Once
}

func newWedgedStream() *wedgedStream {
	return &wedgedStream{release: make(chan struct{}), entered: make(chan struct{})}
}

func (w *wedgedStream) SetWriteDeadline(t time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deadline = t
	return nil
}

func (w *wedgedStream) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	w.mu.Lock()
	deadline := w.deadline
	w.mu.Unlock()
	var expiry <-chan time.Time
	if !deadline.IsZero() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		expiry = timer.C
	}
	select {
	case <-w.release:
		return 0, os.ErrClosed
	case <-expiry:
		return 0, os.ErrDeadlineExceeded
	}
}

func (w *wedgedStream) Close() error {
	w.mu.Lock()
	w.closes++
	w.closed = true
	w.mu.Unlock()
	w.once.Do(func() { close(w.entered) })
	select {
	case <-w.release:
	default:
		close(w.release)
	}
	return nil
}

func (w *wedgedStream) closeCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closes
}

// recordingStream is a healthy subscriber: every write lands immediately.
type recordingStream struct {
	mu     sync.Mutex
	writes int
	closes int
	got    chan struct{}
}

func (r *recordingStream) Write(p []byte) (int, error) {
	r.mu.Lock()
	r.writes++
	n := r.writes
	r.mu.Unlock()
	if r.got != nil && n == 1 {
		close(r.got)
	}
	return len(p), nil
}

func (r *recordingStream) Close() error {
	r.mu.Lock()
	r.closes++
	r.mu.Unlock()
	return nil
}

func (r *recordingStream) counts() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.writes, r.closes
}

func testEvent() *protocol.Event {
	return protocol.NewEvent("node.state", protocol.NodeStateData{Node: 1, State: "running"})
}

func subCount(b *broadcaster) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

// Item 6 regression: publish must never block on a subscriber that has stopped
// reading. Before the bounded-queue/writer-goroutine split, publish wrote to
// each subscriber synchronously from the emitting goroutine, so one wedged tab
// stalled every later node.state emission and hung lab.start mid-loop.
func TestPublishDoesNotBlockOnWedgedSubscriber(t *testing.T) {
	b := newBroadcaster()
	stream := newWedgedStream()
	unsub := b.subscribe(protocol.NewEncoder(stream))
	defer unsub()

	// Let the writer goroutine dequeue one event and park inside Write, so the
	// backlog that follows has to be absorbed by the bounded queue alone.
	b.publish(testEvent())
	select {
	case <-stream.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("writer goroutine never reached the socket write")
	}

	done := make(chan struct{})
	go func() {
		for i := 0; i < broadcastQueueSize+8; i++ {
			b.publish(testEvent())
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("publish blocked on a wedged subscriber")
	}

	if n := subCount(b); n != 0 {
		t.Fatalf("backlogged subscriber was not dropped, subscribers=%d", n)
	}
	if n := stream.closeCount(); n == 0 {
		t.Fatal("dropped subscriber's stream was not closed; its read loop would never notice")
	}
}

// timedOutStream stands in for a peer whose TCP receive window never opens:
// it reports that the encoder installed a real write deadline, then fails the
// write the way net.Conn does once that deadline expires.
type timedOutStream struct {
	mu       sync.Mutex
	deadline time.Time
	closes   int
}

func (s *timedOutStream) SetWriteDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !t.IsZero() {
		s.deadline = t
	}
	return nil
}

func (s *timedOutStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	set := !s.deadline.IsZero()
	s.mu.Unlock()
	if !set {
		return len(p), nil
	}
	return 0, os.ErrDeadlineExceeded
}

func (s *timedOutStream) Close() error {
	s.mu.Lock()
	s.closes++
	s.mu.Unlock()
	return nil
}

func (s *timedOutStream) state() (time.Time, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deadline, s.closes
}

// A wedged subscriber must also be dropped by the write deadline alone, with
// no backlog pressure at all — that is the suspended-laptop case where events
// are sparse but the peer never ACKs. Asserts both halves: the encoder really
// installs a bounded deadline (nothing in ws/protocol did before item 6), and
// the resulting timeout drops and closes the subscriber.
func TestWriteDeadlineDropsWedgedSubscriber(t *testing.T) {
	b := newBroadcaster()
	stream := &timedOutStream{}
	unsub := b.subscribe(protocol.NewEncoder(stream))
	defer unsub()

	b.publish(testEvent())
	deadline := time.Now().Add(5 * time.Second)
	for subCount(b) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber survived a write that exceeded its deadline")
		}
		time.Sleep(5 * time.Millisecond)
	}
	installed, closes := stream.state()
	if installed.IsZero() {
		t.Fatal("event write installed no write deadline")
	}
	if d := time.Until(installed); d > broadcastWriteTimeout+time.Second {
		t.Fatalf("write deadline %v is not bounded by broadcastWriteTimeout %v", d, broadcastWriteTimeout)
	}
	if closes == 0 {
		t.Fatal("timed-out subscriber's stream was not closed")
	}
}

// One dropped subscriber must not cost a healthy one its events, and the
// healthy one must still be delivered to asynchronously.
func TestHealthySubscriberKeepsReceivingAfterPeerDropped(t *testing.T) {
	b := newBroadcaster()
	wedged := newWedgedStream()
	wedgedUnsub := b.subscribe(protocol.NewEncoder(wedged))
	defer wedgedUnsub()
	healthy := &recordingStream{got: make(chan struct{})}
	healthyUnsub := b.subscribe(protocol.NewEncoder(healthy))
	defer healthyUnsub()

	b.publish(testEvent())
	select {
	case <-wedged.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("wedged writer never reached the socket write")
	}
	for i := 0; i < broadcastQueueSize+8; i++ {
		b.publish(testEvent())
	}

	select {
	case <-healthy.got:
	case <-time.After(5 * time.Second):
		t.Fatal("healthy subscriber received nothing while a peer was wedged")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		writes, closes := healthy.counts()
		if closes != 0 {
			t.Fatalf("healthy subscriber was closed (%d times)", closes)
		}
		if writes >= broadcastQueueSize {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("healthy subscriber only received %d events", writes)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := subCount(b); n != 1 {
		t.Fatalf("subscribers=%d, want only the healthy one", n)
	}
}

// A clean unsubscribe (ServeConn unwinding) must not close the stream: the
// connection owner does that, and double-closing would mask its own error
// handling. It must still stop the writer goroutine.
func TestUnsubscribeStopsWriterWithoutClosingStream(t *testing.T) {
	b := newBroadcaster()
	stream := &recordingStream{}
	unsub := b.subscribe(protocol.NewEncoder(stream))
	unsub()
	if n := subCount(b); n != 0 {
		t.Fatalf("subscribers=%d after unsubscribe", n)
	}
	unsub() // idempotent
	b.publish(testEvent())
	time.Sleep(50 * time.Millisecond)
	writes, closes := stream.counts()
	if closes != 0 {
		t.Fatalf("unsubscribe closed the caller-owned stream (%d times)", closes)
	}
	if writes != 0 {
		t.Fatalf("writer goroutine kept writing after unsubscribe (%d writes)", writes)
	}
}
