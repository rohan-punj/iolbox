package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// ---- Stubs ---------------------------------------------------------------

type stubControlClient struct {
	mu sync.Mutex

	registered []string // guest paths passed to registerImage
	registerErrFor map[string]error

	savedLabs []json.RawMessage
	saveErr   error

	labs map[string]json.RawMessage // id -> doc, the guest's current lab set
	getErrFor map[string]error
	listErr   error
}

func newStubControlClient() *stubControlClient {
	return &stubControlClient{
		labs:           make(map[string]json.RawMessage),
		registerErrFor: make(map[string]error),
		getErrFor:      make(map[string]error),
	}
}

func (s *stubControlClient) registerImage(guestPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err, ok := s.registerErrFor[guestPath]; ok {
		return err
	}
	s.registered = append(s.registered, guestPath)
	return nil
}

func (s *stubControlClient) saveLab(doc json.RawMessage) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return "", s.saveErr
	}
	id, _ := labDocID(doc)
	s.savedLabs = append(s.savedLabs, doc)
	s.labs[id] = doc
	return id, nil
}

func (s *stubControlClient) listLabIDs() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listErr != nil {
		return nil, s.listErr
	}
	ids := make([]string, 0, len(s.labs))
	for id := range s.labs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func (s *stubControlClient) getLab(id string) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err, ok := s.getErrFor[id]; ok {
		return nil, err
	}
	doc, ok := s.labs[id]
	if !ok {
		return nil, fmt.Errorf("no such lab %q", id)
	}
	return doc, nil
}

type stubUploader struct {
	mu sync.Mutex

	uploaded map[string][]byte // filename -> body bytes seen
	failFor  map[string]error
	pathFor  func(filename string) string
}

func newStubUploader() *stubUploader {
	return &stubUploader{
		uploaded: make(map[string][]byte),
		failFor:  make(map[string]error),
	}
}

