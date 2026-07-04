package consolescript

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestHasPromptSuffix(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		wantPrompt string
		wantPriv   bool
		wantOK     bool
	}{
		{"user exec", "R1>", "R1>", false, true},
		{"priv exec", "R1#", "R1#", true, true},
		{"config mode", "R1(config)#", "R1(config)#", true, true},
		{"trailing whitespace", "R1# \r\n", "R1#", true, true},
		{"multi-line, last line matters", "some output\nR1#", "R1#", true, true},
		{"empty", "", "", false, false},
		{"no trailing prompt char", "hello world", "", false, false},
		{"prompt-looking token but has space before it", "abc R1#", "", false, false},
		{"invalid char", "R1$#", "", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prompt, priv, ok := HasPromptSuffix(c.in)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if !ok {
				return
			}
			if prompt != c.wantPrompt || priv != c.wantPriv {
				t.Fatalf("got (%q,%v), want (%q,%v)", prompt, priv, c.wantPrompt, c.wantPriv)
			}
		})
	}
}

func TestCleanShowOutput(t *testing.T) {
	raw := "show version\r\nCisco IOS Software\r\nUptime is 1 day\r\nR1#"
	got := CleanShowOutput(raw, "show version")
	want := "Cisco IOS Software\nUptime is 1 day"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// fakeConsole is an in-memory stand-in for a telnet-decoded console stream:
// Write appends to a log the test can inspect; the ReadFunc pulls
// pre-scripted response chunks in order, feeding them into the Session.
type fakeConsole struct {
	sess    *Session
	written [][]byte
	chunks  [][]byte // scripted responses, consumed in order by read()
}

func (f *fakeConsole) write(p []byte) error {
	cp := append([]byte(nil), p...)
	f.written = append(f.written, cp)
	return nil
}

func (f *fakeConsole) read(ctx context.Context) error {
	if len(f.chunks) == 0 {
		return errors.New("fakeConsole: no more scripted chunks")
	}
	chunk := f.chunks[0]
	f.chunks = f.chunks[1:]
	f.sess.Feed(chunk)
	return nil
}

func TestSessionSyncPrompt(t *testing.T) {
	f := &fakeConsole{}
	f.sess = New(f.write)
	f.chunks = [][]byte{[]byte("\r\nR1>")}

	priv, err := f.sess.SyncPrompt(context.Background(), f.read)
	if err != nil {
		t.Fatalf("SyncPrompt: %v", err)
	}
	if priv {
		t.Fatalf("expected non-privileged prompt")
	}
	if len(f.written) != 1 || !bytes.Equal(f.written[0], []byte("\r")) {
		t.Fatalf("expected a bare CR write to kick the sync, got %v", f.written)
	}
}

// TestSessionRunExec exercises the full enable -> terminal length 0 -> show
// sequence end-to-end against a scripted fake, mirroring the exact behavior
// consoleSession.runShow had inline before the Phase 0 extraction.
func TestSessionRunExec(t *testing.T) {
	f := &fakeConsole{}
	f.sess = New(f.write)
	// The Session's buffer is a running accumulator that is only Reset()
	// explicitly between the "enable" and "terminal length 0" phases (see
	// RunExec) — critically, SyncPrompt.HasPromptSuffix matches ANY trailing
	// prompt char ('>' or '#'), not a *specific* target prompt, so once the
	// first bare-CR sync sees a (non-priv) "R1>" tail, the very next
	// SyncPrompt call (after writing "enable\r", with NO reset in between)
	// already satisfies its own exit condition from that same stale buffer
	// tail without reading anything new. This is the original inline
	// consoleSession behavior, unchanged by the Phase 0 extraction — so only
	// ONE read is consumed across the first two sync phases combined.
	f.chunks = [][]byte{
		[]byte("\r\nR1>"), // response to the initial bare CR (unprivileged) — also satisfies the immediately-following enable-sync's stale-buffer check
		[]byte("\r\nR1#"), // response after Reset()+"terminal length 0\r"+bare CR
		[]byte("show clock\r\n*12:00:00.000 UTC\r\nR1#"), // response after Reset()+the show command
	}

	out, err := f.sess.RunExec(context.Background(), f.read, "show clock")
	if err != nil {
		t.Fatalf("RunExec: %v", err)
	}
	want := "*12:00:00.000 UTC"
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}

	// Verify the write sequence: CR, "enable\r", CR (next syncPrompt), "terminal length 0\r", CR, "show clock\r".
	wantWrites := []string{"\r", "enable\r", "\r", "terminal length 0\r", "\r", "show clock\r"}
	if len(f.written) != len(wantWrites) {
		t.Fatalf("write count = %d, want %d: %q", len(f.written), len(wantWrites), f.written)
	}
	for i, w := range wantWrites {
		if string(f.written[i]) != w {
			t.Fatalf("write[%d] = %q, want %q", i, f.written[i], w)
		}
	}
}

