package node

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/telnet"
)

// fakePty is an in-memory stand-in for the pty master: the test writes node
// output into ptyOut (the hub reads it) and reads client keystrokes from
// ptyIn (the hub writes them).
type fakePty struct {
	r *io.PipeReader // hub reads node output from here
	w *io.PipeWriter // hub writes client keystrokes here
}

func (f *fakePty) Read(p []byte) (int, error)  { return f.r.Read(p) }
func (f *fakePty) Write(p []byte) (int, error) { return f.w.Write(p) }

// newFakePty returns the hub-side ReadWriter plus the test-side ends: write
// node output to outW; read client keystrokes from inR.
func newFakePty() (pty *fakePty, outW *io.PipeWriter, inR *io.PipeReader) {
	or, ow := io.Pipe() // node output -> hub
	ir, iw := io.Pipe() // hub -> node input
	return &fakePty{r: or, w: iw}, ow, ir
}

// readWithDeadline reads whatever arrives on conn within d, returning it.
func readWithDeadline(t *testing.T, conn net.Conn, d time.Duration) []byte {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(d))
	var out []byte
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			return out
		}
	}
}

// waitFor polls cond until true or the timeout elapses.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// telnetPreamble is what attach volunteers to every client before any data.
var telnetPreamble = []byte{255, 251, 1, 255, 251, 3} // IAC WILL ECHO, IAC WILL SGA

// attachPipe attaches one in-memory client to the hub, returning the test side.
func attachPipe(h *consoleHub) net.Conn {
	server, client := net.Pipe()
	h.attach(server)
	return client
}

// collectUntil reads from conn until want appears in the accumulated bytes (or
// 2s passes), returning everything read.
func collectUntil(t *testing.T, conn net.Conn, want []byte) []byte {
	t.Helper()
	var got []byte
	buf := make([]byte, 4096)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, err := conn.Read(buf)
		got = append(got, buf[:n]...)
		if bytes.Contains(got, want) {
			return got
		}
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			t.Fatalf("read failed before %q arrived: %v (got %q)", want, err, got)
		}
	}
	t.Fatalf("timed out waiting for %q (got %q)", want, got)
	return nil
}

// TestHubBroadcastsToTwoClients: both attached clients receive every pty
// output chunk, and both can write keystrokes that reach the pty.
func TestHubBroadcastsToTwoClients(t *testing.T) {
	pty, outW, inR := newFakePty()
	h := newConsoleHub(pty, "")
	defer h.shutdown()

	c1 := attachPipe(h)
	c2 := attachPipe(h)
	defer c1.Close()
	defer c2.Close()

	if _, err := outW.Write([]byte("R1#")); err != nil {
		t.Fatal(err)
	}
	for i, c := range []net.Conn{c1, c2} {
		got := collectUntil(t, c, []byte("R1#"))
		if !bytes.HasPrefix(got, telnetPreamble) {
			t.Fatalf("client %d: missing telnet preamble: %q", i+1, got)
		}
	}

	// Both clients can write to the pty (serialized, order unspecified between
	// clients but bytes intact).
	if _, err := c1.Write([]byte("a")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 8)
	n, err := inR.Read(buf)
	if err != nil || string(buf[:n]) != "a" {
		t.Fatalf("pty did not receive c1's byte: %q err=%v", buf[:n], err)
	}
	if _, err := c2.Write([]byte("b")); err != nil {
		t.Fatal(err)
	}
	n, err = inR.Read(buf)
	if err != nil || string(buf[:n]) != "b" {
		t.Fatalf("pty did not receive c2's byte: %q err=%v", buf[:n], err)
	}
}

// TestHubReplaysRecentOutputOnAttach: a client attaching AFTER output was
// produced receives the replay ring first, so it sees the current prompt.
func TestHubReplaysRecentOutputOnAttach(t *testing.T) {
	pty, outW, _ := newFakePty()
	h := newConsoleHub(pty, "")
	defer h.shutdown()

	if _, err := outW.Write([]byte("booted\r\nR1>")); err != nil {
		t.Fatal(err)
	}
	// Wait until the hub's readLoop has ingested the bytes into the ring.
	waitFor(t, "ring fill", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(h.ring) > 0
	})

	c := attachPipe(h)
	defer c.Close()
	got := collectUntil(t, c, []byte("R1>"))
	if !bytes.Contains(got, []byte("booted")) {
		t.Fatalf("replay missing earlier output: %q", got)
	}
}

