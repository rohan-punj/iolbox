package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rohanpunj/iolab/supervisor/internal/lab"
	"github.com/rohanpunj/iolab/supervisor/internal/node"
	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
)

// loadLab loads doc into s and returns the loaded lab so tests can inspect
// runtime state. It fails the test on a non-ok lab.load.
func loadLab(t *testing.T, s *Server, doc *lab.Lab) *loadedLab {
	t.Helper()
	resp := dispatch(t, s, "lab.load", protocol.LabLoadArgs{Lab: *doc})
	if !resp.OK {
		t.Fatalf("lab.load failed: %+v", resp.Error)
	}
	return s.lab
}

// TestLabWipeDeletesNvramAndStopsNode is the headline case: a wipe stops a
// running node (transitioning it to stopped) and deletes its nvram_<id> file
// from the shared lab dir, returning the wiped id.
func TestLabWipeDeletesNvramAndStopsNode(t *testing.T) {
	runDir := t.TempDir()
	s := New(Config{ControlAddr: "127.0.0.1:0", ImageDir: "/opt/iolab/images", RunDir: runDir, Version: "test"})
	doc := &lab.Lab{Version: 1, ID: "lab-w", Name: "n", Nodes: []lab.Node{iolNode(0)}}
	ll := loadLab(t, s, doc)

	// Drive the node's state machine to running (off Linux there is no real
	// process, so stopNode falls back to machine.To(stopped)); the wipe must
	// stop it via the shared stop path.
	nr := ll.get(0)
	nr.machine.To(node.StateStarting)
	nr.machine.To(node.StateRunning)

	// Seed the persisted nvram file the wipe must delete.
	if err := os.MkdirAll(ll.labDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	nvramPath := filepath.Join(ll.labDir(), nvramFilename(0))
	if err := os.WriteFile(nvramPath, []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}

	resp := dispatch(t, s, "lab.wipe", protocol.LabWipeArgs{LabID: "lab-w"})
	if !resp.OK {
		t.Fatalf("lab.wipe failed: %+v", resp.Error)
	}
	var r protocol.LabWipeResult
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Wiped) != 1 || r.Wiped[0] != 0 {
		t.Fatalf("wiped set wrong: %+v", r)
	}
	if _, err := os.Stat(nvramPath); !os.IsNotExist(err) {
		t.Fatalf("nvram file must be gone, stat err = %v", err)
	}
	if st := nr.machine.State(); st != node.StateStopped {
		t.Fatalf("node must be stopped after wipe, got %s", st)
	}
}

// TestLabWipeMissingNvramNotError confirms wiping a node with no persisted nvram
// succeeds (a fresh node that never booted has no file yet).
func TestLabWipeMissingNvramNotError(t *testing.T) {
	runDir := t.TempDir()
	s := New(Config{ControlAddr: "127.0.0.1:0", ImageDir: "/opt/iolab/images", RunDir: runDir, Version: "test"})
	doc := &lab.Lab{Version: 1, ID: "lab-w2", Name: "n", Nodes: []lab.Node{iolNode(0), iolNode(1)}}
	loadLab(t, s, doc)

	resp := dispatch(t, s, "lab.wipe", protocol.LabWipeArgs{LabID: "lab-w2"})
	if !resp.OK {
		t.Fatalf("lab.wipe over missing nvram must succeed: %+v", resp.Error)
	}
	var r protocol.LabWipeResult
	json.Unmarshal(resp.Result, &r)
	if len(r.Wiped) != 2 {
		t.Fatalf("expected both nodes wiped, got %+v", r)
	}
}

// TestLabWipeUnknownNode rejects a wipe targeting a node id not in the lab.
func TestLabWipeUnknownNode(t *testing.T) {
	runDir := t.TempDir()
	s := New(Config{ControlAddr: "127.0.0.1:0", ImageDir: "/opt/iolab/images", RunDir: runDir, Version: "test"})
	doc := &lab.Lab{Version: 1, ID: "lab-w3", Name: "n", Nodes: []lab.Node{iolNode(0)}}
	loadLab(t, s, doc)

	resp := dispatch(t, s, "lab.wipe", protocol.LabWipeArgs{LabID: "lab-w3", Nodes: []int{9}})
	if resp.OK || resp.Error.Code != protocol.CodeBadRequest {
		t.Fatalf("expected bad_request for unknown node, got %+v", resp)
	}
}

// TestLabWipeRequiresLoadedLab rejects a wipe when no lab is loaded.
func TestLabWipeRequiresLoadedLab(t *testing.T) {
	s := newTestServer()
	resp := dispatch(t, s, "lab.wipe", protocol.LabWipeArgs{LabID: "nope"})
	if resp.OK || resp.Error.Code != protocol.CodeNotLoaded {
		t.Fatalf("expected not_loaded, got %+v", resp)
	}
}