// TestSessionRunExecAlreadyPrivileged confirms the enable step is skipped
// when the console is already at a privileged prompt.
func TestSessionRunExecAlreadyPrivileged(t *testing.T) {
	f := &fakeConsole{}
	f.sess = New(f.write)
	f.chunks = [][]byte{
		[]byte("R1#"),       // already privileged
		[]byte("R1#"),       // after terminal length 0
		[]byte("ok\r\nR1#"), // after show command
	}
	out, err := f.sess.RunExec(context.Background(), f.read, "show version")
	if err != nil {
		t.Fatalf("RunExec: %v", err)
	}
	if out != "ok" {
		t.Fatalf("got %q", out)
	}
	// No "enable\r" write since we started privileged.
	for _, w := range f.written {
		if string(w) == "enable\r" {
			t.Fatalf("unexpected enable write: %q", f.written)
		}
	}
}

// TestSessionRunExecConfigModeUsesDoPrefix confirms that when the shared
// console is left in config mode ("R1(config)#"), RunExec runs exec commands
// with a "do " prefix so they execute WITHOUT leaving config mode — instead of
// failing with "% Invalid input". This is the fix for the STP painter reporting
// "no VLANs" when a user had left the console in config mode. RunExec must NOT
// send end/exit/enable (config mode is privileged and must stay intact for the
// interactive user sharing the arbitrated console).
func TestSessionRunExecConfigModeUsesDoPrefix(t *testing.T) {
	f := &fakeConsole{}
	f.sess = New(f.write)
	f.chunks = [][]byte{
		[]byte("R1(config)#"),                                      // initial sync: config mode
		[]byte("R1(config)#"),                                      // after "do terminal length 0"
		[]byte("do show spanning-tree\r\nVLAN0001\r\nR1(config)#"), // after the do-prefixed show
	}
	out, err := f.sess.RunExec(context.Background(), f.read, "show spanning-tree")
	if err != nil {
		t.Fatalf("RunExec: %v", err)
	}
	if out != "VLAN0001" {
		t.Fatalf("got %q, want %q", out, "VLAN0001")
	}
	var sawDoShow, sawDoTermLen bool
	for _, w := range f.written {
		switch string(w) {
		case "do show spanning-tree\r":
			sawDoShow = true
		case "do terminal length 0\r":
			sawDoTermLen = true
		case "end\r", "exit\r", "enable\r":
			t.Fatalf("RunExec must not leave config mode, but wrote %q", w)
		}
	}
	if !sawDoShow || !sawDoTermLen {
		t.Fatalf("expected do-prefixed commands, writes = %q", f.written)
	}
}

// TestIsConfigPrompt pins the config-mode detection used to pick the `do` prefix.
func TestIsConfigPrompt(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{
		{"R1#", false},
		{"R1>", false},
		{"R1(config)#", true},
		{"R1(config-if)#", true},
		{"R1(config-router)#", true},
		{"SW1(config-vlan)#", true},
	} {
		if got := IsConfigPrompt(c.in); got != c.want {
			t.Errorf("IsConfigPrompt(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestSessionSyncPromptReadError confirms a read failure aborts the sync loop
// with the underlying error instead of hanging.
func TestSessionSyncPromptReadError(t *testing.T) {
	f := &fakeConsole{}
	f.sess = New(f.write)
	// No chunks queued: read() immediately errors.
	_, err := f.sess.SyncPrompt(context.Background(), f.read)
	if err == nil {
		t.Fatal("expected error from exhausted read")
	}
}

// TestSessionRunExecCtxTimeout confirms a cancelled/expired context aborts
// RunExec rather than looping forever when the console never produces a
// prompt.
func TestSessionRunExecCtxTimeout(t *testing.T) {
	f := &fakeConsole{}
	f.sess = New(f.write)
	f.chunks = [][]byte{[]byte("no prompt here, just noise")}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// read() will be called once (consuming the only chunk), then ctx.Err()
	// becomes non-nil on the next loop iteration since chunks are exhausted
	// and the deadline has passed — RunExec must return, not hang.
	done := make(chan struct{})
	go func() {
		_, _ = f.sess.RunExec(ctx, func(c context.Context) error {
			<-c.Done()
			return c.Err()
		}, "show version")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunExec did not respect context timeout")
	}
}
