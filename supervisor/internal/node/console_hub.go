package node

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/consolediag"
	"github.com/rohanpunj/iolbox/supervisor/internal/consolescript"
	"github.com/rohanpunj/iolbox/supervisor/internal/telnet"
)

// errHubClosed is returned by ClaimTurn/RunExec when the hub has already shut
// down (node exited/torn down) — the v0.3.0 Phase 4 equivalent of runShow's
// previous "dial console: connection refused" failure mode, but without a
// socket involved.
var errHubClosed = errors.New("node: console hub is shut down")

// ErrNoConsoleHub is returned by Process.RunExec when the node has no console
// hub at all — VPCS (its own telnet server, see the Process doc comment) or an
// IOL node that hasn't started yet / has already torn down. Distinct from
// errHubClosed (hub existed but shut down) so callers/logs can tell "never had
// one" from "had one, it's gone" apart, though today's only caller
// (painter_linux.go's runShow) treats both as "console unavailable".
var ErrNoConsoleHub = errors.New("node: no console hub for this node")

// consoleHub multiplexes ONE node's pty console across any number of
// subscribers simultaneously — telnet TCP clients (native OS telnet) and
// in-process subscribers (wsbridge, programmatic exec) alike. It replaces the
// previous one-client-at-a-time bridge (bridgeConsole), whose synchronous
// accept loop starved every client behind the first: the wsbridge webconsole
// holds its connection for the tab's whole lifetime, so a native telnet
// client's TCP connect completed in the kernel backlog but was never
// serviced ("opens but does not work").
//
// Design:
//
//   - ONE reader goroutine owns ptmx.Read and broadcasts each chunk to every
//     attached subscriber. Subscribers never read the pty themselves, so
//     concurrent subscribers cannot steal bytes from each other.
//   - Each subscriber has a BOUNDED output queue pumped by its own writer
//     goroutine (TCP clients) or drained directly by the in-process consumer
//     (wsbridge/programmatic). Backpressure policy: a subscriber whose queue
//     is full when a broadcast arrives is DROPPED (TCP: connection closed;
//     in-process: its channel is closed, ending its Subscribe loop). A
//     console stream is low-bandwidth; a client that falls hubClientQueue
//     chunks behind is dead or unrecoverably slow, and dropping it beats
//     stalling the pty reader or unboundedly buffering. (Documented in
//     supervisor/README.md.)
//   - Client->pty writes are serialized through one mutex so interleaved
//     keystrokes from two clients never split multi-byte sequences written in
//     a single call.
//   - Every native TCP stream owns its telnet negotiator. Parser state and
//     temporary decoded buffers never cross streams. In-process clients use
//     application bytes directly.
//   - A replay ring of the most recent replayRingSize bytes of pty output is
//     sent to every newly attached subscriber first, so a fresh session shows
//     the current prompt/context instead of a blank screen.
//
// Lifecycle: the hub shuts down when the pty read fails (node exit or
// teardown's ptmx.Close unblocking the read), closing every subscriber. The
// hub never closes the pty itself — Process.teardown owns that.
type consoleHub struct {
	pty io.ReadWriter
	// name is the node's display name, sent to every attaching client as an
	// xterm title escape (OSC 0) so native telnet clients that honour remote
	// titles (PuTTY & friends) label their tab/window "R1" instead of a bare
	// host:port. Clients that ignore OSC just discard the sequence.
	name    string
	metrics *consolediag.Recorder

	mu      sync.Mutex
	clients map[*hubClient]struct{}
	ring    []byte
	closed  bool

	// wmu serializes all client->pty writes.
	wmu sync.Mutex

	// turn is the v0.3.0 Phase 3/4 input-arbitration gate: while a programmatic
	// caller (ClaimTurn/RunExec) holds it, interactive input from BOTH TCP
	// clients (attach's reader) and in-process Subscriptions (Subscription.Write)
	// is queued instead of written straight to the pty, then flushed verbatim on
	// release — see docs/v0.3.0-console-unification.md §2.3/§7. Interactive
	// writes never touch wmu directly anymore; they all funnel through
	// gatedWrite, which either writes immediately (no turn active) or buffers
	// (turn active).
	turn inputTurn

	done chan struct{}
	once sync.Once
}

