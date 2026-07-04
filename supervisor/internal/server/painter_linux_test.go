//go:build linux

package server

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
	"github.com/rohanpunj/iolbox/supervisor/internal/node"
)

// fakeIOSPty is a fake pty that behaves enough like a live IOS/IOL console to
// drive runShow end-to-end through node.Process.RunExec/consoleHub.RunExec:
// it echoes every line it receives back with CRLF, and after a bare CR
// (SyncPrompt's kick) or after "enable\r"/"terminal length 0\r"/any other
// command line, it emits a trailing prompt. State: unprivileged until
// "enable\r" is seen, then always privileged (matches consolescript's
// assumption that labs boot with no enable secret).
type fakeIOSPty struct {
	name string // prompt token, e.g. "R1"

	mu   sync.Mutex
	priv bool

	r *io.PipeReader
	w *io.PipeWriter
}

func newFakeIOSPty(name string) *fakeIOSPty {
	r, w := io.Pipe()
	return &fakeIOSPty{name: name, r: r, w: w}
}

func (p *fakeIOSPty) Read(b []byte) (int, error) { return p.r.Read(b) }

func (p *fakeIOSPty) Write(b []byte) (int, error) {
	line := strings.TrimRight(string(b), "\r\n")
	p.mu.Lock()
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
			_, _ = p.w.Write([]byte("\r\n" + prompt))
			return
		}
		_, _ = p.w.Write([]byte(line + "\r\n" + prompt))
	}()
	return len(b), nil
}

// newTestRunningNode builds a loadedLab with a single running IOL node whose
// console hub wraps a fakeIOSPty, so s.runShow can be exercised end-to-end
// without a real spawned node — verifying the v0.3.0 Phase 4 migration off
// dialConsole/consoleSession still parses a `show` command correctly through
// node.Process.RunExec -> consoleHub.RunExec.
func newTestRunningNode(t *testing.T, nodeID int, name string) *loadedLab {
	t.Helper()
	doc := &lab.Lab{ID: "test-lab", Nodes: []lab.Node{{ID: nodeID, Name: name, Kind: lab.KindIOL}}}
	ll := newLoadedLab(doc, t.TempDir())

	m := node.NewMachine(nil)
	if !m.To(node.StateStarting) || !m.To(node.StateRunning) {
		t.Fatal("failed to drive test machine to running")
	}
	proc := node.NewProcessForTest(newFakeIOSPty(name), name)

	ll.mu.Lock()
	ll.nodes[nodeID] = &nodeRuntime{id: nodeID, machine: m, proc: proc}
	ll.mu.Unlock()
	return ll
}

// TestRunShowReturnsCleanOutput confirms runShow (now routed through
// node.Process.RunExec / consoleHub.RunExec — v0.3.0 Phase 4, no dialConsole/
// consoleSession) still drives the full enable/terminal-length-0/show
// sequence and returns output with the echo and trailing prompt stripped.
func TestRunShowReturnsCleanOutput(t *testing.T) {
	s := &Server{}
	ll := newTestRunningNode(t, 1, "R1")

	out, err := s.runShow(context.Background(), ll, 1, "show version")
	if err != nil {
		t.Fatalf("runShow: %v", err)
	}
	if strings.Contains(out, "show version") {
		t.Fatalf("output should have the echoed command stripped: %q", out)
	}
	if strings.Contains(out, "R1#") {
		t.Fatalf("output should have the trailing prompt stripped: %q", out)
	}
}

// TestRunShowNotRunningNode confirms runShow still fails cleanly (no panic, a
// protocol.CodeNotLoaded-class error) for a node that isn't running — the
// existing pre-Phase-4 contract callers depend on.
func TestRunShowNotRunningNode(t *testing.T) {
	s := &Server{}
	doc := &lab.Lab{ID: "test-lab", Nodes: []lab.Node{{ID: 1, Name: "R1", Kind: lab.KindIOL}}}
	ll := newLoadedLab(doc, t.TempDir())
	m := node.NewMachine(nil) // stays StateStopped

	ll.mu.Lock()
	ll.nodes[1] = &nodeRuntime{id: 1, machine: m}
	ll.mu.Unlock()

	if _, err := s.runShow(context.Background(), ll, 1, "show version"); err == nil {
		t.Fatal("expected an error for a non-running node")
	}
}

// TestRunShowUnderConcurrentInteractiveWrite pins the teaching/safety property
// the Phase 4 migration exists for: a concurrent interactive write (standing
// in for a student typing in their open web/native console) does not corrupt
// runShow's captured output — it's deferred by the turn gate instead.
func TestRunShowUnderConcurrentInteractiveWrite(t *testing.T) {
	s := &Server{}
	ll := newTestRunningNode(t, 1, "R1")

	nr := ll.get(1)
	sub := nr.proc.Subscribe()
	if sub == nil {
		t.Fatal("expected a subscription on the test node's hub")
	}
	defer sub.Unsubscribe()

	go func() {
		time.Sleep(5 * time.Millisecond) // let runShow's turn claim land first
		_ = sub.Write([]byte("Z"))
	}()

	out, err := s.runShow(context.Background(), ll, 1, "show version")
	if err != nil {
		t.Fatalf("runShow: %v", err)
	}
	if strings.Contains(out, "Z") {
		t.Fatalf("interactive byte leaked into runShow's captured output: %q", out)
	}
}
