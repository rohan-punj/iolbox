package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
	"github.com/rohanpunj/iolbox/supervisor/internal/tool"
)

func TestToolListPacksEmptyIsArray(t *testing.T) {
	s := newTestServer()
	result, err := s.handleToolListPacks(nil)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if string(wire) != `{"packs":[]}` {
		t.Fatalf("empty tool.listPacks = %s, want packs array", wire)
	}
}

func TestToolListPacksMapsManifestMetadata(t *testing.T) {
	s := newTestServer()
	s.toolPacks = []tool.Pack{{
		ID: "secbench",
		Manifest: tool.Manifest{
			ID:     "secbench",
			Name:   "Security Bench",
			Icon:   "shield",
			Groups: []string{"recon", "spoof"},
			GUI:    tool.GUI{Transport: "unix"},
			Modules: []tool.Module{
				{Key: "arp", Label: "ARP Spoof", Group: "spoof"},
				{Key: "scan", Label: "Scan", Group: "recon"},
			},
		},
	}}
	result, err := s.handleToolListPacks(nil)
	if err != nil {
		t.Fatal(err)
	}
	got := result.(protocol.ToolListPacksResult)
	want := protocol.ToolListPacksResult{Packs: []protocol.ToolPackInfo{{
		ID:        "secbench",
		Name:      "Security Bench",
		Icon:      "shield",
		Transport: "unix",
		Groups:    []string{"recon", "spoof"},
		Modules: []protocol.ToolModuleInfo{
			{Key: "arp", Label: "ARP Spoof", Group: "spoof"},
			{Key: "scan", Label: "Scan", Group: "recon"},
		},
	}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tool.listPacks = %#v, want %#v", got, want)
	}
}

func TestToolPackLookup(t *testing.T) {
	s := newTestServer()
	want := tool.Pack{ID: "stub"}
	s.toolPacks = []tool.Pack{want}
	if got, ok := s.toolPack("stub"); !ok || got.ID != want.ID {
		t.Fatalf("toolPack hit = %#v, %t; want %q, true", got, ok, want.ID)
	}
	if got, ok := s.toolPack("missing"); ok || got.ID != "" {
		t.Fatalf("toolPack miss = %#v, %t; want zero, false", got, ok)
	}
}

func TestToolpacksLoadKeepsValidPackWithMalformedPack(t *testing.T) {
	dir := t.TempDir()
	toolpacksTestWritePack(t, filepath.Join(dir, "valid"), "valid", "Valid")
	invalid := filepath.Join(dir, "invalid")
	if err := os.MkdirAll(invalid, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invalid, "pack.json"), []byte("{not-json}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := newTestServer()
	s.toolpacksLoad(dir)
	if len(s.toolPacks) != 1 || s.toolPacks[0].ID != "valid" {
		t.Fatalf("cached packs = %#v, want valid pack despite warning", s.toolPacks)
	}
	if _, ok := s.toolPack("valid"); !ok {
		t.Fatal("valid pack was not available after partial load")
	}
}

func TestStopRuntimeIsNilSafeAndIdempotent(t *testing.T) {
	s := newTestServer()
	s.StopRuntime()
	calls := 0
	s.toolStop = func() { calls++ }
	s.StopRuntime()
	s.StopRuntime()
	if calls != 1 {
		t.Fatalf("tool stop calls = %d, want 1", calls)
	}
}

func toolpacksTestWritePack(t *testing.T, root, id, name string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"manifestVersion":1,"id":"` + id + `","name":"` + name + `","gui":{"bin":"gui","health":"/healthz","transport":"unix"}}` + "\n"
	if err := os.WriteFile(filepath.Join(root, "pack.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gui"), []byte("stub"), 0o700); err != nil {
		t.Fatal(err)
	}
}
