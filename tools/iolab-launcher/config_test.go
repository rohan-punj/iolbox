package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLauncherConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Missing file -> defaults.
	got := loadLauncherConfig(dir)
	want := defaultLauncherConfig()
	if got != want {
		t.Fatalf("loadLauncherConfig on missing file = %+v, want defaults %+v", got, want)
	}

	// Save then reload should round-trip exactly.
	cfg := launcherConfig{CPUs: 8, RAMMB: 8192, Deployment: "wsl", ImagesDir: "C:\\custom\\images", LabsDir: "C:\\custom\\labs"}
	if err := saveLauncherConfig(dir, cfg); err != nil {
		t.Fatalf("saveLauncherConfig: %v", err)
	}
	got = loadLauncherConfig(dir)
	if got != cfg {
		t.Fatalf("loadLauncherConfig after save = %+v, want %+v", got, cfg)
	}

	// File must actually exist at the documented name.
	if _, err := os.Stat(filepath.Join(dir, launcherConfigFileName)); err != nil {
		t.Fatalf("expected %s to exist: %v", launcherConfigFileName, err)
	}
}

func TestLauncherConfig_CorruptFileFallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := writeFileAtomic(filepath.Join(dir, launcherConfigFileName), []byte("{not json")); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	got := loadLauncherConfig(dir)
	want := defaultLauncherConfig()
	if got != want {
		t.Fatalf("corrupt config = %+v, want defaults %+v", got, want)
	}
}

func TestLauncherConfig_PartialFileKeepsDefaultsForMissingFields(t *testing.T) {
	dir := t.TempDir()
	// Only cpus set; ramMB/deployment should fall back to defaults.
	if err := writeFileAtomic(filepath.Join(dir, launcherConfigFileName), []byte(`{"cpus":16}`)); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	got := loadLauncherConfig(dir)
	if got.CPUs != 16 {
		t.Errorf("CPUs = %d, want 16", got.CPUs)
	}
	if got.RAMMB != defaultLauncherConfig().RAMMB {
		t.Errorf("RAMMB = %d, want default %d", got.RAMMB, defaultLauncherConfig().RAMMB)
	}
}

func TestNormalizeDeployment(t *testing.T) {
	cases := map[string]string{
		"qemu":    "qemu",
		"wsl":     "wsl",
		"auto":    "auto",
		"":        "qemu",
		"vmware":  "qemu",
		"remote":  "qemu",
		"garbage": "qemu",
	}
	for in, want := range cases {
		if got := normalizeDeployment(in); got != want {
			t.Errorf("normalizeDeployment(%q) = %q, want %q", in, got, want)
		}
	}
}
