package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestSecbenchKeyPathContract(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	guiDir := filepath.Dir(testFile)
	repoRoot := filepath.Clean(filepath.Join(guiDir, "..", "..", "..", "..", "..", ".."))

	cmdDir := filepath.Join(repoRoot, "tools", "secbench-attacks-go", "cmd")
	entries, err := os.ReadDir(cmdDir)
	if err != nil {
		t.Fatalf("read Go command directory: %v", err)
	}
	var cmdKeys []string
	for _, entry := range entries {
		if entry.IsDir() {
			cmdKeys = append(cmdKeys, entry.Name())
		}
	}
	sort.Strings(cmdKeys)

	data, err := os.ReadFile(filepath.Join(guiDir, "..", "pack.json"))
	if err != nil {
		t.Fatalf("read pack.json: %v", err)
	}
	var manifest struct {
		Modules []struct {
			Key    string `json:"key"`
			Script string `json:"script"`
		} `json:"modules"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse pack.json: %v", err)
	}
	manifestModules := make(map[string]string, len(manifest.Modules))
	for _, module := range manifest.Modules {
		if _, duplicate := manifestModules[module.Key]; duplicate {
			t.Fatalf("pack.json contains duplicate module key %q", module.Key)
		}
		manifestModules[module.Key] = module.Script
	}

	guiModules := make(map[string]string, len(moduleDefs))
	for _, module := range moduleDefs {
		if _, duplicate := guiModules[module.Key]; duplicate {
			t.Fatalf("GUI moduleDefs contains duplicate module key %q", module.Key)
		}
		guiModules[module.Key] = module.Script
	}

	wantKeys := strings.Join(cmdKeys, ",")
	if len(cmdKeys) != len(manifestModules) || len(cmdKeys) != len(guiModules) {
		t.Fatalf("key counts differ: cmd=%d manifest=%d GUI=%d (cmd keys: %s)", len(cmdKeys), len(manifestModules), len(guiModules), wantKeys)
	}
	for _, key := range cmdKeys {
		if script, ok := guiModules[key]; !ok || script != key {
			t.Fatalf("GUI module for %q = %q, want Script=%q", key, script, key)
		}
		if script, ok := manifestModules[key]; !ok || script != "bin/"+key {
			t.Fatalf("manifest module for %q = %q, want script=%q", key, script, "bin/"+key)
		}
		if _, err := os.Stat(filepath.Join(cmdDir, key)); err != nil {
			t.Fatalf("cmd/%s directory is not accessible: %v", key, err)
		}
	}

	binDir := os.Getenv("SECBENCH_BIN_DIR")
	if binDir == "" {
		t.Skip("SECBENCH_BIN_DIR is unset; binary-name equality is checked when the staged install is supplied")
	}
	installed, err := os.ReadDir(binDir)
	if err != nil {
		t.Fatalf("read staged secbench bin directory %q: %v", binDir, err)
	}
	installedNames := make(map[string]struct{}, len(installed))
	for _, entry := range installed {
		if entry.IsDir() {
			t.Fatalf("staged secbench bin contains directory %q", entry.Name())
		}
		installedNames[entry.Name()] = struct{}{}
	}
	if len(installedNames) != len(cmdKeys) {
		t.Fatalf("staged binary count = %d, want %d", len(installedNames), len(cmdKeys))
	}
	for _, key := range cmdKeys {
		if _, ok := installedNames[key]; !ok {
			t.Fatalf("staged binary basename %q is missing", key)
		}
	}
	if len(installedNames) != len(manifestModules) {
		t.Fatalf("staged binary names do not match manifest keys: %v", fmt.Sprint(installedNames))
	}
}
