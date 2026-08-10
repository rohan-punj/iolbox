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

func TestLoadPackStubFixture(t *testing.T) {
	root := manifestTestFixtureRoot()
	pack, err := LoadPack(root)
	if err != nil {
		t.Fatalf("LoadPack() error = %v", err)
	}
	wantManifest := Manifest{
		ManifestVersion: 1,
		ID:              "stub",
		Name:            "Stub Tool",
		Interpreter:     "none",
		GUI: GUI{
			Bin:       "tool-stubgui",
			Transport: "unix",
			Console:   "http",
			Health:    "/healthz",
		},
		Caps:    []string{"NET_RAW"},
		Options: []Option{},
		Groups:  []string{},
		Modules: []Module{},
	}
	wantLimits := DefaultLimits()
	wantManifest.Limits = &wantLimits
	if !reflect.DeepEqual(pack.Manifest, wantManifest) {
		t.Fatalf("manifest = %#v, want %#v", pack.Manifest, wantManifest)
	}
	if pack.ID != "stub" || pack.Root != root {
		t.Fatalf("pack identity = (%q, %q), want (stub, %q)", pack.ID, pack.Root, root)
	}
	if pack.GUIBin != filepath.Join(root, "tool-stubgui") {
		t.Fatalf("GUIBin = %q, want %q", pack.GUIBin, filepath.Join(root, "tool-stubgui"))
	}
	if len(pack.Scripts) != 0 {
		t.Fatalf("Scripts = %#v, want empty map", pack.Scripts)
	}
	if pack.Manifest.Limits == nil || *pack.Manifest.Limits != wantLimits {
		t.Fatalf("limits = %#v, want %#v", pack.Manifest.Limits, wantLimits)
	}
}

func TestManifestValidateAcceptsDefaultTransport(t *testing.T) {
	manifest := Manifest{
		ManifestVersion: ManifestVersion,
		ID:              "pack",
		Name:            "Pack",
		GUI:             GUI{Bin: "gui", Health: "/healthz"},
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestLoadPackRejectsManifestRules(t *testing.T) {
	tests := []struct {
		name string
		want string
		edit func(*Manifest)
	}{
		{name: "missing health", want: "gui.health is required", edit: func(m *Manifest) { m.GUI.Health = "" }},
		{name: "health must be path", want: "gui.health", edit: func(m *Manifest) { m.GUI.Health = "healthz" }},
		{name: "NET_ADMIN is rejected", want: "NET_ADMIN", edit: func(m *Manifest) { m.Caps = []string{"NET_ADMIN"} }},
		{name: "unknown manifest version", want: "manifestVersion", edit: func(m *Manifest) { m.ManifestVersion = 2 }},
		{
			name: "traversal script",
			want: "..",
			edit: func(m *Manifest) {
				m.Groups = []string{"test"}
				m.Modules = []Module{{Key: "bad", Group: "test", Script: "../../etc/passwd"}}
			},
		},
		{
			name: "duplicate module key",
			want: "duplicated",
			edit: func(m *Manifest) {
				m.Groups = []string{"test"}
				m.Modules = []Module{{Key: "same", Group: "test", Script: "tool-stubgui"}, {Key: "same", Group: "test", Script: "tool-stubgui"}}
			},
		},
		{
			name: "unknown group",
			want: "unknown group",
			edit: func(m *Manifest) {
				m.Modules = []Module{{Key: "bad", Group: "missing", Script: "tool-stubgui"}}
			},
		},
		{name: "invalid transport", want: "gui.transport", edit: func(m *Manifest) { m.GUI.Transport = "http" }},
		{name: "missing id", want: "manifest id", edit: func(m *Manifest) { m.ID = "" }},
		{name: "missing name", want: "manifest name", edit: func(m *Manifest) { m.Name = "" }},
		{name: "missing gui bin", want: "gui.bin", edit: func(m *Manifest) { m.GUI.Bin = "" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := manifestTestWritePack(t, test.edit)
			if _, err := LoadPack(root); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadPack() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestLoadPackRejectsSymlinkEscape(t *testing.T) {
	root := manifestTestWritePack(t, func(m *Manifest) {
		m.Groups = []string{"test"}
		m.Modules = []Module{{Key: "escape", Group: "test", Script: "outside"}}
	})
	outside := filepath.Join(filepath.Dir(root), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := LoadPack(root); err == nil || !strings.Contains(err.Error(), "escapes pack root") {
		t.Fatalf("LoadPack() error = %v, want symlink escape rejection", err)
	}
}

func TestLoadPacksPartialSuccessAndOrdering(t *testing.T) {
	root := t.TempDir()
	manifestTestWritePackAt(t, filepath.Join(root, "zeta"), func(m *Manifest) { m.ID = "zeta" })
	manifestTestWritePackAt(t, filepath.Join(root, "alpha"), func(m *Manifest) { m.ID = "alpha" })
	broken := filepath.Join(root, "malformed")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "pack.json"), []byte(`{"manifestVersion":2}`), 0o600); err != nil {
		t.Fatal(err)
	}

	packs, err := LoadPacks(root)
	if err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("LoadPacks() error = %v, want malformed-pack warning", err)
	}
	if len(packs) != 2 || packs[0].ID != "alpha" || packs[1].ID != "zeta" {
		t.Fatalf("LoadPacks() = %#v, want alpha then zeta", packs)
	}
}

func TestLoadPacksMissingDirectory(t *testing.T) {
	packs, err := LoadPacks(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("LoadPacks() error = %v, want nil", err)
	}
	if packs == nil || len(packs) != 0 {
		t.Fatalf("LoadPacks() = %#v, want non-nil empty slice", packs)
	}
}

func manifestTestFixtureRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	root, err := filepath.Abs(filepath.Join(filepath.Dir(filename), "testdata", "packs", "stub"))
	if err != nil {
		panic(err)
	}
	return filepath.Clean(root)
}

func manifestTestWritePack(t *testing.T, edit func(*Manifest)) string {
	root := filepath.Join(t.TempDir(), "pack")
	manifestTestWritePackAt(t, root, edit)
	return root
}

func manifestTestWritePackAt(t *testing.T, root string, edit func(*Manifest)) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(manifestTestFixtureRoot(), "pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	edit(&manifest)
	data, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pack.json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	gui, err := os.ReadFile(filepath.Join(manifestTestFixtureRoot(), "tool-stubgui"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tool-stubgui"), gui, 0o600); err != nil {
		t.Fatal(err)
	}
}
