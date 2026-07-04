package node

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"time"

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
//   - v0.3.0 Phase 1: the hub owns exactly ONE telnet.Negotiator for the
//     whole node (not one per attached TCP connection). Every TCP-attached
//     connection's raw bytes are fed through this single shared Negotiator
//     before being forwarded to the pty; negotiation replies are written back
//     to whichever connection triggered them. This is safe because IOL/VPCS
//     telnet consoles are the ONLY thing that ever sees the negotiated
//     wire-protocol view; every attached TCP peer is negotiating the SAME
//     logical option set (echo/SGA) against the SAME logical console, so one
//     shared Q-method state machine is the structurally-correct model — see
//     docs/v0.3.0-console-unification.md §2.2/§3(a). In-process subscribers
//     (wsbridge, programmatic RunExec — Phase 2/4) never touch the
//     Negotiator at all: they get pre-decoded application bytes directly from
//     the broadcast, and their writes go straight to the pty write path
//     unchanged, exactly as if they were a TCP client whose telnet layer the
//     hub already peeled off.
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
	name string

	mu      sync.Mutex
	clients map[*hubClient]struct{}
	ring    []byte
	closed  bool

	// wmu serializes all client->pty writes.
	wmu sync.Mutex

	// neg is the SOLE telnet.Negotiator for this node (v0.3.0 Phase 1): every
	// TCP-attached connection's inbound bytes are fed through it (nmu
	// serializes access, since multiple TCP readers could otherwise race the
	// same state machine). In-process subscribers never touch it.
	nmu sync.Mutex
	neg *telnet.Negotiator

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

// inputTurn is the hub's exclusive-input-turn gate (v0.3.0 Phase 4). Only one
// programmatic caller (painter's runShow today; any future scripted driver)
// may hold it at a time per node. While held, interactive input from every
// other source is queued (never dropped, never written straight to the pty)
// and flushed in original order the instant the turn releases.
type inputTurn struct {
	mu     sync.Mutex // guards the fields below; held only briefly per operation
	active bool
	holder string // e.g. "painter:show:R1" — for the force-release warning log
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
// (each up to 4096 bytes). A client this far behind is dropped.
const hubClientQueue = 64

// hubClient is one attached subscriber. Exactly one of conn (a real TCP
// connection — native telnet) or inProcess (true — wsbridge/programmatic, no
// socket) applies. TCP clients get their bytes telnet-decoded through the
// hub's single shared Negotiator before being forwarded to the pty; in-process
// subscribers hand the hub already-clean bytes via Subscription.Write.
type hubClient struct {
	conn net.Conn // nil for in-process subscribers
	out  chan []byte
	stop chan struct{}
	once sync.Once
	// crPending tracks a CR seen at the END of the previous input chunk, so the
	// telnet NVT line-ending normalization (CR LF / CR NUL -> CR) works across
	// chunk boundaries. Touched only by this client's reader goroutine (TCP) or
	// by Subscription.Write (in-process) — never both, since a client is one or
	// the other.
	crPending bool
}

// Subscription is the in-process handle an attach-without-a-socket consumer
// (wsbridge, and later programmatic RunExec/ClaimTurn) gets from
// consoleHub.Subscribe. It mirrors hubClient's output/replay/backpressure
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
	if h.turn.isActive() {
		h.turn.enqueue(p)
		return nil
	}
	return h.writeDirect(p)
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
	_, err := h.pty.Write(p)
	h.wmu.Unlock()
	return err
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
		clients: make(map[*hubClient]struct{}),
		neg:     telnet.NewNegotiator(),
		done:    make(chan struct{}),
	}
	go h.readLoop()
	return h
}

