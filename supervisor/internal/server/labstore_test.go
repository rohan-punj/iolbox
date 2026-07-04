package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
)

// newStoreServer builds a server whose lab-document store points at a fresh
// temp dir, so save/list/get/delete tests never touch the real /opt path.
func newStoreServer(t *testing.T) *Server {
	t.Helper()
	return New(Config{
		ControlAddr: "127.0.0.1:0",
		ImageDir:    "/opt/iolab/images",
		RunDir:      "/run/iolab",
		LabsDir:     t.TempDir(),
		Version:     "test",
	})
}

func TestLabStoreRoundTrip(t *testing.T) {
	s := newStoreServer(t)

	// A YAML document (the native format) carrying an unknown field, to prove the
	// store is byte-exact and format-agnostic.
	doc := "version: 1\nid: lab-abc\nname: Store Test\nnodes: []\nlinks: []\ncustom:\n  note: keep me\n"

	resp := dispatch(t, s, "lab.saveDoc", protocol.LabSaveDocArgs{Lab: doc})
	if !resp.OK {
		t.Fatalf("saveDoc failed: %+v", resp.Error)
	}
	var saved protocol.LabSaveDocResult
	if err := json.Unmarshal(resp.Result, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.ID != "lab-abc" {
		t.Fatalf("saveDoc id = %q, want lab-abc", saved.ID)
	}
	// Stored as <id>.yml.
	if _, err := os.Stat(filepath.Join(s.cfg.LabsDir, "lab-abc.yml")); err != nil {
		t.Fatalf("expected lab-abc.yml on disk: %v", err)
	}

	// getDoc round-trips the exact text (including the unknown field).
	resp = dispatch(t, s, "lab.getDoc", protocol.LabGetDocArgs{LabID: "lab-abc"})
	if !resp.OK {
		t.Fatalf("getDoc failed: %+v", resp.Error)
	}
	var got protocol.LabGetDocResult
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatal(err)
	}
	if got.Lab != doc {
		t.Fatalf("getDoc = %q, want byte-exact %q", got.Lab, doc)
	}

	// listDocs returns exactly the one stored doc.
	resp = dispatch(t, s, "lab.listDocs", nil)
	if !resp.OK {
		t.Fatalf("listDocs failed: %+v", resp.Error)
	}
	var list protocol.LabListDocsResult
	if err := json.Unmarshal(resp.Result, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Labs) != 1 || list.Labs[0] != doc {
		t.Fatalf("listDocs = %v, want one byte-exact doc", list.Labs)
	}

	// deleteDoc removes it; a second delete is idempotent.
	resp = dispatch(t, s, "lab.deleteDoc", protocol.LabGetDocArgs{LabID: "lab-abc"})
	if !resp.OK {
		t.Fatalf("deleteDoc failed: %+v", resp.Error)
	}
	resp = dispatch(t, s, "lab.deleteDoc", protocol.LabGetDocArgs{LabID: "lab-abc"})
	if !resp.OK {
		t.Fatalf("deleteDoc of missing file should succeed, got: %+v", resp.Error)
	}
	resp = dispatch(t, s, "lab.getDoc", protocol.LabGetDocArgs{LabID: "lab-abc"})
	if resp.OK || resp.Error.Code != protocol.CodeNotFound {
		t.Fatalf("getDoc after delete: want not_found, got %+v", resp)
	}
}

// TestLabStoreReadsLegacyJSON confirms a lab saved before the YAML switch (a
// bare <id>.json on disk) is still listed and fetched.
func TestLabStoreReadsLegacyJSON(t *testing.T) {
	s := newStoreServer(t)
	legacy := `{"version":1,"id":"old-lab","name":"Legacy","nodes":[],"links":[]}`
	if err := os.MkdirAll(s.cfg.LabsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.cfg.LabsDir, "old-lab.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := dispatch(t, s, "lab.getDoc", protocol.LabGetDocArgs{LabID: "old-lab"})
	if !resp.OK {
		t.Fatalf("getDoc legacy json failed: %+v", resp.Error)
	}
	var got protocol.LabGetDocResult
	json.Unmarshal(resp.Result, &got)
	if got.Lab != legacy {
		t.Fatalf("legacy getDoc = %q, want %q", got.Lab, legacy)
	}
	resp = dispatch(t, s, "lab.listDocs", nil)
	var list protocol.LabListDocsResult
	json.Unmarshal(resp.Result, &list)
	if len(list.Labs) != 1 {
		t.Fatalf("listDocs should include the legacy json, got %d", len(list.Labs))
	}
}

func TestLabStoreSaveOverwrites(t *testing.T) {
	s := newStoreServer(t)
	dispatch(t, s, "lab.saveDoc", protocol.LabSaveDocArgs{Lab: "id: dup\nname: v1\n"})
	dispatch(t, s, "lab.saveDoc", protocol.LabSaveDocArgs{Lab: "id: dup\nname: v2\n"})

	resp := dispatch(t, s, "lab.getDoc", protocol.LabGetDocArgs{LabID: "dup"})
	var got protocol.LabGetDocResult
	json.Unmarshal(resp.Result, &got)
	if got.Lab != "id: dup\nname: v2\n" {
		t.Fatalf("overwrite: got %q, want v2", got.Lab)
	}
}

func TestLabStoreRejectsBadID(t *testing.T) {
	s := newStoreServer(t)
	// Traversal / illegal filename tokens are rejected on save and get.
	resp := dispatch(t, s, "lab.saveDoc", protocol.LabSaveDocArgs{Lab: "id: ../evil\nname: x\n"})
	if resp.OK {
		t.Fatalf("saveDoc with bad id must fail, got %+v", resp)
	}

	resp = dispatch(t, s, "lab.getDoc", protocol.LabGetDocArgs{LabID: "a/b"})
	if resp.OK || resp.Error.Code != protocol.CodeSchemaInvalid {
		t.Fatalf("getDoc with bad id: want schema_invalid, got %+v", resp)
	}

	// A document with no id is rejected.
	resp = dispatch(t, s, "lab.saveDoc", protocol.LabSaveDocArgs{Lab: "name: noid\n"})
	if resp.OK || resp.Error.Code != protocol.CodeSchemaInvalid {
		t.Fatalf("saveDoc with no id: want schema_invalid, got %+v", resp)
	}
}