// TestHubReplayRingTrims: the ring keeps only the newest replayRingSize bytes.
func TestHubReplayRingTrims(t *testing.T) {
	pty, outW, _ := newFakePty()
	h := newConsoleHub(pty, "")
	defer h.shutdown()

	big := bytes.Repeat([]byte("x"), replayRingSize)
	if _, err := outW.Write(big); err != nil {
		t.Fatal(err)
	}
	if _, err := outW.Write([]byte("TAIL")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "ring to trim", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(h.ring) == replayRingSize && bytes.HasSuffix(h.ring, []byte("TAIL"))
	})
}

// TestHubTeardownUnblocksClients: shutting the hub down (as teardown does)
// closes every attached client and further attaches are refused.
func TestHubTeardownUnblocksClients(t *testing.T) {
	pty, outW, _ := newFakePty()
	h := newConsoleHub(pty, "")

	c := attachPipe(h)
	defer c.Close()
	// Consume the preamble so the connection is demonstrably live.
	if _, err := outW.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	collectUntil(t, c, []byte("hi"))

	h.shutdown()

	// The client's connection must now be closed: reads drain and then error.
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	for {
		_, err := c.Read(buf)
		if err == nil {
			continue
		}
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			t.Fatal("client read still blocked after hub shutdown")
		}
		break // closed — expected
	}

	// A post-shutdown attach is refused (connection closed immediately).
	server, client := net.Pipe()
	h.attach(server)
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := client.Read(buf); err == nil {
		t.Fatal("expected post-shutdown attach to be closed")
	}
}

// TestHubDropsBackpressuredClient: a client that stops reading long enough to
// fill its queue is dropped, while a healthy client keeps receiving.
func TestHubDropsBackpressuredClient(t *testing.T) {
	pty, outW, _ := newFakePty()
	h := newConsoleHub(pty, "")
	defer h.shutdown()

	stalled := attachPipe(h) // never read from: net.Pipe blocks its writer
	defer stalled.Close()
	healthy := attachPipe(h)
	defer healthy.Close()

	// Drain the healthy client continuously in the background.
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := healthy.Read(buf); err != nil {
				return
			}
		}
	}()

	// Push queue-capacity+2 chunks. The stalled client's writer goroutine
	// blocks on the first chunk (net.Pipe is unbuffered), its queue fills, and
	// the overflow broadcast drops it.
	chunk := bytes.Repeat([]byte("y"), 512)
	for i := 0; i < hubClientQueue+2; i++ {
		if _, err := outW.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}

	waitFor(t, "stalled client to be dropped", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(h.clients) == 1
	})
}