// turnQueueCap bounds the interactive-input queue held while a programmatic
// turn is active (docs/v0.3.0-console-unification.md §2.3: "cap the queue...
// and drop-oldest... only in the pathological case of someone holding a key
// down across a stalled painter call"). A real turn finishes in well under a
// second, so this is generous for anything a human could type in that window.
const turnQueueCap = 256

const consoleWriteTimeout = 2 * time.Second

// inputTurn is the hub's exclusive-input-turn gate (v0.3.0 Phase 4). Only one
// programmatic caller (painter's runShow today; any future scripted driver)
// may hold it at a time per node. While held, interactive input from every
// other source is queued (never dropped, never written straight to the pty)
// and flushed in original order the instant the turn releases.
type inputTurn struct {
	mu     sync.Mutex // guards the fields below; held only briefly per operation
	active bool
	holder string // e.g. "painter:show:R1" — for the force-release warning log
	id     uint64 // distinguishes expired holders from their successors
	queue  [][]byte
}

// claim marks the turn active for holder, or returns false if a turn is
// already active (caller should treat that as "busy", though today's only
// caller — ClaimTurn — retries under ctx rather than surfacing this).
func (t *inputTurn) claim(holder string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active {
		return false
	}
	t.active = true
	t.holder = holder
	t.id++
	return true
}

// enqueue buffers an interactive write while a turn is active. Bounded by
// turnQueueCap; drop-oldest on overflow (documented policy, §2.3) — this only
// matters if a turn hangs past its timeout, which is itself a bug the 8s
// ClaimTurn deadline and force-release log below catch.
func (t *inputTurn) enqueue(p []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cp := make([]byte, len(p))
	copy(cp, p)
	t.queue = append(t.queue, cp)
	for len(t.queue) > turnQueueCap {
		t.queue = t.queue[1:]
	}
}

// release clears the active turn and returns the queued interactive bytes (in
// original order) for the caller to flush to the pty. Idempotent: releasing a
// turn that isn't held (e.g. double-release race) is a no-op returning nil.
func (t *inputTurn) release() [][]byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		return nil
	}
	t.active = false
	t.holder = ""
	queued := t.queue
	t.queue = nil
	return queued
}

// isActive reports whether a turn is currently held (used by gatedWrite to
// decide queue-vs-write-through without taking the turn itself).
func (t *inputTurn) isActive() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.active
}

// replayRingSize is how many bytes of recent pty output are kept for replay to
// newly attached clients (enough for a screenful of prompt/context).
const replayRingSize = 8 * 1024

// hubClientQueue is the per-client output queue depth, in broadcast chunks
// (each up to 16 KiB for deadline-capable readers). A client this far behind is dropped.
const hubClientQueue = 64

// hubClient is one TCP or in-process subscriber. Its mutex protects
// output enqueue/close; each native reader owns its own telnet parser.
type hubClient struct {
	mu     sync.Mutex // serializes every enqueue with output channel closure
	closed bool
	conn   net.Conn // nil for in-process subscribers
	out    chan []byte
	stop   chan struct{}
	once   sync.Once
	// crPending tracks a CR seen at the END of the previous input chunk, so the
	// telnet NVT line-ending normalization (CR LF / CR NUL -> CR) works across
	// chunk boundaries. Touched only by this client's reader goroutine (TCP) or
	// by Subscription.Write (in-process) — never both, since a client is one or
	// the other.
	crPending bool
}

// Subscription is the in-process handle an attach-without-a-socket consumer
// (wsbridge, and RunExec/ClaimTurn internally) gets from consoleHub.Subscribe.
// It mirrors hubClient's output/replay/backpressure
// behavior exactly, minus the TCP socket and minus telnet negotiation — the
// hub already decoded the pty's byte stream once for every TCP peer that
// needs it; an in-process subscriber just wants clean application bytes in
// and clean application bytes out.
type Subscription struct {
	hub *consoleHub
	c   *hubClient
	// Out delivers decoded pty output chunks (the replay ring first, then live
	// broadcast chunks) until the hub shuts down or the subscription is
	// dropped for backpressure, at which point Out is closed.
	Out <-chan []byte
	// Done closes immediately on detach, eviction, or shutdown, independently
	// of buffered output. Consumers use it to interrupt blocked socket writes.
	Done <-chan struct{}
}

