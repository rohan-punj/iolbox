package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultDarwinSyncDirsUseUserConfigDir(t *testing.T) {
	images, labs, err := resolveDarwinSyncDirs("", "", func() (string, error) {
		return filepath.Join("/Users", "Rohan Sharma", "Library", "Application Support"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := filepath.Join("/Users", "Rohan Sharma", "Library", "Application Support", "iolbox")
	if images != filepath.Join(wantRoot, "images") || labs != filepath.Join(wantRoot, "labs") {
		t.Fatalf("default Darwin sync dirs = %q/%q", images, labs)
	}
	if _, _, err := resolveDarwinSyncDirs("", "", func() (string, error) {
		return "", errors.New("no config dir")
	}); err == nil {
		t.Fatal("config directory failure was ignored")
	}
}

func TestDarwinSyncRescuesMissingLabsThenHostWins(t *testing.T) {
	root := filepath.Join(t.TempDir(), "M3 Data café")
	imagesDir := filepath.Join(root, "images")
	labsDir := filepath.Join(root, "labs")
	if err := os.MkdirAll(labsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, labsDir, "same.yml", labYAML("same", "host copy wins"))

	cc := newStubControlClient()
	cc.labs["same"] = labYAML("same", "guest copy loses")
	cc.labs["guest-only"] = labYAML("guest-only", "rescued")
	fs := newDarwinFolderSync(imagesDir, labsDir, cc, newStubUploader())
	if _, err := fs.syncLabsOutMissingOnly(); err != nil {
		t.Fatalf("missing-only rescue: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(labsDir, "same.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte(labYAML("same", "host copy wins"))) {
		t.Fatalf("host collision was overwritten: %q", data)
	}
	if _, err := os.Stat(filepath.Join(labsDir, "guest-only.yml")); err != nil {
		t.Fatalf("guest-only lab was not rescued: %v", err)
	}
	if _, err := fs.syncLabsIn(); err != nil {
		t.Fatalf("host-wins import: %v", err)
	}
	if cc.labs["same"] != labYAML("same", "host copy wins") {
		t.Fatalf("host copy was not imported into guest: %q", cc.labs["same"])
	}
}

func TestDarwinSyncDoesNotDeleteStaleHostDocuments(t *testing.T) {
	labsDir := filepath.Join(t.TempDir(), "Labs with space café")
	if err := os.MkdirAll(labsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(labsDir, "offline.yml")
	if err := os.WriteFile(stale, []byte(labYAML("offline", "offline edit")), 0o644); err != nil {
		t.Fatal(err)
	}
	cc := newStubControlClient()
	fs := newDarwinFolderSync(filepath.Join(t.TempDir(), "Images"), labsDir, cc, newStubUploader())
	if _, err := fs.syncLabsIn(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("sync removed a stale host document: %v", err)
	}
	if cc.labs["offline"] == "" {
		t.Fatal("stale host document was not re-imported")
	}
}

func TestDarwinStrictStopSyncReturnsGuestReadFailure(t *testing.T) {
	cc := newStubControlClient()
	cc.labs["lab1"] = labYAML("lab1", "one")
	cc.getErrFor["lab1"] = errors.New("control channel dropped")
	fs := newDarwinFolderSync(filepath.Join(t.TempDir(), "images"), filepath.Join(t.TempDir(), "labs"), cc, nil)
	if _, err := fs.syncLabsOutStrict(); err == nil || !strings.Contains(err.Error(), "control channel dropped") {
		t.Fatalf("strict stop sync error = %v", err)
	}
}

func TestDarwinPreStopSyncPreservesHostCollisionByteForByte(t *testing.T) {
	root := filepath.Join(t.TempDir(), "M3 Data "+string([]rune{0x00e9}))
	imagesDir := filepath.Join(root, "images")
	labsDir := filepath.Join(root, "labs")
	if err := os.MkdirAll(labsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	host := []byte(labYAML("same", "host edit while VM was running"))
	guest := labYAML("same", "different guest state")
	if err := os.WriteFile(filepath.Join(labsDir, "same.yml"), host, 0o644); err != nil {
		t.Fatal(err)
	}
	cc := newStubControlClient()
	cc.labs["same"] = guest
	config := darwinSyncConfig{ImagesDir: imagesDir, LabsDir: labsDir}
	if err := syncDarwinBeforeStop(cc, config); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(labsDir, "same.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, host) {
		t.Fatalf("pre-stop export changed host bytes: got %q want %q", got, host)
	}
}
