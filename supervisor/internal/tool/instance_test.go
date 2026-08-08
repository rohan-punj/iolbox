package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestInstanceIDIsDurable(t *testing.T) {
	stateDir := t.TempDir()
	first, err := InstanceID(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := InstanceID(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second || !instValidUUID(first) {
		t.Fatalf("InstanceID values = %q and %q", first, second)
	}
	info, err := os.Stat(filepath.Join(stateDir, instInstanceFile))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("instance-id mode = %o, want 600", info.Mode().Perm())
	}
}

func TestInstanceIDDoesNotRegenerateExistingFile(t *testing.T) {
	stateDir := t.TempDir()
	want := "existing-install-id"
	path := filepath.Join(stateDir, instInstanceFile)
	if err := os.WriteFile(path, []byte(want+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := InstanceID(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("InstanceID = %q, want existing %q", got, want)
	}
}

func TestObjectStateRecordLoadPruneRoundTrip(t *testing.T) {
	stateDir := t.TempDir()
	instanceID, err := InstanceID(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	first := ObjectRecord{NodeID: 7, CgroupPath: "/d/tool-7", Netns: "iolt7", HostVeth: "vtool7", SocketDir: "/run/iolbox/tool/7"}
	second := ObjectRecord{NodeID: 8, CgroupPath: "/d/tool-8", Netns: "iolt8", HostVeth: "vtool8", MgmtVeth: "mtool8", SocketDir: "/run/iolbox/tool/8"}
	if err := RecordObject(stateDir, instanceID, first); err != nil {
		t.Fatal(err)
	}
	if err := RecordObject(stateDir, instanceID, second); err != nil {
		t.Fatal(err)
	}
	state, err := LoadObjectState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	want := ObjectState{InstanceID: instanceID, Objects: map[string]ObjectRecord{"7": first, "8": second}}
	if !reflect.DeepEqual(state, want) {
		t.Fatalf("loaded state = %#v, want %#v", state, want)
	}
	info, err := os.Stat(filepath.Join(stateDir, instObjectFile))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("tool-objects.json mode = %o, want 600", info.Mode().Perm())
	}
	if err := PruneObject(stateDir, instanceID, first.NodeID); err != nil {
		t.Fatal(err)
	}
	state, err = LoadObjectState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Objects) != 1 || !reflect.DeepEqual(state.Objects["8"], second) {
		t.Fatalf("state after pruning node 7 = %#v", state)
	}
	if err := PruneObject(stateDir, instanceID, second.NodeID); err != nil {
		t.Fatal(err)
	}
	state, err = LoadObjectState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if state.InstanceID != instanceID || len(state.Objects) != 0 {
		t.Fatalf("state after pruning all objects = %#v", state)
	}
}

func TestLoadObjectStateMissingIsZeroValue(t *testing.T) {
	state, err := LoadObjectState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state, ObjectState{}) {
		t.Fatalf("missing state = %#v, want zero value", state)
	}
}

func TestForeignObjectStateIsNotAttributedOrPruned(t *testing.T) {
	stateDir := t.TempDir()
	ownID := "own-install"
	foreignID := "other-install"
	foreignRecord := ObjectRecord{NodeID: 12, Netns: "iolt12", HostVeth: "vtool12"}
	data, err := json.Marshal(ObjectState{InstanceID: foreignID, Objects: map[string]ObjectRecord{"12": foreignRecord}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, instObjectFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadObjectState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.InstanceID != foreignID || !reflect.DeepEqual(loaded.Objects["12"], foreignRecord) {
		t.Fatalf("foreign state was not loaded intact: %#v", loaded)
	}
	if err := PruneObject(stateDir, ownID, foreignRecord.NodeID); err != nil {
		t.Fatal(err)
	}
	untouched, err := LoadObjectState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(untouched, loaded) {
		t.Fatalf("foreign state changed during own prune: %#v -> %#v", loaded, untouched)
	}
	if err := RecordObject(stateDir, ownID, ObjectRecord{NodeID: 13}); err == nil || !strings.Contains(err.Error(), "belongs to instance") {
		t.Fatalf("foreign state record error = %v", err)
	}
}

func TestCorruptObjectStateIsReported(t *testing.T) {
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, instObjectFile)
	if err := os.WriteFile(path, []byte(`{"instanceId":"broken"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadObjectState(stateDir); err == nil {
		t.Fatal("truncated object state was accepted")
	}
	if err := RecordObject(stateDir, "current-install", ObjectRecord{NodeID: 1}); err == nil {
		t.Fatal("RecordObject accepted truncated object state")
	}
}
