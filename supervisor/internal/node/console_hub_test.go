package node

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
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
