package server

import (
	"encoding/json"
	"testing"

	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
)

func newTestServer() *Server {
	return New(Config{ControlAddr: "127.0.0.1:0", ImageDir: "/opt/iolab/images", RunDir: "/run/iolab", Version: "test"})
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
		"node.start", "node.stop", "node.restart", "node.setImage", "link.add",
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
