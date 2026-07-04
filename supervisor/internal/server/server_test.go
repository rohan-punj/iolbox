package server

import (
	"encoding/json"
	"testing"

	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
)

func newTestServer() *Server {
	return New(Config{ControlAddr: "127.0.0.1:0", ImageDir: "/opt/iolbox/images", RunDir: "/run/iolbox", Version: "test"})
}

func dispatch(t *testing.T, s *Server, op string, args any) *protocol.Response {
	t.Helper()
	var raw json.RawMessage
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		raw = b
	}
	return s.Dispatcher().Dispatch(&protocol.Request{ID: "t", Op: op, Args: raw})
}

func TestHelloVerb(t *testing.T) {
	s := newTestServer()
	resp := dispatch(t, s, "hello", protocol.HelloArgs{Client: "gui"})
	if !resp.OK {
		t.Fatalf("hello failed: %+v", resp.Error)
	}
	var r protocol.HelloResult
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		t.Fatal(err)
	}
	if r.Supervisor != "test" || r.Arch == "" {
		t.Fatalf("hello result: %+v", r)
	}
	// Egress is always resolved to a concrete value ("slirp" or "routed"); the
	// GUI reads it to badge the NAT node. The test box is not behind slirp.
	if r.Egress != "slirp" && r.Egress != "routed" {
		t.Fatalf("hello egress = %q, want slirp or routed", r.Egress)
	}
}

func TestLabLoadValidation(t *testing.T) {
	s := newTestServer()
	// Invalid: version 2.
	bad := json.RawMessage(`{"lab":{"version":2,"id":"x","name":"n","nodes":[],"links":[]}}`)
	resp := s.Dispatcher().Dispatch(&protocol.Request{ID: "1", Op: "lab.load", Args: bad})
	if resp.OK || resp.Error.Code != protocol.CodeSchemaInvalid {
		t.Fatalf("expected schema_invalid, got %+v", resp)
	}

	// Valid lab with one vpcs node -> allocates a console port.
	good := json.RawMessage(`{"lab":{"version":1,"id":"lab-1","name":"n","nodes":[{"id":0,"kind":"vpcs","name":"PC","x":0,"y":0}],"links":[]}}`)
	resp = s.Dispatcher().Dispatch(&protocol.Request{ID: "2", Op: "lab.load", Args: good})
	if !resp.OK {
		t.Fatalf("good lab.load failed: %+v", resp.Error)
	}
	var r protocol.LabLoadResult
	json.Unmarshal(resp.Result, &r)
	if r.LabID != "lab-1" || len(r.Nodes) != 1 || r.Nodes[0].ConsolePort < 9000 {
		t.Fatalf("lab.load result: %+v", r)
	}
}

func TestStatusNotLoaded(t *testing.T) {
	s := newTestServer()
	resp := dispatch(t, s, "status", protocol.LabSelectArgs{})
	if !resp.OK {
		t.Fatalf("status should succeed when empty: %+v", resp.Error)
	}
	var r protocol.StatusResult
	json.Unmarshal(resp.Result, &r)
	if len(r.Nodes) != 0 {
		t.Fatalf("expected empty status, got %+v", r)
	}
}

func TestOperationsRequireLoadedLab(t *testing.T) {
	s := newTestServer()
	resp := dispatch(t, s, "lab.start", protocol.LabSelectArgs{LabID: "nope"})
	if resp.OK || resp.Error.Code != protocol.CodeNotLoaded {
		t.Fatalf("expected not_loaded, got %+v", resp)
	}
}

func TestUnknownVerb(t *testing.T) {
	s := newTestServer()
	resp := dispatch(t, s, "does.not.exist", nil)
	if resp.OK || resp.Error.Code != protocol.CodeUnsupported {
		t.Fatalf("expected unsupported, got %+v", resp)
	}
}

func TestAllVerbsRegistered(t *testing.T) {
	s := newTestServer()
	want := []string{
		"hello", "image.list", "image.register", "lab.load", "lab.start", "lab.stop",
		"lab.wipe", "node.start", "node.stop", "node.restart", "node.setImage", "link.add",
		"link.remove", "capture.start", "capture.stop", "config.save", "config.extract", "status",
	}
	have := map[string]bool{}
	for _, v := range s.Dispatcher().Verbs() {
		have[v] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Fatalf("verb %q not registered", w)
		}
	}
}

// TestNodeAddRemove: incremental topology sync — a node dropped onto a loaded
// lab registers with the supervisor (console port allocated, doc updated) so
// it can start without a lab.load; duplicates are rejected; remove drops the
// node AND its links from the loaded doc.
func TestNodeAddRemove(t *testing.T) {
	s := newTestServer()
	load := json.RawMessage(`{"lab":{"version":1,"id":"l1","name":"n","nodes":[{"id":0,"kind":"vpcs","name":"PC","x":0,"y":0}],"links":[]}}`)
	if resp := s.Dispatcher().Dispatch(&protocol.Request{ID: "1", Op: "lab.load", Args: load}); !resp.OK {
		t.Fatalf("lab.load: %+v", resp.Error)
	}

	// Add a NAT node mid-session (the exact user flow that used to fail).
	add := json.RawMessage(`{"labId":"l1","node":{"id":1,"kind":"nat","name":"NAT1","x":10,"y":10}}`)
	resp := s.Dispatcher().Dispatch(&protocol.Request{ID: "2", Op: "node.add", Args: add})
	if !resp.OK {
		t.Fatalf("node.add: %+v", resp.Error)
	}
	var r protocol.NodeAddResult
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		t.Fatal(err)
	}
	if r.Node != 1 || r.ConsolePort < 9000 {
		t.Fatalf("node.add result: %+v", r)
	}
	if s.lab.get(1) == nil || len(s.lab.doc.Nodes) != 2 {
		t.Fatalf("node 1 not registered with the loaded lab")
	}

	// Duplicate id is rejected without side effects.
	if resp := s.Dispatcher().Dispatch(&protocol.Request{ID: "3", Op: "node.add", Args: add}); resp.OK {
		t.Fatal("duplicate node.add must fail")
	}

	// A link to the new node lands in the LOADED doc too (link.add upsert).
	// Off-linux the relay start itself fails (no UDP data plane in the stub)
	// AFTER the upsert -- the doc sync is what this test pins, so the response
	// status is deliberately not asserted here.
	link := json.RawMessage(`{"labId":"l1","link":{"id":0,"type":"p2p","endpoints":[{"node":0,"interface":"eth0"},{"node":1,"interface":"eth0"}]}}`)
	_ = s.Dispatcher().Dispatch(&protocol.Request{ID: "4", Op: "link.add", Args: link})
	if len(s.lab.doc.Links) != 1 {
		t.Fatalf("link.add must upsert into the loaded doc, links=%d", len(s.lab.doc.Links))
	}

	// node.remove drops the node AND its links from the doc.
	rm := json.RawMessage(`{"labId":"l1","node":1}`)
	if resp := s.Dispatcher().Dispatch(&protocol.Request{ID: "5", Op: "node.remove", Args: rm}); !resp.OK {
		t.Fatalf("node.remove: %+v", resp.Error)
	}
	if s.lab.get(1) != nil || len(s.lab.doc.Nodes) != 1 || len(s.lab.doc.Links) != 0 {
		t.Fatalf("node.remove must drop node + its links: nodes=%d links=%d", len(s.lab.doc.Nodes), len(s.lab.doc.Links))
	}
}
