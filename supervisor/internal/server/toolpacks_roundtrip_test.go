package server

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
)

func TestToolListPacksDispatcherRoundTripWire(t *testing.T) {
	s := newTestServer()
	packsDir := t.TempDir()
	toolpacksTestWritePack(t, filepath.Join(packsDir, "stub"), "stub", "Stub Tool")
	s.toolpacksLoad(packsDir)
	if len(s.toolPacks) != 1 || s.toolPacks[0].ID != "stub" {
		t.Fatalf("loaded tool packs = %#v, want stub fixture", s.toolPacks)
	}

	response := dispatch(t, s, "tool.listPacks", protocol.ToolListPacksArgs{})
	wire, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"t","ok":true,"result":{"packs":[{"id":"stub","name":"Stub Tool","icon":"","transport":"unix","groups":[],"modules":[]}]}}`
	if string(wire) != want {
		t.Fatalf("tool.listPacks dispatcher wire = %s, want %s", wire, want)
	}
}
