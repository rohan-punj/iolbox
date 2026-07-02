package server

import (
	"encoding/json"
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

	// A document carrying a field the lab struct does not model, to prove the
	// store is byte-exact and preserves unknown fields.
	doc := json.RawMessage(`{"version":1,"id":"lab-abc","name":"Store Test","nodes":[],"links":[],"custom":{"note":"keep me"}}`)

	// saveDoc -> {id}
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

	// getDoc round-trips the exact bytes (including the unknown field).
	resp = dispatch(t, s, "lab.getDoc", protocol.LabGetDocArgs{LabID: "lab-abc"})
	if !resp.OK {
		t.Fatalf("getDoc failed: %+v", resp.Error)
	}
	var got protocol.LabGetDocResult
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatal(err)
	}
	if string(got.Lab) != string(doc) {
		t.Fatalf("getDoc = %s, want byte-exact %s", got.Lab, doc)
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
	if len(list.Labs) != 1 || string(list.Labs[0]) != string(doc) {
		t.Fatalf("listDocs = %v, want one byte-exact doc", list.Labs)
	}

	// deleteDoc removes it; a second delete is not an error (idempotent).
	resp = dispatch(t, s, "lab.deleteDoc", protocol.LabGetDocArgs{LabID: "lab-abc"})
	if !resp.OK {
		t.Fatalf("deleteDoc failed: %+v", resp.Error)
	}
	resp = dispatch(t, s, "lab.deleteDoc", protocol.LabGetDocArgs{LabID: "lab-abc"})
	if !resp.OK {
		t.Fatalf("deleteDoc of missing file should succeed, got: %+v", resp.Error)
	}

	// getDoc after delete -> not_found.
	resp = dispatch(t, s, "lab.getDoc", protocol.LabGetDocArgs{LabID: "lab-abc"})
	if resp.OK || resp.Error.Code != protocol.CodeNotFound {
		t.Fatalf("getDoc after delete: want not_found, got %+v", resp)
	}

	// listDocs is empty again.
	resp = dispatch(t, s, "lab.listDocs", nil)
	json.Unmarshal(resp.Result, &list)
	if len(list.Labs) != 0 {
		t.Fatalf("listDocs after delete = %v, want empty", list.Labs)
	}
}

func TestLabStoreSaveOverwrites(t *testing.T) {
	s := newStoreServer(t)
	v1 := json.RawMessage(`{"id":"dup","name":"v1"}`)
	v2 := json.RawMessage(`{"id":"dup","name":"v2"}`)
	dispatch(t, s, "lab.saveDoc", protocol.LabSaveDocArgs{Lab: v1})
	dispatch(t, s, "lab.saveDoc", protocol.LabSaveDocArgs{Lab: v2})

	resp := dispatch(t, s, "lab.getDoc", protocol.LabGetDocArgs{LabID: "dup"})
	var got protocol.LabGetDocResult
	json.Unmarshal(resp.Result, &got)
	if string(got.Lab) != string(v2) {
		t.Fatalf("overwrite: got %s, want %s", got.Lab, v2)
	}
}

func TestLabStoreRejectsBadID(t *testing.T) {
	s := newStoreServer(t)
	// Traversal / illegal filename tokens are rejected on save and get.
	bad := json.RawMessage(`{"id":"../evil","name":"x"}`)
	resp := dispatch(t, s, "lab.saveDoc", protocol.LabSaveDocArgs{Lab: bad})
	if resp.OK || resp.Error.Code != protocol.CodeSchemaInvalid {
		t.Fatalf("saveDoc with bad id: want schema_invalid, got %+v", resp)
	}

	resp = dispatch(t, s, "lab.getDoc", protocol.LabGetDocArgs{LabID: "a/b"})
	if resp.OK || resp.Error.Code != protocol.CodeSchemaInvalid {
		t.Fatalf("getDoc with bad id: want schema_invalid, got %+v", resp)
	}

	// A document with no id is rejected.
	resp = dispatch(t, s, "lab.saveDoc", protocol.LabSaveDocArgs{Lab: json.RawMessage(`{"name":"noid"}`)})
	if resp.OK || resp.Error.Code != protocol.CodeSchemaInvalid {
		t.Fatalf("saveDoc with no id: want schema_invalid, got %+v", resp)
	}
}