// readLoop is the single owner of pty reads: it appends each chunk to the
// replay ring and enqueues it to every client, dropping clients whose queue is
// full. Exits (and shuts the hub down) when the pty read errors — node exit or
// teardown closing the pty master.
func (h *consoleHub) readLoop() {
	buf := make([]byte, 4096)
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
		n, err := h.pty.Read(buf)
		if n > 0 {
			// Copy out of the reused read buffer before it escapes to queues.
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			h.broadcast(chunk)
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
		select {
		case c.out <- chunk:
		default:
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
func (h *consoleHub) registerLocked(conn net.Conn, wantTelnetPreamble bool) *hubClient {
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
	if h.name != "" {
		c.out <- []byte("\x1b]0;" + h.name + "\x07")
	}
	if len(h.ring) > 0 {
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
// v0.3.0 Phase 1: inbound bytes from conn are fed through the hub's ONE
// shared Negotiator (h.neg, serialized by h.nmu) instead of a fresh
// per-connection Negotiator — see the consoleHub doc comment.
func (h *consoleHub) attach(conn net.Conn) {
	c := h.registerLocked(conn, true)
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
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				h.nmu.Lock()
				clean := h.neg.Feed(buf[:n])
				reply := h.neg.Reply()
				h.nmu.Unlock()
				if len(reply) > 0 {
					select {
					case c.out <- reply:
					default:
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

// Subscribe attaches an in-process consumer (wsbridge, and later programmatic
// RunExec/ClaimTurn — v0.3.0 Phases 2/4) to the hub without any socket or
// telnet negotiation: the returned Subscription's Out channel delivers the
// SAME decoded application bytes a TCP client would receive (replay ring
// first, then live broadcast), and Write sends raw bytes straight to the pty
// under the hub's write mutex, exactly like a TCP client's already-decoded
// keystrokes. Returns nil if the hub is already shut down.
func (h *consoleHub) Subscribe() *Subscription {
	c := h.registerLocked(nil, false)
	if c == nil {
		return nil
	}
	return &Subscription{hub: h, c: c, Out: c.out}
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
	for {
		h.mu.Lock()
		closed := h.closed
		h.mu.Unlock()
		if closed {
			return nil, errHubClosed
		}
		if h.turn.claim(holder) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-h.done:
			return nil, errHubClosed
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
				queued := h.turn.release()
				log.Printf("consoleHub: FORCE-RELEASED stuck input turn holder=%q reason=%v (queued %d interactive write(s) flushed)", holder, ctx.Err(), len(queued))
				h.flushQueued(queued)
			})
		case <-h.done:
			once.Do(func() {
				queued := h.turn.release()
				log.Printf("consoleHub: force-released input turn holder=%q on hub shutdown (queued %d interactive write(s) discarded)", holder, len(queued))
				// Hub is shutting down — the pty is going away too; don't bother
				// flushing to a dead write path.
			})
		case <-watchdogDone:
		}
	}()

	release = func() {
		once.Do(func() {
			close(watchdogDone)
			queued := h.turn.release()
			h.flushQueued(queued)
		})
	}
	return release, nil
}

// flushQueued writes queued interactive bytes to the pty in original order,
// AFTER the turn's own output has settled — this is the ordering guarantee
// docs/v0.3.0-console-unification.md §5 calls out ("queued interactive bytes
// go out after the turn's own output settles, not interleaved mid-turn").
// Uses writeDirect (not gatedWrite) since the turn is already released by the
// time this runs; re-entering gatedWrite here would be correct too (no turn
// active) but writeDirect makes the "no re-queueing" invariant explicit.
func (h *consoleHub) flushQueued(queued [][]byte) {
	for _, p := range queued {
		if err := h.writeDirect(p); err != nil {
			return
		}
	}
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
	release, err := h.ClaimTurn(ctx, holder)
	if err != nil {
		return "", err
	}
	defer release()

	sub := h.Subscribe()
	if sub == nil {
		return "", errHubClosed
	}
	defer sub.Unsubscribe()

	sess := consolescript.New(h.writeDirect)
	read := func(ctx context.Context) error {
		select {
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
// Idempotent. Safe to call after the client has already been removed from
// h.clients (detach/shutdown both remove-then-close), so no further send on
// c.out can race this close.
func (c *hubClient) close() {
	c.once.Do(func() {
		close(c.stop)
		if c.conn != nil {
			_ = c.conn.Close()
		}
		close(c.out)
	})
}

// shutdown closes every client and marks the hub closed. Idempotent. Called
// when the pty read fails; also safe to call from teardown paths.
func (h *consoleHub) shutdown() {
	h.once.Do(func() {
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
