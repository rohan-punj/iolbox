package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
	"github.com/rohanpunj/iolbox/supervisor/internal/node"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
	"github.com/rohanpunj/iolbox/supervisor/internal/tool"
)

func TestB15StartToolNodeRejectsUnsupportedRuntime(t *testing.T) {
	s := newTestServer()
	doc := &lab.Lab{Version: 1, ID: "tool-unsupported", Name: "tool", Nodes: []lab.Node{{
		ID: 7, Kind: lab.KindTool, Name: "Tool", Config: map[string]json.RawMessage{
			"pack": json.RawMessage(`"stub"`),
		},
	}}}
	ll := newLoadedLab(doc, t.TempDir())
	nr := &nodeRuntime{id: 7, machine: node.NewMachine(nil)}

	_, err := s.startToolNode(ll, &doc.Nodes[0], nr)
	if err == nil {
		t.Fatal("startToolNode unexpectedly accepted unsupported runtime")
	}
	if got := err.(*protocol.Error).Code; got != protocol.CodeUnsupported {
		t.Fatalf("startToolNode error code = %q, want %q", got, protocol.CodeUnsupported)
	}
}

func TestB15StartToolNodeRejectsUnknownPack(t *testing.T) {
	s := newTestServer()
	s.toolCaps = tool.Capabilities{
		NetnsCreate: true, VethCreate: true, VethMoveRename: true,
		CgroupDelegated: true, AmbientCapTransition: true, UnixProxy: true,
	}
	doc := &lab.Lab{Version: 1, ID: "tool-unknown", Name: "tool", Nodes: []lab.Node{{
		ID: 8, Kind: lab.KindTool, Name: "Tool", Config: map[string]json.RawMessage{
			"pack": json.RawMessage(`"missing"`),
		},
	}}}
	ll := newLoadedLab(doc, t.TempDir())
	nr := &nodeRuntime{id: 8, machine: node.NewMachine(nil)}

	_, err := s.startToolNode(ll, &doc.Nodes[0], nr)
	if err == nil {
		t.Fatal("startToolNode unexpectedly accepted unknown pack")
	}
	if got := err.(*protocol.Error).Code; got != protocol.CodeBadRequest {
		t.Fatalf("startToolNode error code = %q, want %q", got, protocol.CodeBadRequest)
	}
}

func TestB15LabLoadChecksInstalledToolPack(t *testing.T) {
	doc := lab.Lab{Version: 1, ID: "tool-load-unknown", Name: "tool", Nodes: []lab.Node{{
		ID: 9, Kind: lab.KindTool, Name: "Tool", Config: map[string]json.RawMessage{
			"pack": json.RawMessage(`"uninstalled"`),
		},
	}}}
	s := newTestServer()
	resp := dispatch(t, s, "lab.load", protocol.LabLoadArgs{Lab: doc})
	if resp.OK || resp.Error.Code != protocol.CodeBadRequest {
		t.Fatalf("unknown pack lab.load = %+v, want bad_request", resp)
	}
	if !strings.Contains(resp.Error.Message, "uninstalled") || !strings.Contains(resp.Error.Message, "node 9") {
		t.Fatalf("unknown pack lab.load message = %q, want node and pack", resp.Error.Message)
	}

	doc.ID = "tool-load-installed"
	s = newTestServer()
	s.toolPacks = []tool.Pack{{ID: "installed"}}
	doc.Nodes[0].Config["pack"] = json.RawMessage(`"installed"`)
	resp = dispatch(t, s, "lab.load", protocol.LabLoadArgs{Lab: doc})
	if !resp.OK {
		t.Fatalf("installed pack lab.load failed: %+v", resp.Error)
	}
}

func TestB15HelloToolFeatureIsCapabilityGated(t *testing.T) {
	s := newTestServer()
	resp := dispatch(t, s, "hello", protocol.HelloArgs{Client: "gui"})
	if !resp.OK {
		t.Fatalf("hello without tool capabilities failed: %+v", resp.Error)
	}
	var result protocol.HelloResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	for _, feature := range result.Features {
		if feature == "tools" {
			t.Fatal("unsupported tool feature advertised")
		}
	}

	s.toolCaps = tool.Capabilities{
		NetnsCreate: true, VethCreate: true, VethMoveRename: true,
		CgroupDelegated: true, AmbientCapTransition: true, UnixProxy: true,
	}
	resp = dispatch(t, s, "hello", protocol.HelloArgs{Client: "gui"})
	if !resp.OK {
		t.Fatalf("hello with tool capabilities failed: %+v", resp.Error)
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	for _, feature := range result.Features {
		if feature == "tools" {
			return
		}
	}
	t.Fatalf("tool feature missing from hello: %v", result.Features)
}

func TestB15StopNodeWithNilToolIsNoOp(t *testing.T) {
	s := newTestServer()
	doc := &lab.Lab{Version: 1, ID: "tool-stop", Name: "tool", Nodes: []lab.Node{{
		ID: 10, Kind: lab.KindTool, Name: "Tool", Config: map[string]json.RawMessage{
			"pack": json.RawMessage(`"stub"`),
		},
	}}}
	ll := newLoadedLab(doc, t.TempDir())
	nr := &nodeRuntime{id: 10, machine: node.NewMachine(nil)}
	ll.nodes[10] = nr

	s.stopNode(ll, 10)
	if nr.tool != nil || nr.machine.State() != node.StateStopped {
		t.Fatalf("stopNode changed nil-tool runtime: tool=%v state=%s", nr.tool, nr.machine.State())
	}
}