// Write sends raw application bytes toward the pty, subject to the hub's
// input-arbitration gate (v0.3.0 Phase 3): if a programmatic turn is active
// (ClaimTurn/RunExec), the bytes are queued and flushed verbatim on release
// instead of being written immediately — exactly like a TCP client's (attach's
// reader) interactive keystrokes. When no turn is active this is the same
// direct wmu-serialized write as before Phase 3.
func (s *Subscription) Write(p []byte) error {
	return s.hub.gatedWrite(p)
}

// gatedWrite is the ONE interactive-input write path every non-programmatic
// caller uses (TCP clients via attach's reader, in-process subscribers via
// Subscription.Write) — v0.3.0 Phase 3. If a programmatic turn is active it
// queues p (see inputTurn.enqueue); otherwise it writes straight to the pty
// under wmu, unchanged from pre-Phase-3 behavior. A turn holder's OWN writes
// never call this — RunExec/ClaimTurn callers write directly (see
// writeDirect), since they already hold the gate and queuing their own bytes
// behind themselves would deadlock the flush.
func (h *consoleHub) gatedWrite(p []byte) error {
	if len(p) == 0 {
		return nil
	}
	h.wmu.Lock()
	defer h.wmu.Unlock()
	select {
	case <-h.done:
		return errHubClosed
	default:
	}
	if h.turn.isActive() {
		h.turn.enqueue(p)
		return nil
	}
	return h.writePTY(context.Background(), p)
}

// writeDirect writes p straight to the pty under wmu, bypassing the turn gate
// entirely. Used by: gatedWrite when no turn is active, the turn holder itself
// (ClaimTurn/RunExec — it already serialized itself in front of every other
// writer by holding the turn), and the turn-release flush (queued interactive
// bytes, replayed in order after the turn's own output settles).
func (h *consoleHub) writeDirect(p []byte) error {
	if len(p) == 0 {
		return nil
	}
	h.wmu.Lock()
	err := h.writePTY(context.Background(), p)
	h.wmu.Unlock()
	return err
}

// Runtime PTYs and PC sockets support write deadlines. Test doubles may omit
// them. This is called only while wmu is held, so deadlines cannot interfere
// with another writer and a partial write is never silently accepted.
func (h *consoleHub) writePTY(ctx context.Context, p []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d, ok := h.pty.(interface{ SetWriteDeadline(time.Time) error }); ok {
		deadline := time.Now().Add(consoleWriteTimeout)
		if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
			deadline = end
		}
		if err := d.SetWriteDeadline(deadline); err != nil {
			return err
		}
		defer d.SetWriteDeadline(time.Time{})
	}
	n, err := h.pty.Write(p)
	if err == nil && n != len(p) {
		return io.ErrShortWrite
	}
	return err
}

// Claim/script waits remain cancellable even while an earlier device write
// owns the sequencing mutex. No detached writer goroutine can outlive a turn.
func (h *consoleHub) lockInput(ctx context.Context) error {
	for !h.wmu.TryLock() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-h.done:
			return errHubClosed
		case <-time.After(turnClaimPoll):
		}
	}
	return nil
}

// Unsubscribe detaches the subscription. Idempotent; safe to call more than
// once (e.g. from both a defer and an explicit early-exit path).
func (s *Subscription) Unsubscribe() {
	s.hub.detach(s.c)
}

// NewSubscriptionForTest starts a standalone consoleHub over pty and returns
// an in-process Subscription attached to it, for exercising a
// Subscription-shaped consumer (e.g. internal/wsbridge's bridgeConsoleSub)
// without a real spawned node. Exported ONLY for cross-package tests — normal
// callers get a Subscription via Process.Subscribe/Server.ConsoleSubscribe.
func NewSubscriptionForTest(pty io.ReadWriter, name string) *Subscription {
	return newConsoleHub(pty, name).Subscribe()
}

// newConsoleHub starts the hub's pty reader goroutine and returns the hub.
// name is the node's display name for the attach-time title escape (may be "").
func newConsoleHub(pty io.ReadWriter, name string) *consoleHub {
	h := &consoleHub{
		pty:     pty,
		name:    name,
		metrics: consolediag.New(name, "pty-read-to-broadcast"),
		clients: make(map[*hubClient]struct{}),
		done:    make(chan struct{}),
	}
	go h.readLoop()
	return h
}