func (u *stubUploader) upload(filename string, body io.Reader) (string, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if err, ok := u.failFor[filename]; ok {
		return "", err
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	u.uploaded[filename] = data
	if u.pathFor != nil {
		return u.pathFor(filename), nil
	}
	return "/opt/iolab/images/" + filename, nil
}

// ---- helpers ---------------------------------------------------------

func mustWriteFile(t *testing.T, dir, name string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func labJSON(id, name string) string {
	return fmt.Sprintf(`{"id":%q,"version":1,"name":%q,"nodes":[],"links":[]}`, id, name)
}

// ---- syncImagesIn ------------------------------------------------------

func TestSyncImagesIn_UploadsAndRegisters(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "router1.bin", "BINARY-DATA-1")
	mustWriteFile(t, dir, "router2.iol", "BINARY-DATA-2")
	mustWriteFile(t, dir, "readme.txt", "not an image")
	mustWriteFile(t, dir, "SWITCH.BIN", "upper-ext-data") // case-insensitive ext

	cc := newStubControlClient()
	up := newStubUploader()
	fs := newFolderSync(dir, t.TempDir(), cc, up)

	count, err := fs.syncImagesIn()
	if err != nil {
		t.Fatalf("syncImagesIn: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
	if len(up.uploaded) != 3 {
		t.Errorf("uploaded %d files, want 3: %v", len(up.uploaded), up.uploaded)
	}
	if string(up.uploaded["router1.bin"]) != "BINARY-DATA-1" {
		t.Errorf("router1.bin body mismatch: %q", up.uploaded["router1.bin"])
	}
	if _, ok := up.uploaded["readme.txt"]; ok {
		t.Error("readme.txt should not have been uploaded")
	}
	if len(cc.registered) != 3 {
		t.Errorf("registered %d images, want 3: %v", len(cc.registered), cc.registered)
	}
}

func TestSyncImagesIn_ContinuesPastPerFileErrors(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "good.bin", "ok")
	mustWriteFile(t, dir, "bad.bin", "will fail upload")

	cc := newStubControlClient()
	up := newStubUploader()
	up.failFor["bad.bin"] = errors.New("simulated upload failure")
	fs := newFolderSync(dir, t.TempDir(), cc, up)

	count, err := fs.syncImagesIn()
	if err != nil {
		t.Fatalf("syncImagesIn: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 (only good.bin should succeed)", count)
	}
	if _, ok := up.uploaded["good.bin"]; !ok {
		t.Error("good.bin should have been uploaded despite bad.bin failing")
	}
}

func TestSyncImagesIn_RegisterFailureIsNonFatal(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "a.bin", "data-a")
	mustWriteFile(t, dir, "b.bin", "data-b")

	cc := newStubControlClient()
	up := newStubUploader()
	up.pathFor = func(filename string) string { return "/opt/iolab/images/" + filename }
	cc.registerErrFor["/opt/iolab/images/a.bin"] = errors.New("registry rejected")
	fs := newFolderSync(dir, t.TempDir(), cc, up)

	count, err := fs.syncImagesIn()
	if err != nil {
		t.Fatalf("syncImagesIn: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if len(cc.registered) != 1 || cc.registered[0] != "/opt/iolab/images/b.bin" {
		t.Errorf("registered = %v, want only b.bin's path", cc.registered)
	}
}

// ---- syncLabsIn ----------------------------------------------------------

func TestSyncLabsIn_SavesValidSkipsInvalid(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "lab1.json", labJSON("lab1", "First Lab"))
	mustWriteFile(t, dir, "lab2.json", labJSON("lab2", "Second Lab"))
	mustWriteFile(t, dir, "malformed.json", `{not valid json`)
	mustWriteFile(t, dir, "noid.json", `{"name":"missing id field"}`)
	mustWriteFile(t, dir, "notes.txt", "ignore me")

	cc := newStubControlClient()
	up := newStubUploader()
	fs := newFolderSync(t.TempDir(), dir, cc, up)

	count, err := fs.syncLabsIn()
	if err != nil {
		t.Fatalf("syncLabsIn: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if len(cc.savedLabs) != 2 {
		t.Errorf("saved %d labs, want 2", len(cc.savedLabs))
	}
	ids, _ := cc.listLabIDs()
	want := []string{"lab1", "lab2"}
	if !stringSlicesEqual(ids, want) {
		t.Errorf("saved lab ids = %v, want %v", ids, want)
	}
}

func TestSyncLabsIn_EmptyDirNoError(t *testing.T) {
	dir := t.TempDir()
	cc := newStubControlClient()
	up := newStubUploader()
	fs := newFolderSync(t.TempDir(), dir, cc, up)

	count, err := fs.syncLabsIn()
	if err != nil {
		t.Fatalf("syncLabsIn on empty dir: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

// ---- recordSeedLabIDs + syncLabsOut --------------------------------------

func TestSyncLabsOut_WritesNonSeedSkipsSeed(t *testing.T) {
	labsDir := t.TempDir()
	cc := newStubControlClient()
	up := newStubUploader()
	fs := newFolderSync(t.TempDir(), labsDir, cc, up)

	// Seed the guest with two "starter" labs before recordSeedLabIDs.
	cc.labs["starter1"] = json.RawMessage(labJSON("starter1", "Starter One"))
	cc.labs["starter2"] = json.RawMessage(labJSON("starter2", "Starter Two"))

	if err := fs.recordSeedLabIDs(); err != nil {
		t.Fatalf("recordSeedLabIDs: %v", err)
	}

	// Now the user creates a new lab in the GUI (not a seed).
	cc.labs["user-lab-1"] = json.RawMessage(labJSON("user-lab-1", "My Lab"))

	count, err := fs.syncLabsOut()
	if err != nil {
		t.Fatalf("syncLabsOut: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 (only the non-seed lab)", count)
	}

	if _, err := os.Stat(filepath.Join(labsDir, "user-lab-1.json")); err != nil {
		t.Errorf("expected user-lab-1.json to be written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(labsDir, "starter1.json")); err == nil {
		t.Error("starter1.json should NOT have been written (it's a seed lab)")
	}
	if _, err := os.Stat(filepath.Join(labsDir, "starter2.json")); err == nil {
		t.Error("starter2.json should NOT have been written (it's a seed lab)")
	}

	// Content should be pretty-printed JSON containing the id.
	data, err := os.ReadFile(filepath.Join(labsDir, "user-lab-1.json"))
	if err != nil {
		t.Fatalf("read written lab: %v", err)
	}
	if !bytes.Contains(data, []byte(`"user-lab-1"`)) {
		t.Errorf("written file doesn't contain the id: %s", data)
	}
	if !bytes.Contains(data, []byte("\n")) {
		t.Error("expected pretty-printed (indented, multi-line) JSON")
	}
}

func TestSyncLabsOut_SeedIDCollisionDoesNotClobberExistingFile(t *testing.T) {
	labsDir := t.TempDir()
	cc := newStubControlClient()
	up := newStubUploader()
	fs := newFolderSync(t.TempDir(), labsDir, cc, up)

	cc.labs["starter1"] = json.RawMessage(labJSON("starter1", "Starter One"))
	if err := fs.recordSeedLabIDs(); err != nil {
		t.Fatalf("recordSeedLabIDs: %v", err)
	}

	// Pre-existing user file that happens to collide with a seed id — must
	// not be touched. (This scenario would only arise if listLabIDs somehow
	// returned a seed id as "current" AND it wasn't filtered — but the guard
	// in syncLabsOut is belt-and-suspenders: isSeedID(id) already skips it
	// before getLab is even called. This test locks in that behavior.)
	existing := labJSON("starter1", "user's local copy - should not change")
	mustWriteFile(t, labsDir, "starter1.json", existing)

	count, err := fs.syncLabsOut()
	if err != nil {
		t.Fatalf("syncLabsOut: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 (starter1 is a seed id)", count)
	}

	data, err := os.ReadFile(filepath.Join(labsDir, "starter1.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != existing {
		t.Errorf("existing file was modified: got %q, want unchanged %q", data, existing)
	}
}

func TestSyncLabsOut_SkipsUnsafeIDs(t *testing.T) {
	labsDir := t.TempDir()
	cc := newStubControlClient()
	up := newStubUploader()
	fs := newFolderSync(t.TempDir(), labsDir, cc, up)

	if err := fs.recordSeedLabIDs(); err != nil {
		t.Fatalf("recordSeedLabIDs: %v", err)
	}

	cc.labs["../escape"] = json.RawMessage(`{"id":"../escape"}`)
	cc.labs["good-id"] = json.RawMessage(labJSON("good-id", "Fine"))

	count, err := fs.syncLabsOut()
	if err != nil {
		t.Fatalf("syncLabsOut: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 (only good-id)", count)
	}
	entries, _ := os.ReadDir(labsDir)
	for _, e := range entries {
		if strings.Contains(e.Name(), "..") {
			t.Errorf("unsafe filename escaped into labs dir: %s", e.Name())
		}
	}
}

// ---- labDocID --------------------------------------------------------

func TestLabDocID(t *testing.T) {
	cases := []struct {
		raw     string
		wantID  string
		wantOK  bool
	}{
		{labJSON("abc", "x"), "abc", true},
		{`{"name":"no id"}`, "", false},
		{`{"id":123}`, "", false}, // id must be a string
		{`{"id":""}`, "", false},
		{`not json`, "", false},
		{`["array","not","object"]`, "", false},
	}
	for _, c := range cases {
		id, ok := labDocID([]byte(c.raw))
		if id != c.wantID || ok != c.wantOK {
			t.Errorf("labDocID(%q) = (%q,%v), want (%q,%v)", c.raw, id, ok, c.wantID, c.wantOK)
		}
	}
}

// ---- isImageFile / isPlainLabID ------------------------------------------

func TestIsImageFile(t *testing.T) {
	cases := map[string]bool{
		"router.bin":  true,
		"router.iol":  true,
		"ROUTER.BIN":  true,
		"router.IOL":  true,
		"readme.txt":  false,
		"noext":       false,
		"archive.zip": false,
	}
	for name, want := range cases {
		if got := isImageFile(name); got != want {
			t.Errorf("isImageFile(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestIsPlainLabID(t *testing.T) {
	cases := map[string]bool{
		"lab1":        true,
		"my-lab_2":    true,
		"":            false,
		".":           false,
		"..":          false,
		"../escape":   false,
		"a/b":         false,
		"a b":         false,
		"lab.json":    false,
	}
	for id, want := range cases {
		if got := isPlainLabID(id); got != want {
			t.Errorf("isPlainLabID(%q) = %v, want %v", id, got, want)
		}
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
