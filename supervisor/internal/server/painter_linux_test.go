//go:build linux

package server

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
	"github.com/rohanpunj/iolbox/supervisor/internal/node"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
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

	mu       sync.Mutex
	priv     bool
	outputs  map[string]string
	commands []string

	r *io.PipeReader
	w *io.PipeWriter
}

func newFakeIOSPty(name string) *fakeIOSPty {
	r, w := io.Pipe()
	return &fakeIOSPty{name: name, r: r, w: w, outputs: make(map[string]string)}
}

func (p *fakeIOSPty) Read(b []byte) (int, error) { return p.r.Read(b) }

func (p *fakeIOSPty) Write(b []byte) (int, error) {
	line := strings.TrimRight(string(b), "\r\n")
	p.mu.Lock()
	if line == "enable" {
		p.priv = true
	}
	priv := p.priv
	p.commands = append(p.commands, line)
	output := p.outputs[line]
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
		payload := line + "\r\n"
		if output != "" {
			payload += strings.TrimRight(output, "\r\n") + "\r\n"
		}
		_, _ = p.w.Write([]byte(payload + prompt))
	}()
	return len(b), nil
}

// newTestRunningNode builds a loadedLab with a single running IOL node whose
// console hub wraps a fakeIOSPty, so s.runShow can be exercised end-to-end
// without a real spawned node — verifying the v0.3.0 Phase 4 migration off
// dialConsole/consoleSession still parses a `show` command correctly through
// node.Process.RunExec -> consoleHub.RunExec.
func newTestRunningNode(t *testing.T, nodeID int, name string) *loadedLab {
	ll, _ := newTestRunningNodeWithPty(t, nodeID, name)
	return ll
}

func newTestRunningNodeWithPty(t *testing.T, nodeID int, name string) (*loadedLab, *fakeIOSPty) {
	t.Helper()
	doc := &lab.Lab{ID: "test-lab", Nodes: []lab.Node{{ID: nodeID, Name: name, Kind: lab.KindIOL}}}
	pty := newFakeIOSPty(name)
	return newTestRunningLab(t, doc, pty), pty
}

func newTestRunningNodeForDoc(t *testing.T, doc *lab.Lab) (*loadedLab, *fakeIOSPty) {
	t.Helper()
	if len(doc.Nodes) == 0 {
		t.Fatal("test document has no nodes")
	}
	pty := newFakeIOSPty(doc.Nodes[0].Name)
	return newTestRunningLab(t, doc, pty), pty
}