const consoleReadBatchLimit = 16 * 1024
const consoleReadBatchWindow = 2 * time.Millisecond

type consoleReadDeadline interface {
	SetReadDeadline(time.Time) error
}

// readConsoleBatch keeps one fixed deadline from the first received bytes.
// The initial read blocks normally; only subsequent reads may wait for the
// small coalescing window. The caller supplies a bounded buffer and owns all
// reads, so no background reader can outlive this batch or steal later bytes.
func readConsoleBatch(reader io.Reader, deadline consoleReadDeadline, buf []byte) (n int, at time.Time, err error) {
	if deadline != nil {
		// A previous batch's timeout must never expire the next initial read.
		if err = deadline.SetReadDeadline(time.Time{}); err != nil {
			return
		}
	}
	n, err = reader.Read(buf)
	if n == 0 {
		return
	}
	at = time.Now()
	if err != nil || deadline == nil || n == len(buf) {
		return
	}
	until := at.Add(consoleReadBatchWindow)
	if err = deadline.SetReadDeadline(until); err != nil {
		return
	}
	for n < len(buf) && time.Now().Before(until) {
		var read int
		read, err = reader.Read(buf[n:])
		n += read
		if err != nil {
			var timeout net.Error
			if errors.As(err, &timeout) && timeout.Timeout() {
				// The batching deadline ends this batch, not the console stream.
				err = nil
			}
			return
		}
		if read == 0 {
			return
		}
	}
	return
}

// readLoop is the single owner of pty reads: it appends each bounded batch to the
// replay ring and enqueues it to every client, dropping clients whose queue is
// full. Exits (and shuts the hub down) when the pty read errors — node exit or
// teardown closing the pty master.
func (h *consoleHub) readLoop() {
	var deadline consoleReadDeadline
	limit := 4096
	if d, ok := h.pty.(consoleReadDeadline); ok && d.SetReadDeadline(time.Time{}) == nil {
		deadline = d
		limit = consoleReadBatchLimit
	}
	buf := make([]byte, limit)
	for {
		// Exit promptly once the hub is shut down (teardown closed done), even if
		// the pty keeps yielding: without this a pty that returns (0, nil) on a
		// dead node — or a Close that fails to interrupt the read — would spin
		// this goroutine at 100% CPU (observed: leaked readLoops burning CPU
		// after a node was stopped). This is a non-blocking guard between reads.
		select {
		case <-h.done:
			return
		default:
		}
		n, at, err := readConsoleBatch(h.pty, deadline, buf)
		if n > 0 {
			// Copy out of the reused read buffer before it escapes to queues.
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			h.broadcast(chunk)
			// Includes the bounded coalescing delay, copy, and fanout, but not
			// the initial blocking read's idle time. Bytes count the full batch.
			h.metrics.Observe(n, time.Since(at))
		}
		if err != nil {
			h.shutdown()
			return
		}
	}
}

// broadcast appends chunk to the replay ring and enqueues it to every client.
// Clients with a full queue are dropped (see backpressure policy above).
func (h *consoleHub) broadcast(chunk []byte) {
	var dropped []*hubClient
	h.mu.Lock()
	// Ring append, trimmed to the newest replayRingSize bytes.
	h.ring = append(h.ring, chunk...)
	if over := len(h.ring) - replayRingSize; over > 0 {
		h.ring = append([]byte(nil), h.ring[over:]...)
	}
	for c := range h.clients {
		if !c.enqueue(chunk) {
			delete(h.clients, c)
			dropped = append(dropped, c)
		}
	}
	h.mu.Unlock()
	for _, c := range dropped {
		c.close()
	}
}

