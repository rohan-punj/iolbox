package server

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rohanpunj/iolbox/supervisor/internal/image"
	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
	"github.com/rohanpunj/iolbox/supervisor/internal/node"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
)

func helloForTest(t *testing.T, s *Server) (protocol.HelloResult, []byte) {
	t.Helper()
	resp := s.Dispatcher().Dispatch(&protocol.Request{ID: "hello-m5", Op: "hello", Args: json.RawMessage(`{}`)})
	if !resp.OK {
		t.Fatalf("hello failed: %+v", resp.Error)
	}
	var hello protocol.HelloResult
	if err := json.Unmarshal(resp.Result, &hello); err != nil {
		t.Fatal(err)
	}
	return hello, resp.Result
}

func TestHelloCapabilitySignal(t *testing.T) {
	legacy, legacyRaw := helloForTest(t, newTestServer())
	if !contains(legacy.Features, "i386") {
		t.Fatalf("default hello omitted i386: %+v", legacy.Features)
	}
	if !bytes.Contains(legacyRaw, []byte(`"arch":"x86_64"`)) {
		t.Fatalf("default hello did not preserve runtime arch: %s", legacyRaw)
	}
	if bytes.Contains(legacyRaw, []byte("iolArchitectures")) {
		t.Fatalf("hello grew the cut iolArchitectures field: %s", legacyRaw)
	}

	honest, honestRaw := helloForTest(t, New(Config{
		ControlAddr: "127.0.0.1:0",
		ImageDir:    "/opt/iolbox/images",
		RunDir:      "/run/iolbox",
		Version:     "test",
		DisableI386: true,
	}))
	if contains(honest.Features, "i386") {
		t.Fatalf("disabled hello advertised i386: %+v", honest.Features)
	}
	if bytes.Contains(honestRaw, []byte("iolArchitectures")) {
		t.Fatalf("disabled hello grew the cut iolArchitectures field: %s", honestRaw)
	}
}

func TestDisabledI386PreflightRejectsBeforeSideEffects(t *testing.T) {
	s := New(Config{ControlAddr: "127.0.0.1:0", ImageDir: t.TempDir(), RunDir: t.TempDir(), DisableI386: true})
	s.images["old-iol"] = image.Info{ID: "old-iol", Filename: "i86bi_m5_unsupported.bin", Arch: image.ArchI386, Class: image.ClassL3}
	doc := &lab.Lab{Version: 1, ID: "m5", Name: "m5", Nodes: []lab.Node{{ID: 7, Kind: lab.KindIOL, Name: "old", Image: &lab.ImageRef{ID: "old-iol"}}}}
	ll := newLoadedLab(doc, t.TempDir())
	ll.nodes[7] = &nodeRuntime{id: 7, imageID: "old-iol", machine: node.NewMachine(nil)}

	out, err := s.startNodes(ll, []int{7})
	if err != nil {
		t.Fatalf("rejected start returned top-level error: %v", err)
	}
	result, ok := out.(protocol.StartResult)
	if !ok || len(result.Failed) != 1 || result.Failed[0].Node != 7 {
		t.Fatalf("unexpected partial failure result: %#v", out)
	}
	if !strings.Contains(result.Failed[0].Error, protocol.CodeImageArchMismatch) {
		t.Fatalf("wrong rejection: %#v", result.Failed[0])
	}
	if len(ll.captures) != 0 || len(ll.staticTaps) != 0 {
		t.Fatalf("rejected start performed side effects: captures=%v taps=%v", ll.captures, ll.staticTaps)
	}
}

func TestDisabledI386PreflightPreservesRunningAndUnknown(t *testing.T) {
	s := New(Config{ControlAddr: "127.0.0.1:0", DisableI386: true})
	s.images["old-iol"] = image.Info{ID: "old-iol", Filename: "i86bi_m5_unsupported.bin", Arch: image.ArchI386}
	doc := &lab.Lab{Version: 1, ID: "m5", Name: "m5", Nodes: []lab.Node{
		{ID: 1, Kind: lab.KindIOL, Name: "running", Image: &lab.ImageRef{ID: "old-iol"}},
		{ID: 2, Kind: lab.KindIOL, Name: "unknown", Image: &lab.ImageRef{ID: "missing"}},
	}}
	ll := newLoadedLab(doc, t.TempDir())
	machine := node.NewMachine(nil)
	machine.To(node.StateStarting)
	machine.To(node.StateRunning)
	ll.nodes[1] = &nodeRuntime{id: 1, imageID: "old-iol", machine: machine, proc: &node.Process{Spec: node.Spec{NodeID: 1}, Machine: machine}}
	ll.nodes[2] = &nodeRuntime{id: 2, imageID: "missing", machine: node.NewMachine(nil)}
	ids, result := s.preflightIOLImages(ll, []int{1, 2})
	if len(result.Failed) != 0 || len(ids) != 2 {
		t.Fatalf("running/unknown preflight changed behavior: ids=%v result=%+v", ids, result)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
