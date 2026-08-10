package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestManifestModuleKeysMatchModuleDefs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "pack.json"))
	if err != nil {
		t.Fatalf("read sibling pack.json: %v", err)
	}
	var manifest struct {
		Modules []struct {
			Key string `json:"key"`
		} `json:"modules"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse sibling pack.json: %v", err)
	}
	manifestKeys := make(map[string]struct{}, len(manifest.Modules))
	for _, module := range manifest.Modules {
		if module.Key == "" {
			t.Fatal("pack.json contains a module with an empty key")
		}
		if _, duplicate := manifestKeys[module.Key]; duplicate {
			t.Fatalf("pack.json contains duplicate module key %q", module.Key)
		}
		manifestKeys[module.Key] = struct{}{}
	}
	guiKeys := make(map[string]struct{}, len(moduleDefs))
	for _, module := range moduleDefs {
		if _, duplicate := guiKeys[module.Key]; duplicate {
			t.Fatalf("GUI moduleDefs contains duplicate module key %q", module.Key)
		}
		guiKeys[module.Key] = struct{}{}
	}
	var missing, extra []string
	for key := range manifestKeys {
		if _, ok := guiKeys[key]; !ok {
			missing = append(missing, key)
		}
	}
	for key := range guiKeys {
		if _, ok := manifestKeys[key]; !ok {
			extra = append(extra, key)
		}
	}
	if len(missing) != 0 || len(extra) != 0 {
		sort.Strings(missing)
		sort.Strings(extra)
		t.Fatalf("manifest/GUI module key mismatch: missing from GUI=%v extra in GUI=%v", missing, extra)
	}
	if len(manifestKeys) != 18 {
		t.Fatalf("expected 18 manifest modules, got %d", len(manifestKeys))
	}
}