// registerLocked creates and registers a hubClient, queuing the standard
// attach preamble (telnet negotiation advertisement for TCP clients only,
// title escape, replay ring) under h.mu so no broadcast can interleave before
// registration. Returns nil if the hub is already shut down (conn, if
// non-nil, is closed by the caller in that case — see attach/Subscribe).
// wantTelnetPreamble is false for in-process subscribers: they consume
// decoded application bytes and were never going to speak telnet themselves,
// so advertising WILL ECHO/WILL SGA to them (bytes they'd forward straight
// into a browser's xterm.js, which already handles its own local echo
// semantics) would be a protocol leak, not a courtesy.
func (h *consoleHub) registerLocked(conn net.Conn, wantTelnetPreamble, replay bool) *hubClient {
	c := &hubClient{
		conn: conn,
		out:  make(chan []byte, hubClientQueue),
		stop: make(chan struct{}),
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	if wantTelnetPreamble {
		// Queue the telnet preamble (server-side echo + suppress-go-ahead, matching
		// a real Cisco console; a dumb raw client just discards these valid
		// commands) — only meaningful to a real telnet peer.
		c.out <- []byte{
			telnet.IAC, telnet.WILL, telnet.OptEcho,
			telnet.IAC, telnet.WILL, telnet.OptSGA,
		}
	}
	if replay && h.name != "" {
		c.out <- []byte("\x1b]0;" + h.name + "\x07")
	}
	if replay && len(h.ring) > 0 {
		replay := make([]byte, len(h.ring))
		copy(replay, h.ring)
		c.out <- replay
	}
	h.clients[c] = struct{}{}
	return c
}

// attach registers a real TCP telnet client: it volunteers WILL ECHO + WILL
// SGA (so line-mode clients switch to character-at-a-time and let the node
// own echo), replays the recent-output ring, then joins the broadcast set.
// STRICTLY non-blocking: the preamble and replay are enqueued on the client's
// queue and written by its writer goroutine, so a slow (or dead) client can
// never stall the caller — the accept loop services every connection
// immediately. If the hub is already shut down the connection is closed
// instead.
//
// Each attached TCP stream has independent telnet parser state.
func (h *consoleHub) attach(conn net.Conn) {
	c := h.registerLocked(conn, true, true)
	if c == nil {
		_ = conn.Close()
		return
	}

	// Writer: pump queued pty output to the client. c.out is closed by
	// c.close() alongside c.stop; a nil chunk from a closed channel must not
	// be written or busy-loop, so check ok explicitly rather than relying on
	// select's pseudo-random case order between the two simultaneously-ready
	// cases.
	go func() {
		for {
			select {
			case chunk, ok := <-c.out:
				if !ok {
					return
				}
				if _, err := conn.Write(chunk); err != nil {
					h.detach(c)
					return
				}
			case <-c.stop:
				return
			}
		}
	}()

	// Reader: strip/answer IAC via the hub's shared Negotiator, normalize NVT
	// line endings, forward clean bytes toward the pty through gatedWrite
	// (v0.3.0 Phase 3) — so native's keystrokes are queued, not written
	// straight through, while a programmatic turn is active, exactly like an
	// in-process Subscription's Write. Negotiation replies are routed through
	// the client's out queue — NOT written directly — so the writer goroutine
	// stays the connection's single writer and a reply can never interleave
	// into the middle of a data chunk.
	go func() {
		defer h.detach(c)
		// Parser state and Feed's reused output buffer belong to this stream.
		neg := telnet.NewNegotiator()
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				clean := neg.Feed(buf[:n])
				reply := neg.Reply()
				if len(reply) > 0 {
					if !c.enqueue(reply) {
						return // queue jammed — same drop policy as broadcast
					}
				}
				clean = normalizeNVTLineEndings(clean, &c.crPending)
				if len(clean) > 0 {
					if werr := h.gatedWrite(clean); werr != nil {
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()
}

// Subscribe attaches an in-process consumer (wsbridge, and RunExec/ClaimTurn
// internally) to the hub without any socket or telnet negotiation: the
// returned Subscription's Out channel delivers the SAME decoded application
// bytes a TCP client would receive (replay ring first, then live broadcast),
// and Write sends raw bytes toward the pty through the same input-arbitration
// gate a TCP client's decoded keystrokes go through (gatedWrite — queued
// while a turn is active, direct otherwise). Returns nil if the hub is
// already shut down.
func (h *consoleHub) Subscribe() *Subscription {
	return h.subscribe(true)
}

func (h *consoleHub) subscribe(replay bool) *Subscription {
	c := h.registerLocked(nil, false, replay)
	if c == nil {
		return nil
	}
	return &Subscription{hub: h, c: c, Out: c.out, Done: c.stop}
}

// turnClaimPoll is how often ClaimTurn re-checks whether the turn has become
// free while waiting for a prior holder to release. A console turn finishes in
// well under a second (real show commands), so this is fine-grained enough to
// not add perceptible latency while staying cheap.
const turnClaimPoll = 10 * time.Millisecond

// ClaimTurn blocks (bounded by ctx) until it can become the SOLE programmatic
// input-turn holder for this node, then returns a release func the caller
// MUST call exactly once to hand the turn back — typically via `defer release()`.
// While the turn is held, every interactive writer (TCP clients via attach,
// in-process Subscriptions via Write) has its input queued instead of written
// to the pty (see inputTurn/gatedWrite); release() flushes that queue in
// original order.
//
// holder is a short diagnostic label (e.g. "painter:show:R1") used ONLY in the
// force-release warning log below — never surfaced to any protocol/UI (v0.3.0
// §7 decision 5: no protocol-visible "console busy" event).
//
// If ctx is done before the turn is released, ClaimTurn force-releases it and
// logs a visible warning (v0.3.0 §7 decision 4: no silent recovery) — this is
// the "stuck turn" guard: a caller that forgets to release, or whose ctx
// carries a deadline shorter than its own work, cannot wedge every future
// programmatic call or all interactive input forever.
//
// Returns an error (without claiming) if the hub is already shut down or ctx
// is done before a turn becomes available.
func (h *consoleHub) ClaimTurn(ctx context.Context, holder string) (release func(), err error) {
	release, _, err = h.claimTurn(ctx, holder)
	return release, err
}

func (h *consoleHub) claimTurn(ctx context.Context, holder string) (release func(), id uint64, err error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		h.mu.Lock()
		closed := h.closed
		h.mu.Unlock()
		if closed {
			return nil, 0, errHubClosed
		}
		if err := h.lockInput(ctx); err != nil {
			return nil, 0, err
		}
		if err := ctx.Err(); err != nil {
			h.wmu.Unlock()
			return nil, 0, err
		}
		select {
		case <-h.done:
			h.wmu.Unlock()
			return nil, 0, errHubClosed
		default:
		}
		claimed := h.turn.claim(holder)
		id = h.turn.id
		h.wmu.Unlock()
		if claimed {
			break
		}
		select {
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		case <-h.done:
			return nil, 0, errHubClosed
		case <-time.After(turnClaimPoll):
		}
	}

	var once sync.Once
	// watchdog force-releases the turn if the caller's ctx expires (or the hub
	// shuts down) before an explicit release() call — the stuck-turn guard.
	watchdogDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			once.Do(func() {
				count := h.releaseTurn(true)
				log.Printf("consoleHub: FORCE-RELEASED stuck input turn holder=%q reason=%v (queued %d interactive write(s) flushed)", holder, ctx.Err(), count)
			})
		case <-h.done:
			once.Do(func() {
				count := h.releaseTurn(false)
				log.Printf("consoleHub: force-released input turn holder=%q on hub shutdown (queued %d interactive write(s) discarded)", holder, count)
				// Hub is shutting down — the pty is going away too; don't bother
				// flushing to a dead write path.
			})
		case <-watchdogDone:
		}
	}()

	release = func() {
		once.Do(func() {
			close(watchdogDone)
			h.releaseTurn(true)
		})
	}
	return release, id, nil
}

// Holding wmu across release and the complete drain prevents either a new
// claim or interactive input from overtaking queued input.
func (h *consoleHub) releaseTurn(flush bool) int {
	h.wmu.Lock()
	defer h.wmu.Unlock()
	queued := h.turn.release()
	if flush {
		ctx, cancel := context.WithTimeout(context.Background(), consoleWriteTimeout)
		defer cancel()
		for i, p := range queued {
			select {
			case <-h.done:
				return len(queued)
			default:
			}
			if err := h.writePTY(ctx, p); err != nil {
				log.Printf("consoleHub: queued input drain failed after %d of %d write(s): %v", i, len(queued), err)
				break
			}
		}
	}
	return len(queued)
}

func (h *consoleHub) writeTurn(ctx context.Context, id uint64, p []byte) error {
	if err := h.lockInput(ctx); err != nil {
		return err
	}
	defer h.wmu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-h.done:
		return errHubClosed
	default:
	}
	if !h.turn.isActive() || h.turn.id != id {
		return errors.New("node: console input turn expired")
	}
	return h.writePTY(ctx, p)
}