func newTestRunningLab(t *testing.T, doc *lab.Lab, pty *fakeIOSPty) *loadedLab {
	t.Helper()
	ll := newLoadedLab(doc, t.TempDir())

	m := node.NewMachine(nil)
	if !m.To(node.StateStarting) || !m.To(node.StateRunning) {
		t.Fatal("failed to drive test machine to running")
	}
	proc := node.NewProcessForTest(pty, doc.Nodes[0].Name)

	ll.mu.Lock()
	nodeID := doc.Nodes[0].ID
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

const nodeMACShowInterfacesCommand = "show interfaces | include ^[A-Za-z].* is |Hardware is .*address is"

const nodeMACShowInterfacesFixture = `
Ethernet0/0 is administratively down, line protocol is down
  Hardware is AmdP2, address is aabb.cc00.0801 (bia aabb.cc00.0800)
Ethernet0/1 is up, line protocol is up
  Hardware is AmdP2, address is aabb.cc00.0802 (bia aabb.cc00.0802)
Serial0/0 is down, line protocol is down
  Hardware is HDLC, no IEEE address is reported
Loopback0 is up, line protocol is up
  Hardware is Loopback, address is 0200.0000.0001
`

func iolNodeMAC(t *testing.T, result any, index int) protocol.NodeMAC {
	t.Helper()
	macsResult, ok := result.(protocol.NodeMACsResult)
	if !ok {
		t.Fatalf("result type = %T, want protocol.NodeMACsResult", result)
	}
	if index < 0 || index >= len(macsResult.MACs) {
		t.Fatalf("MAC row index %d out of range for %+v", index, macsResult.MACs)
	}
	return macsResult.MACs[index]
}

func TestNodeMACsReadsIOLShowInterfaces(t *testing.T) {
	eth, serial := 1, 1
	doc := &lab.Lab{
		ID: "macs-test",
		Nodes: []lab.Node{{
			ID:       1,
			Name:     "R1",
			Kind:     lab.KindIOL,
			Ethernet: &eth,
			Serial:   &serial,
		}},
	}
	ll, pty := newTestRunningNodeForDoc(t, doc)
	pty.mu.Lock()
	pty.outputs[nodeMACShowInterfacesCommand] = nodeMACShowInterfacesFixture
	pty.mu.Unlock()
	s := &Server{}
	s.mu.Lock()
	s.lab = ll
	s.mu.Unlock()

	result, err := s.handleNodeMACs(json.RawMessage(`{"node":1}`))
	if err != nil {
		t.Fatalf("handleNodeMACs: %v", err)
	}
	macsResult, ok := result.(protocol.NodeMACsResult)
	if !ok {
		t.Fatalf("result type = %T, want protocol.NodeMACsResult", result)
	}
	if len(macsResult.MACs) != 8 {
		t.Fatalf("len(MACs) = %d, want 8: %+v", len(macsResult.MACs), macsResult.MACs)
	}
	if got := macsResult.MACs[0]; got.Interface != "e0/0" || got.MAC != "aa:bb:cc:00:08:01" || got.Source != "read" || got.State != "known" {
		t.Errorf("admin-down e0/0 row = %+v, want known current address with read source", got)
	}
	if got := macsResult.MACs[1]; got.Interface != "e0/1" || got.MAC != "aa:bb:cc:00:08:02" || got.State != "known" {
		t.Errorf("e0/1 row = %+v, want known address", got)
	}
	if got := macsResult.MACs[2]; got.State != "unknown" || got.Reason != "hardware address not reported by IOS" {
		t.Errorf("missing Ethernet row = %+v, want per-row unknown", got)
	}
	if got := macsResult.MACs[4]; got.Interface != "s0/0" || got.State != "unknown" || got.Reason != "interface has no IEEE MAC address" {
		t.Errorf("Serial row = %+v, want preserved no-MAC row", got)
	}
	for _, row := range macsResult.MACs {
		if strings.Contains(row.Interface, "Loopback") || strings.Contains(row.Interface, "show") || strings.Contains(row.Interface, "R1#") {
			t.Errorf("console/logical interface leaked into result: %+v", row)
		}
	}

	pty.mu.Lock()
	defer pty.mu.Unlock()
	count := 0
	for _, command := range pty.commands {
		if command == nodeMACShowInterfacesCommand {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("filtered show command count = %d, want 1; commands=%v", count, pty.commands)
	}
}

func TestNodeMACsIOLNotRunning(t *testing.T) {
	eth, serial := 1, 1
	doc := &lab.Lab{ID: "macs-stopped", Nodes: []lab.Node{{ID: 1, Name: "R1", Kind: lab.KindIOL, Ethernet: &eth, Serial: &serial}}}
	ll := newLoadedLab(doc, t.TempDir())
	ll.nodes[1] = &nodeRuntime{id: 1, machine: node.NewMachine(nil)}
	s := &Server{}
	s.mu.Lock()
	s.lab = ll
	s.mu.Unlock()

	result, err := s.handleNodeMACs(json.RawMessage(`{"node":1}`))
	if err != nil {
		t.Fatalf("handleNodeMACs: %v", err)
	}
	macsResult := result.(protocol.NodeMACsResult)
	if len(macsResult.MACs) != 8 {
		t.Fatalf("len(MACs) = %d, want 8", len(macsResult.MACs))
	}
	for _, row := range macsResult.MACs {
		if row.State != "unknown" || row.Reason != "node not running" || row.MAC != "" {
			t.Errorf("stopped row = %+v, want honest unknown", row)
		}
	}
}

func TestNodeMACsIOLConsoleUnavailableDoesNotUseLearnedFallback(t *testing.T) {
	eth := 1
	doc := &lab.Lab{
		ID:    "macs-console-failure",
		Nodes: []lab.Node{{ID: 1, Name: "R1", Kind: lab.KindIOL, Ethernet: &eth}},
		Links: []lab.Link{{ID: 1, Endpoints: []lab.Endpoint{{Node: 1, Interface: "e0/0"}, {Node: 2, Interface: "e0/0"}}}},
	}
	ll, pty := newTestRunningNodeForDoc(t, doc)
	if err := ll.nodes[1].proc.Stop(); err != nil {
		t.Fatalf("stop fake console: %v", err)
	}
	s := &Server{}
	s.mu.Lock()
	s.lab = ll
	s.mu.Unlock()

	result, err := s.handleNodeMACs(json.RawMessage(`{"node":1}`))
	if err != nil {
		t.Fatalf("handleNodeMACs: %v", err)
	}
	macsResult := result.(protocol.NodeMACsResult)
	for _, row := range macsResult.MACs {
		if row.State != "unknown" || row.Reason != "MAC unavailable (console busy or unreachable)" || row.MAC != "" || row.Source != "" {
			t.Errorf("console failure row = %+v, want no learned fallback", row)
		}
	}
	pty.mu.Lock()
	defer pty.mu.Unlock()
	for _, command := range pty.commands {
		if command == nodeMACShowInterfacesCommand {
			t.Errorf("show command unexpectedly completed after console teardown")
		}
	}
}