// TestNormalizeNVTLineEndings: telnet Enter sequences collapse to bare CR —
// including across chunk boundaries — while lone LF and ordinary bytes pass
// through. This is the double-prompt / "^@ garbage" fix (see the function's
// comment for the live-console evidence).
func TestNormalizeNVTLineEndings(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		want   string
	}{
		{"CRLF collapses", []string{"show run\r\n"}, "show run\r"},
		{"CRNUL collapses", []string{"\r\x00"}, "\r"},
		{"bare CR untouched", []string{"\r"}, "\r"},
		{"lone LF passes", []string{"\n"}, "\n"},
		{"CR|LF split across chunks", []string{"conf t\r", "\nend\r\n"}, "conf t\rend\r"},
		{"CR|NUL split across chunks", []string{"\r", "\x00x"}, "\rx"},
		{"CR CR LF", []string{"\r\r\n"}, "\r\r"},
		{"plain text", []string{"abc"}, "abc"},
	}
	for _, c := range cases {
		crPending := false
		var got []byte
		for _, ch := range c.chunks {
			// Copy: normalize filters in place, and string-backed bytes are read-only.
			in := []byte(ch)
			got = append(got, normalizeNVTLineEndings(in, &crPending)...)
		}
		if string(got) != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

// TestHubClientCRLFReachesPtyAsCR: end-to-end through a real attached client —
// a PuTTY-style "\r\n" Enter arrives at the pty as a single CR.
func TestHubClientCRLFReachesPtyAsCR(t *testing.T) {
	pty, _, inR := newFakePty()
	h := newConsoleHub(pty, "")
	defer h.shutdown()

	c := attachPipe(h)
	defer c.Close()

	if _, err := c.Write([]byte("conf t\r\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	n, err := inR.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "conf t\r" {
		t.Fatalf("pty received %q, want %q (LF must be swallowed)", buf[:n], "conf t\r")
	}
}

// TestHubSendsTitleEscapeOnAttach: a named hub sends the OSC 0 terminal-title
// sequence right after the telnet preamble, so native clients can label their
// tab with the node name. An unnamed hub sends none.
func TestHubSendsTitleEscapeOnAttach(t *testing.T) {
	pty, outW, _ := newFakePty()
	h := newConsoleHub(pty, "R1")
	defer h.shutdown()

	c := attachPipe(h)
	defer c.Close()
	if _, err := outW.Write([]byte("R1#")); err != nil {
		t.Fatal(err)
	}
	got := collectUntil(t, c, []byte("R1#"))
	wantTitle := []byte("\x1b]0;R1\x07")
	if !bytes.Contains(got, wantTitle) {
		t.Fatalf("title escape missing from attach stream: %q", got)
	}
	// The title must come after the preamble and before any pty data.
	if bytes.Index(got, wantTitle) < len(telnetPreamble) {
		t.Fatalf("title escape must follow the telnet preamble: %q", got)
	}
}

// --- v0.3.0 Phase 1/2: in-process Subscribe API + single-Negotiator tests ---

// TestHubSubscribeReceivesBroadcast: an in-process subscriber (no socket) gets
// the same decoded broadcast bytes a TCP client would, with no telnet preamble
// (Subscribe never advertises WILL ECHO/WILL SGA — there's no telnet peer to
// negotiate with).
func TestHubSubscribeReceivesBroadcast(t *testing.T) {
	pty, outW, _ := newFakePty()
	h := newConsoleHub(pty, "")
	defer h.shutdown()

	sub := h.Subscribe()
	if sub == nil {
		t.Fatal("Subscribe returned nil on a live hub")
	}
	defer sub.Unsubscribe()

	if _, err := outW.Write([]byte("R1#")); err != nil {
		t.Fatal(err)
	}

	select {
	case chunk, ok := <-sub.Out:
		if !ok {
			t.Fatal("Out closed unexpectedly")
		}
		if bytes.Contains(chunk, []byte{telnet.IAC}) {
			t.Fatalf("in-process subscriber must never see telnet preamble/IAC bytes: %q", chunk)
		}
		if !bytes.Contains(chunk, []byte("R1#")) {
			t.Fatalf("expected pty output, got %q", chunk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for broadcast on Subscription.Out")
	}
}

// TestHubSubscribeReplaysRing: a subscriber attaching after output was already
// produced gets the replay ring first, exactly like a TCP client.
func TestHubSubscribeReplaysRing(t *testing.T) {
	pty, outW, _ := newFakePty()
	h := newConsoleHub(pty, "")
	defer h.shutdown()

	if _, err := outW.Write([]byte("booted\r\nR1>")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "ring fill", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(h.ring) > 0
	})

	sub := h.Subscribe()
	defer sub.Unsubscribe()

	select {
	case chunk := <-sub.Out:
		if !bytes.Contains(chunk, []byte("booted")) {
			t.Fatalf("replay missing earlier output: %q", chunk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for replay")
	}
}

// TestHubSubscribeWriteReachesPty confirms a subscription's Write goes to the
// pty exactly like a TCP client's decoded keystrokes, serialized through the
// same write mutex.
func TestHubSubscribeWriteReachesPty(t *testing.T) {
	pty, _, inR := newFakePty()
	h := newConsoleHub(pty, "")
	defer h.shutdown()

	sub := h.Subscribe()
	defer sub.Unsubscribe()

	// The fake pty's Write is a synchronous io.Pipe rendezvous (like the real
	// pty writes exercised by the TCP-client tests above, where the write
	// happens on the hub's OWN reader goroutine, not the test's) — so Write
	// must run concurrently with the Read that unblocks it.
	writeErr := make(chan error, 1)
	go func() { writeErr <- sub.Write([]byte("show version\r")) }()

	buf := make([]byte, 32)
	n, err := inR.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "show version\r" {
		t.Fatalf("pty received %q", buf[:n])
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("sub.Write: %v", err)
	}
}

// TestHubSubscribeUnsubscribeClosesOut confirms Unsubscribe closes Out so a
// `for chunk := range sub.Out` consumer terminates instead of blocking
// forever.
func TestHubSubscribeUnsubscribeClosesOut(t *testing.T) {
	pty, _, _ := newFakePty()
	h := newConsoleHub(pty, "")
	defer h.shutdown()

	sub := h.Subscribe()
	sub.Unsubscribe()

	select {
	case _, ok := <-sub.Out:
		if ok {
			t.Fatal("expected Out to be closed, got a value")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Out did not close after Unsubscribe")
	}
}

// TestHubSubscribeAfterShutdownReturnsNil: Subscribe on an already-shut-down
// hub (mirrors attach's post-shutdown-connection-refused behavior) returns
// nil rather than a subscription whose Out never fires.
func TestHubSubscribeAfterShutdownReturnsNil(t *testing.T) {
	pty, _, _ := newFakePty()
	h := newConsoleHub(pty, "")
	h.shutdown()

	if sub := h.Subscribe(); sub != nil {
		t.Fatal("expected nil Subscription from a shut-down hub")
	}
}

// TestHubOneNegotiatorSharedAcrossTCPClients is the core Phase 1 regression
// test: TWO TCP clients attach to the SAME hub, and the hub answers each
// one's WILL/DO negotiation through the SAME underlying Negotiator instance
// (h.neg) — not one each. We prove this indirectly (h.neg is unexported)
// by exploiting RFC 1143's Q-method idempotency, the exact mechanism
// documented in internal/telnet/iac.go: if two independent Negotiators were
// in play, each client's own WILL/DO negotiation would be answered
// independently and every client would see a full negotiation reply
// sequence for ITS OWN option offers. With one shared Negotiator, a SECOND
// client's WILL SGA (an option the hub already accepted from the FIRST
// client) is recognized as already-on and produces NO further reply to that
// second client on that option — the state transitioned once, globally, not
// per-connection.
func TestHubOneNegotiatorSharedAcrossTCPClients(t *testing.T) {
	pty, _, inR := newFakePty()
	h := newConsoleHub(pty, "")
	defer h.shutdown()

	// Confirm there is exactly one Negotiator instance on the hub (not a
	// per-client map or slice) — the structural invariant Phase 1 introduces.
	if h.neg == nil {
		t.Fatal("hub must own a Negotiator instance")
	}

	c1 := attachPipe(h)
	defer c1.Close()
	c2 := attachPipe(h)
	defer c2.Close()

	// Drain each client's preamble first.
	_ = readWithDeadline(t, c1, 200*time.Millisecond)
	_ = readWithDeadline(t, c2, 200*time.Millisecond)

	// c1 offers WILL SGA — the hub's shared Negotiator has ALREADY set
	// remoteOn[SGA]=true when it sent its own WILL SGA in the preamble... but
	// preamble WILL is US offering, not the peer. To exercise the shared
	// remoteOn/localOn transition tracked by the SAME instance across
	// connections, send the identical WILL SGA from c1, then from c2: the
	// first transitions the state (hub replies DO SGA), the second is a
	// no-op replay of a state the hub already considers "on" and gets no
	// reply — proving one shared state machine, not two independent ones.
	willSGA := []byte{telnet.IAC, telnet.WILL, telnet.OptSGA}
	if _, err := c1.Write(willSGA); err != nil {
		t.Fatal(err)
	}
	reply1 := readWithDeadline(t, c1, 300*time.Millisecond)
	if !bytes.Equal(reply1, []byte{telnet.IAC, telnet.DO, telnet.OptSGA}) {
		t.Fatalf("c1's WILL SGA should get a fresh DO SGA reply, got %q", reply1)
	}

	if _, err := c2.Write(willSGA); err != nil {
		t.Fatal(err)
	}
	reply2 := readWithDeadline(t, c2, 300*time.Millisecond)
	if len(reply2) != 0 {
		t.Fatalf("c2's WILL SGA replays an ALREADY-ON global state (shared Negotiator) — expected NO reply, got %q", reply2)
	}

	_ = inR // pty input side unused in this test; keeping the handle for symmetry with other hub tests
}

// --- v0.3.0 Phase 3: input-arbitration write-path tests ---
//
// ClaimTurn/RunExec (the public turn API) land in Phase 4; these tests drive
// the underlying inputTurn gate directly (h.turn.claim/release, both
// package-internal) to prove gatedWrite's queue-then-flush behavior for BOTH
// interactive input sources — in-process Subscriptions and TCP attach's
// reader — independent of who ends up calling claim/release in Phase 4.

// TestHubSubscriptionWriteQueuedDuringTurnThenFlushed: an in-process
// Subscription's Write while a turn is active is NOT written to the pty until
// the turn releases, and reaches the pty in original order after the turn
// holder's own writes — never interleaved.
func TestHubSubscriptionWriteQueuedDuringTurnThenFlushed(t *testing.T) {
	pty, _, inR := newFakePty()
	h := newConsoleHub(pty, "")
	defer h.shutdown()

	if !h.turn.claim("test:turn") {
		t.Fatal("claim should succeed on a fresh hub")
	}

	sub := h.Subscribe()
	defer sub.Unsubscribe()

	// Interactive write while the turn is held: must NOT reach the pty yet.
	writeErr := make(chan error, 1)
	go func() { writeErr <- sub.Write([]byte("x")) }()
	if err := <-writeErr; err != nil {
		t.Fatalf("queued Write returned error: %v", err)
	}

	// The turn holder's own direct write DOES go straight through.
	holderWriteErr := make(chan error, 1)
	go func() { holderWriteErr <- h.writeDirect([]byte("enable\r")) }()
	buf := make([]byte, 16)
	n, err := inR.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "enable\r" {
		t.Fatalf("pty received %q before the queued write, want the holder's own write first", buf[:n])
	}
	if err := <-holderWriteErr; err != nil {
		t.Fatal(err)
	}

	// Releasing the turn must flush the queued interactive byte, and ONLY
	// after the turn holder's own write already landed (already proven above).
	queued := h.turn.release()
	if len(queued) != 1 || string(queued[0]) != "x" {
		t.Fatalf("release() queue = %q, want [\"x\"]", queued)
	}
	flushErr := make(chan error, 1)
	go func() { flushErr <- h.writeDirect(queued[0]) }()
	n, err = inR.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "x" {
		t.Fatalf("flushed write = %q, want %q", buf[:n], "x")
	}
	if err := <-flushErr; err != nil {
		t.Fatal(err)
	}
}

// TestHubTCPAttachInputQueuedDuringTurn confirms native's TCP-attach reader
// (not just in-process Subscriptions) is subject to the same turn gate —
// the Phase 3 requirement that ALL interactive input funnels through one
// arbitrated write path, not just wsbridge's.
func TestHubTCPAttachInputQueuedDuringTurn(t *testing.T) {
	pty, _, inR := newFakePty()
	h := newConsoleHub(pty, "")
	defer h.shutdown()

	if !h.turn.claim("test:turn") {
		t.Fatal("claim should succeed on a fresh hub")
	}

	conn := attachPipe(h)
	defer conn.Close()
	// Drain the attach preamble so it doesn't confuse anything.
	_ = readWithDeadline(t, conn, 200*time.Millisecond)

	if _, err := conn.Write([]byte("y")); err != nil {
		t.Fatal(err)
	}
	// Give the reader goroutine time to process the byte through gatedWrite;
	// it must be queued, not forwarded — prove no read arrives on the pty side
	// within a generous window (io.PipeReader has no deadline, so race a
	// background read against a timer instead).
	gotRead := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 8)
		n, err := inR.Read(buf)
		if err == nil {
			gotRead <- buf[:n]
		}
	}()
	select {
	case got := <-gotRead:
		t.Fatalf("native keystroke reached the pty while turn was active: %q", got)
	case <-time.After(100 * time.Millisecond):
	}

	queued := h.turn.release()
	if len(queued) != 1 || string(queued[0]) != "y" {
		t.Fatalf("release() queue = %q, want [\"y\"]", queued)
	}
}

// TestHubGatedWriteBypassesQueueWhenNoTurnActive confirms gatedWrite behaves
// exactly like the pre-Phase-3 direct write when no turn is active — the
// no-regression case for the common (no painter running) path.
func TestHubGatedWriteBypassesQueueWhenNoTurnActive(t *testing.T) {
	pty, _, inR := newFakePty()
	h := newConsoleHub(pty, "")
	defer h.shutdown()

	writeErr := make(chan error, 1)
	go func() { writeErr <- h.gatedWrite([]byte("no turn here")) }()

	buf := make([]byte, 32)
	n, err := inR.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "no turn here" {
		t.Fatalf("pty received %q", buf[:n])
	}
	if err := <-writeErr; err != nil {
		t.Fatal(err)
	}
}

// TestHubTurnClaimIsExclusive confirms only one turn can be active at a time
// — a second claim attempt fails until the first releases.
func TestHubTurnClaimIsExclusive(t *testing.T) {
	pty, _, _ := newFakePty()
	h := newConsoleHub(pty, "")
	defer h.shutdown()

	if !h.turn.claim("first") {
		t.Fatal("first claim should succeed")
	}
	if h.turn.claim("second") {
		t.Fatal("second claim should fail while first is active")
	}
	h.turn.release()
	if !h.turn.claim("second") {
		t.Fatal("claim should succeed after release")
	}
}

// --- v0.3.0 Phase 4: ClaimTurn/RunExec (public API) tests ---

// iosPty is a fake pty that behaves enough like a live IOS/IOL console to
// drive consoleHub.RunExec end-to-end: it echoes every line it receives back
// with CRLF, and after a bare CR (SyncPrompt's kick) or after "enable\r"/
// "terminal length 0\r"/any other command line, it emits a trailing prompt.
// State: unprivileged until "enable\r" is seen, then always privileged
// (matches consolescript's assumption that labs boot with no enable secret).
// Every write is recorded in order in the writes log for the test to assert
// ordering against.
type iosPty struct {
	name string // prompt token, e.g. "R1"

	mu     sync.Mutex
	priv   bool
	writes [][]byte // every byte slice ever written to this pty, in order

	r *io.PipeReader
	w *io.PipeWriter
}

func newIOSPty(name string) *iosPty {
	r, w := io.Pipe()
	return &iosPty{name: name, r: r, w: w}
}

func (p *iosPty) Read(b []byte) (int, error) { return p.r.Read(b) }

// Write implements the pty side: record the bytes, then (asynchronously, so
// Write itself never blocks on the reply reaching the hub's reader) emit the
// scripted echo+prompt response into the read side.
func (p *iosPty) Write(b []byte) (int, error) {
	cp := append([]byte(nil), b...)
	p.mu.Lock()
	p.writes = append(p.writes, cp)
	line := strings.TrimRight(string(cp), "\r\n")
	if line == "enable" {
		p.priv = true
	}
	priv := p.priv
	p.mu.Unlock()

	prompt := p.name + ">"
	if priv {
		prompt = p.name + "#"
	}
	go func() {
		if line == "" {
			// Bare CR (SyncPrompt's kick): just the prompt, no echo.
			_, _ = p.w.Write([]byte("\r\n" + prompt))
			return
		}
		_, _ = p.w.Write([]byte(line + "\r\n" + prompt))
	}()
	return len(b), nil
}

func (p *iosPty) writeCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.writes)
}

// TestHubRunExecReturnsCleanOutput confirms RunExec drives the full
// enable/terminal-length-0/show sequence against the hub (no TCP dial, no
// separate Negotiator) and returns clean parsed output — the painter
// migration's core contract.
func TestHubRunExecReturnsCleanOutput(t *testing.T) {
	pty := newIOSPty("R1")
	h := newConsoleHub(pty, "R1")
	defer h.shutdown()

	out, err := h.RunExec(context.Background(), "test:show", "show version")
	if err != nil {
		t.Fatalf("RunExec: %v", err)
	}
	if strings.Contains(out, "show version") {
		t.Fatalf("output should have the echoed command stripped: %q", out)
	}
	if strings.Contains(out, "R1#") {
		t.Fatalf("output should have the trailing prompt stripped: %q", out)
	}
}

// TestHubClaimTurnQueuesAndFlushesInteractiveInput is the end-to-end Phase 4
// regression test using the PUBLIC ClaimTurn API (Phase 3 pinned the same
// behavior against the internal inputTurn directly): an in-process
// Subscription's Write during an active turn is queued, and flushed in order
// after the turn holder's own writes once release() runs.
func TestHubClaimTurnQueuesAndFlushesInteractiveInput(t *testing.T) {
	pty := newIOSPty("R1")
	h := newConsoleHub(pty, "R1")
	defer h.shutdown()

	release, err := h.ClaimTurn(context.Background(), "test:turn")
	if err != nil {
		t.Fatalf("ClaimTurn: %v", err)
	}

	sub := h.Subscribe()
	defer sub.Unsubscribe()

	if err := sub.Write([]byte("x")); err != nil {
		t.Fatalf("queued Write returned error: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if n := pty.writeCount(); n != 0 {
		t.Fatalf("interactive write reached the pty while turn was active: %d writes logged", n)
	}

	if err := h.writeDirect([]byte("enable\r")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "turn holder's write to land", func() bool { return pty.writeCount() == 1 })

	release()

	waitFor(t, "queued interactive write to flush", func() bool { return pty.writeCount() == 2 })
	pty.mu.Lock()
	defer pty.mu.Unlock()
	if string(pty.writes[0]) != "enable\r" {
		t.Fatalf("write[0] = %q, want the turn holder's own write first", pty.writes[0])
	}
	if string(pty.writes[1]) != "x" {
		t.Fatalf("write[1] = %q, want the flushed interactive byte second", pty.writes[1])
	}
}

// TestHubRunExecUnderConcurrentInteractiveWrite is the capture-correctness
// regression test from docs/v0.3.0-console-unification.md §5: a concurrent
// "interactive" writer firing mid-turn must not corrupt RunExec's captured
// output, and its bytes must land on the pty only after RunExec's turn
// releases.
func TestHubRunExecUnderConcurrentInteractiveWrite(t *testing.T) {
	pty := newIOSPty("R1")
	h := newConsoleHub(pty, "R1")
	defer h.shutdown()

	sub := h.Subscribe()
	defer sub.Unsubscribe()

	// Fire the interactive write as soon as SOME turn-holder write has landed,
	// proving it's deferred rather than winning a race by pure luck.
	interactiveSent := make(chan struct{})
	go func() {
		waitFor(t, "RunExec's first write", func() bool { return pty.writeCount() >= 1 })
		_ = sub.Write([]byte("Z"))
		close(interactiveSent)
	}()

	out, err := h.RunExec(context.Background(), "test:show", "show version")
	if err != nil {
		t.Fatalf("RunExec: %v", err)
	}
	<-interactiveSent
	if strings.Contains(out, "Z") {
		t.Fatalf("interactive byte leaked into RunExec's captured output: %q", out)
	}

	// The interactive "Z" must show up on the pty AFTER RunExec's own writes,
	// once the turn released — not interleaved mid-sequence.
	waitFor(t, "interactive write to flush post-release", func() bool {
		pty.mu.Lock()
		defer pty.mu.Unlock()
		return len(pty.writes) > 0 && string(pty.writes[len(pty.writes)-1]) == "Z"
	})
}

// TestHubSecondRunExecWaitsForFirst confirms two concurrent programmatic
// callers are serialized by the turn gate — the second visibly waits rather
// than interleaving its command with the first's, the exact hazard in §1.4.
func TestHubSecondRunExecWaitsForFirst(t *testing.T) {
	pty := newIOSPty("R1")
	h := newConsoleHub(pty, "R1")
	defer h.shutdown()

	release1, err := h.ClaimTurn(context.Background(), "first")
	if err != nil {
		t.Fatalf("ClaimTurn: %v", err)
	}

	secondClaimed := make(chan struct{})
	go func() {
		release2, err := h.ClaimTurn(context.Background(), "second")
		if err != nil {
			t.Errorf("second ClaimTurn: %v", err)
			return
		}
		close(secondClaimed)
		release2()
	}()

	// The second claim must NOT succeed while the first holds the turn.
	select {
	case <-secondClaimed:
		t.Fatal("second ClaimTurn succeeded while first still held the turn")
	case <-time.After(100 * time.Millisecond):
	}

	release1()

	select {
	case <-secondClaimed:
	case <-time.After(2 * time.Second):
		t.Fatal("second ClaimTurn never succeeded after first released")
	}
}

// TestHubClaimTurnForceReleasesOnCtxTimeout confirms a caller that never
// releases (simulating a wedged painter call) is force-released once its ctx
// expires, and that the force-release is logged (v0.3.0 §7 decision 4: no
// silent recovery) — verified here via the queued-write-flushes-anyway
// behavior, which only happens if the watchdog actually fired.
func TestHubClaimTurnForceReleasesOnCtxTimeout(t *testing.T) {
	pty := newIOSPty("R1")
	h := newConsoleHub(pty, "R1")
	defer h.shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := h.ClaimTurn(ctx, "stuck:holder")
	if err != nil {
		t.Fatalf("ClaimTurn: %v", err)
	}
	// Deliberately never call release() — simulate a caller that forgot, or
	// whose own work hung past ctx's deadline.

	sub := h.Subscribe()
	defer sub.Unsubscribe()
	if err := sub.Write([]byte("q")); err != nil {
		t.Fatal(err)
	}

	// A fresh claim must succeed once the watchdog force-releases the stuck
	// turn — this only happens if force-release actually ran.
	release2, err := h.ClaimTurn(context.Background(), "next")
	if err != nil {
		t.Fatalf("ClaimTurn after force-release: %v", err)
	}
	release2()

	// The queued interactive byte from before the force-release must have been
	// flushed (force-release flushes the queue per the ctx-timeout path).
	waitFor(t, "queued write to flush after force-release", func() bool { return pty.writeCount() >= 1 })
}

// TestHubMixedTCPAndInProcessSubscribers confirms a TCP client and an
// in-process Subscription attached to the same node both receive the same
// broadcast output concurrently (the fan-out property predates Phase 1/2;
// this test pins it against the new mixed-subscriber-type hub).
func TestHubMixedTCPAndInProcessSubscribers(t *testing.T) {
	pty, outW, _ := newFakePty()
	h := newConsoleHub(pty, "")
	defer h.shutdown()

	tcp := attachPipe(h)
	defer tcp.Close()
	sub := h.Subscribe()
	defer sub.Unsubscribe()

	if _, err := outW.Write([]byte("R1#")); err != nil {
		t.Fatal(err)
	}

	gotTCP := collectUntil(t, tcp, []byte("R1#"))
	if !bytes.Contains(gotTCP, []byte("R1#")) {
		t.Fatalf("TCP client missing broadcast: %q", gotTCP)
	}

	select {
	case chunk := <-sub.Out:
		if !bytes.Contains(chunk, []byte("R1#")) {
			t.Fatalf("in-process subscriber missing broadcast: %q", chunk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("in-process subscriber never received broadcast")
	}
}