// RunExec claims a turn, drives one scripted exec command (sync prompt, enable
// if needed, `terminal length 0`, run cmd, capture until the prompt returns)
// via internal/consolescript.Session against this hub's OWN decoded output
// stream, and releases the turn before returning — v0.3.0 Phase 4. This is
// what supervisor/internal/server/painter_linux.go's runShow calls instead of
// dialing its own telnet socket: no TCP dial, no separate Negotiator, and the
// command is written to the SAME pty the student's own web/native console
// reads, so it scrolls in their session too (the teaching-win property, §3c).
//
// ctx bounds the whole call (turn claim + the exec sequence); a caller should
// pass a context already carrying the desired timeout (painter_linux.go uses
// runShowTimeout, 8s, mirroring the turn-claim-timeout decision in §7.4).
func (h *consoleHub) RunExec(ctx context.Context, holder, cmd string) (string, error) {
	release, id, err := h.claimTurn(ctx, holder)
	if err != nil {
		return "", err
	}
	defer release()

	sub := h.subscribe(false)
	if sub == nil {
		return "", errHubClosed
	}
	defer sub.Unsubscribe()

	sess := consolescript.New(func(p []byte) error { return h.writeTurn(ctx, id, p) })
	read := func(ctx context.Context) error {
		select {
		case <-sub.Done:
			return errHubClosed
		case chunk, ok := <-sub.Out:
			if !ok {
				return errHubClosed
			}
			sess.Feed(chunk)
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return sess.RunExec(ctx, read, cmd)
}

// normalizeNVTLineEndings collapses telnet NVT Enter sequences to the bare CR
// an IOS pty expects: CR LF -> CR and CR NUL -> CR (RFC 854 — a telnet client
// sends one of those two per Enter depending on binary mode). Forwarding both
// bytes raw made IOS treat CR and LF as SEPARATE line activations — every
// Enter in a native telnet client printed TWO prompts, and the NUL of CR NUL
// echoed as visible garbage ("^@"). Confirmed against a live IOL console:
// "\r\n" -> 2 prompts, "\r\0" -> 2 prompts + ^@, bare "\r" -> 1 prompt (which
// is why the web console — xterm sends bare CR — was never affected; for it
// this pass is a no-op). A LONE LF (no preceding CR) is passed through: it
// only ever arrives from raw/scripted clients, never from an Enter key.
// crPending carries a chunk-final CR into the next call so the pair is
// collapsed even when split across TCP segments.
func normalizeNVTLineEndings(in []byte, crPending *bool) []byte {
	if len(in) == 0 {
		return in
	}
	out := in[:0] // filter in place — output is never longer than input
	for _, b := range in {
		if *crPending {
			*crPending = false
			if b == '\n' || b == 0 {
				continue // the CR already went through; swallow its pair byte
			}
		}
		if b == '\r' {
			*crPending = true
		}
		out = append(out, b)
	}
	return out
}

// detach unregisters and closes one client. Idempotent.
func (h *consoleHub) detach(c *hubClient) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	c.close()
}

// close closes the client connection (if any — in-process subscribers have
// none), stops its writer goroutine, and closes its output channel so an
// in-process Subscription's Out-channel consumer (e.g. a `for chunk := range
// sub.Out` loop) observes the detach and returns instead of blocking forever.
// Output enqueue and closure share c.mu, including negotiation replies.
func (c *hubClient) enqueue(p []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	select {
	case c.out <- p:
		return true
	default:
		return false
	}
}

func (c *hubClient) close() {
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		close(c.stop)
		close(c.out)
		c.mu.Unlock()
		if c.conn != nil {
			_ = c.conn.Close()
		}
	})
}

// shutdown closes every client and marks the hub closed. Idempotent. Called
// when the pty read fails; also safe to call from teardown paths.
func (h *consoleHub) shutdown() {
	h.once.Do(func() {
		h.metrics.Flush()
		h.mu.Lock()
		h.closed = true
		clients := make([]*hubClient, 0, len(h.clients))
		for c := range h.clients {
			clients = append(clients, c)
		}
		h.clients = make(map[*hubClient]struct{})
		h.mu.Unlock()
		for _, c := range clients {
			c.close()
		}
		close(h.done)
	})
}
