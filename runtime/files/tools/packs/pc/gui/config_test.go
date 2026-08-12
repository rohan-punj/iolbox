package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestStoreAtomicSaveRoundTrip(t *testing.T) {
	path := t.TempDir() + "/nested/options.json"
	store := NewStore(path)
	want := Config{PC: PCState{DHCP: true, SavedCommands: []string{"ip dhcp", "ping 192.0.2.1"}}, Rev: 7}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary state file remains: %v", err)
	}
	loaded := NewStore(path)
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(loaded.Snapshot())
	expected, _ := json.Marshal(want)
	if string(got) != string(expected) {
		t.Fatalf("round trip = %s, want %s", got, expected)
	}
}

func TestCLISaveWritesSharedStateDocument(t *testing.T) {
	app := testApp(t)
	if got := dispatchLine(app, "save"); got != "Saved to this node. Use Lab > Save to store it in the lab file." {
		t.Fatalf("save output = %q", got)
	}
	state := NewStore(app.store.path)
	if err := state.Load(); err != nil {
		t.Fatal(err)
	}
	if state.Snapshot().Rev == 0 {
		t.Fatal("save did not advance revision")
	}
	if state.Snapshot().PC.SavedCommands == nil {
		t.Fatal("savedCommands is not an array")
	}
}
