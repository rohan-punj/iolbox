package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
	"github.com/rohanpunj/iolbox/supervisor/internal/node"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
	"github.com/rohanpunj/iolbox/supervisor/internal/tool"
)

func TestMergePCStateWritesOnlyPCConfig(t *testing.T) {
	node := lab.Node{ID: 4, Kind: lab.KindPC, Name: "PC4", Config: map[string]json.RawMessage{
		"net":  json.RawMessage(`{"ip":"10.0.0.4","prefixLen":24}`),
		"pack": json.RawMessage(`"must-not-be-created"`),
	}}
	ll := newLoadedLab(&lab.Lab{Version: 1, ID: "pc", Name: "pc", Nodes: []lab.Node{node}}, t.TempDir())
	state := lab.PCState{DHCP: true, SavedCommands: []string{"show ip"}}
	if !mergePCState(ll, 4, state) {
		t.Fatal("merge rejected PC")
	}
	got := ll.findNode(4)
	if got.Name != "PC4" || string(got.Config["net"]) != `{"ip":"10.0.0.4","prefixLen":24}` || string(got.Config["pack"]) != `"must-not-be-created"` {
		t.Fatalf("unrelated fields changed: %#v", got)
	}
	var decoded lab.PCState
	if err := json.Unmarshal(got.Config["pc"], &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.DHCP || len(decoded.SavedCommands) != 1 {
		t.Fatalf("merged state = %#v", decoded)
	}
}

// TestStopRunningPCNodeReturns is the finding-#12 regression: stopping a
// running PC (netprobe) node hung the ENTIRE control plane, 100% reproducibly.
// stopNode's tool branch calls syncPCNode, which read ll.nodes under ll.mu and
// then called ll.findNode — which takes ll.mu itself (pcstate.go). sync.Mutex
// is not reentrant, so the stopping goroutine parked forever holding ll.mu AND
// (via serializedHandler's deferred unlock) s.labMu, wedging every handler.
// Reproduced live on the deployed VM by loading a lab with a PC node, starting
// it, and calling lab.stop; diagnosed from a kill -QUIT goroutine dump.
//
// This test drives the same end-to-end path the live repro did: a node.stop
// dispatched through the real serialized dispatcher against a PC-kind node
// whose runtime has a live (stub) tool endpoint. Pre-fix it deadlocks and the
// watchdog fails it; post-fix the RPC returns, the node is stopped, and both
// locks are free again.
func TestStopRunningPCNodeReturns(t *testing.T) {
	s := newTestServer()
	pcNode := lab.Node{ID: 7, Kind: lab.KindPC, Name: "PC7"}
	ll := newLoadedLab(&lab.Lab{Version: 1, ID: "pc-stop", Name: "pc-stop", Nodes: []lab.Node{pcNode}}, t.TempDir())
	ll.nodes[7] = &nodeRuntime{
		id:      7,
		machine: node.NewMachine(s.nodeStateCallback(7)),
		// A zero-value endpoint stands in for the running netprobe: non-nil is
		// what routes stopNode into the PC branch (and syncPCNode into the
		// state pull); its empty SocketPath makes the pull fail fast and
		// non-fatally, exactly like an already-gone GUI process would.
		tool: &tool.Endpoint{},
	}
	ll.nodes[7].machine.To(node.StateStarting)
	ll.nodes[7].machine.To(node.StateRunning)
	s.mu.Lock()
	s.lab = ll
	s.mu.Unlock()

	done := make(chan *protocol.Response, 1)
	go func() {
		done <- s.Dispatcher().Dispatch(&protocol.Request{
			ID: "stop", Op: "node.stop", Args: json.RawMessage(`{"node":7}`),
		})
	}()
	select {
	case resp := <-done:
		if !resp.OK {
			t.Fatalf("node.stop failed: %+v", resp.Error)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("node.stop never returned: syncPCNode self-deadlocked on ll.mu (finding #12)")
	}

	if st := ll.nodes[7].machine.State(); st != node.StateStopped {
		t.Fatalf("node state after stop = %q, want stopped", st)
	}
	if ll.nodes[7].tool != nil {
		t.Fatal("tool endpoint not cleared by stopNode")
	}
	// Both locks must be free again — the live failure mode was ll.mu and
	// s.labMu stranded forever after the stop wedged.
	free := make(chan struct{})
	go func() {
		s.labMu.Lock()
		ll.mu.Lock()
		ll.mu.Unlock()
		s.labMu.Unlock()
		close(free)
	}()
	select {
	case <-free:
	case <-time.After(5 * time.Second):
		t.Fatal("ll.mu / s.labMu still held after node.stop returned")
	}
}

func TestValidatePCStateRejectsUnknownAndUnsafeContent(t *testing.T) {
	if _, err := validatePCState([]byte(`{"pc":{"dhcp":false,"savedCommands":[],"extra":1},"rev":1}`)); err == nil {
		t.Fatal("unknown state field accepted")
	}
	if _, err := validatePCState([]byte(`{"pc":{"dhcp":false,"savedCommands":["bad\u0001"]},"rev":1}`)); err == nil {
		t.Fatal("non-printable command accepted")
	}
}
