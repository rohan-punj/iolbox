package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- Stubs ---------------------------------------------------------------

type stubControlClient struct {
	mu sync.Mutex

	registered     []string // guest paths passed to registerImage
	registerErrFor map[string]error
	hintsSeen      map[string]imageFingerprint // guest path -> hint it was called with
	hintOKSeen     map[string]bool
	resultFor      map[string]imageFingerprint // guest path -> fingerprint to return (default: derived)

	savedLabs []string
	saveErr   error

	labs      map[string]string // id -> doc text, the guest's current lab set
	getErrFor map[string]error
	listErr   error
}

func newStubControlClient() *stubControlClient {
	return &stubControlClient{
		labs:           make(map[string]string),
		registerErrFor: make(map[string]error),
		getErrFor:      make(map[string]error),
		hintsSeen:      make(map[string]imageFingerprint),
		hintOKSeen:     make(map[string]bool),
		resultFor:      make(map[string]imageFingerprint),
	}
}

// registerImage simulates the supervisor: it "trusts" hintOK exactly like the
// real inspectOrTrustHint would (returns the hint's fingerprint verbatim when
// given one), so tests can prove syncImagesIn plumbs a hint through by
// checking whether the result equals the hint. resultFor lets a test override
// the return value to simulate the server rejecting a stale hint.
func (s *stubControlClient) registerImage(guestPath string, hint imageFingerprint, hintOK bool) (imageFingerprint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hintsSeen[guestPath] = hint
	s.hintOKSeen[guestPath] = hintOK
	if err, ok := s.registerErrFor[guestPath]; ok {
		return imageFingerprint{}, err
	}
	s.registered = append(s.registered, guestPath)
	if r, ok := s.resultFor[guestPath]; ok {
		return r, nil
	}
	if hintOK {
		return hint, nil
	}
	return imageFingerprint{SHA256: fakeSha(guestPath), Arch: "x86_64", Class: "l3"}, nil
}

// fakeSha derives a deterministic 64-hex-char stand-in "hash" from guestPath,
// just distinct enough per input for tests to assert on; not a real sha256.
func fakeSha(guestPath string) string {
	const hex = "0123456789abcdef"
	b := make([]byte, 64)
	for i := range b {
		b[i] = hex[(int(guestPath[i%len(guestPath)])+i)%16]
	}
	return string(b)
}

func (s *stubControlClient) saveLab(doc string) (string, error) {
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

func (s *stubControlClient) getLab(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err, ok := s.getErrFor[id]; ok {
		return "", err
	}
	doc, ok := s.labs[id]
	if !ok {
		return "", fmt.Errorf("no such lab %q", id)
	}
	return doc, nil
}

type stubUploader struct {
	mu sync.Mutex

	uploaded   map[string][]byte // filename -> body bytes seen
	mtimesSeen map[string]int64  // filename -> mtimeNs it was called with
	failFor    map[string]error
	pathFor    func(filename string) string
}

func newStubUploader() *stubUploader {
	return &stubUploader{
		uploaded:   make(map[string][]byte),
		mtimesSeen: make(map[string]int64),
		failFor:    make(map[string]error),
	}
}

func (u *stubUploader) upload(filename string, body io.Reader, mtimeNs int64) (string, error) {
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
	u.mtimesSeen[filename] = mtimeNs
	if u.pathFor != nil {
		return u.pathFor(filename), nil
	}
	return "/opt/iolbox/images/" + filename, nil
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

// labYAML is the native format: a lab document as YAML text.
func labYAML(id, name string) string {
	return fmt.Sprintf("version: 1\nid: %s\nname: %s\nnodes: []\nlinks: []\n", id, name)
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
	up.pathFor = func(filename string) string { return "/opt/iolbox/images/" + filename }
	cc.registerErrFor["/opt/iolbox/images/a.bin"] = errors.New("registry rejected")
	fs := newFolderSync(dir, t.TempDir(), cc, up)

	count, err := fs.syncImagesIn()
	if err != nil {
		t.Fatalf("syncImagesIn: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if len(cc.registered) != 1 || cc.registered[0] != "/opt/iolbox/images/b.bin" {
		t.Errorf("registered = %v, want only b.bin's path", cc.registered)
	}
}

// ---- image fingerprint cache (re-upload across simulated boots) ----------

// TestSyncImagesIn_SecondBootReusesFingerprint simulates two launcher boots
// against the SAME images\ folder: the first boot has no cache, so the
// register call carries no hint; the second boot (unchanged source file, same
// mtime — exactly what happens when snapshot=on wipes the guest disk but the
// Windows-side file is untouched) must supply a hint whose fingerprint
// matches what boot 1's register call returned. This is the crux of the
// fix: proving the cache survives a fresh folderSync/uploader/controlClient
// (a new "boot") and that hintOK is true the second time, i.e. the register
// path can skip a real hash.
func TestSyncImagesIn_SecondBootReusesFingerprint(t *testing.T) {
	imagesDir := t.TempDir()
	mustWriteFile(t, imagesDir, "router.bin", "same-bytes-both-boots")

	// --- Boot 1: cold cache ---
	cc1 := newStubControlClient()
	up1 := newStubUploader()
	fs1 := newFolderSync(imagesDir, t.TempDir(), cc1, up1)
	if _, err := fs1.syncImagesIn(); err != nil {
		t.Fatalf("boot1 syncImagesIn: %v", err)
	}
	guestPath := "/opt/iolbox/images/router.bin"
	if cc1.hintOKSeen[guestPath] {
		t.Fatalf("boot1: expected no hint on a cold cache, got one: %+v", cc1.hintsSeen[guestPath])
	}
	boot1Result := cc1.resultFor[guestPath]
	if boot1Result == (imageFingerprint{}) {
		// resultFor wasn't preset, so read back what registerImage computed via fakeSha.
		boot1Result = imageFingerprint{SHA256: fakeSha(guestPath), Arch: "x86_64", Class: "l3"}
	}

	// The guest disk (including the guest's own registry) is now wiped — a
	// fresh controlClient + uploader simulate a brand new boot. Only the
	// Windows-side imagesDir (and its sidecar cache file) survives.
	cc2 := newStubControlClient()
	up2 := newStubUploader()
	fs2 := newFolderSync(imagesDir, t.TempDir(), cc2, up2)
	if _, err := fs2.syncImagesIn(); err != nil {
		t.Fatalf("boot2 syncImagesIn: %v", err)
	}

	if !cc2.hintOKSeen[guestPath] {
		t.Fatalf("boot2: expected a cache hint (unchanged file, same mtime), got none")
	}
	gotHint := cc2.hintsSeen[guestPath]
	if gotHint.SHA256 != boot1Result.SHA256 || gotHint.Arch != boot1Result.Arch || gotHint.Class != boot1Result.Class {
		t.Fatalf("boot2 hint = %+v, want boot1's fingerprint %+v", gotHint, boot1Result)
	}

	// Also assert the upload itself carried a stable mtime both times, since
	// that's the mechanism that keeps (size, mtime) matching across boots.
	fi, err := os.Stat(filepath.Join(imagesDir, "router.bin"))
	if err != nil {
		t.Fatal(err)
	}
	want := fi.ModTime().UnixNano()
	if up1.mtimesSeen["router.bin"] != want || up2.mtimesSeen["router.bin"] != want {
		t.Fatalf("upload mtime not stable across boots: boot1=%d boot2=%d want=%d",
			up1.mtimesSeen["router.bin"], up2.mtimesSeen["router.bin"], want)
	}
}

// TestSyncImagesIn_ChangedFileGetsNoHint proves a changed source file misses
// the cache: overwriting the image after boot 1 (which also changes its
// mtime) must NOT produce a hint on boot 2, so the (simulated) guest always
// re-hashes genuinely different content rather than trusting a stale
// fingerprint.
func TestSyncImagesIn_ChangedFileGetsNoHint(t *testing.T) {
	imagesDir := t.TempDir()
	mustWriteFile(t, imagesDir, "router.bin", "version-one")

	cc1 := newStubControlClient()
	up1 := newStubUploader()
	fs1 := newFolderSync(imagesDir, t.TempDir(), cc1, up1)
	if _, err := fs1.syncImagesIn(); err != nil {
		t.Fatalf("boot1: %v", err)
	}

	// User replaces the image with different content. Force a distinct mtime
	// (filesystem timestamp granularity could otherwise make two writes in
	// the same test look identical).
	mustWriteFile(t, imagesDir, "router.bin", "version-two-different-length")
	newTime := time.Now().Add(1 * time.Hour)
	if err := os.Chtimes(filepath.Join(imagesDir, "router.bin"), newTime, newTime); err != nil {
		t.Fatal(err)
	}

	cc2 := newStubControlClient()
	up2 := newStubUploader()
	fs2 := newFolderSync(imagesDir, t.TempDir(), cc2, up2)
	if _, err := fs2.syncImagesIn(); err != nil {
		t.Fatalf("boot2: %v", err)
	}

	guestPath := "/opt/iolbox/images/router.bin"
	if cc2.hintOKSeen[guestPath] {
		t.Fatalf("boot2: changed file must not produce a cache hint, got %+v", cc2.hintsSeen[guestPath])
	}
}

// ---- syncLabsIn ----------------------------------------------------------

func TestSyncLabsIn_SavesValidSkipsInvalid(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "lab1.yml", labYAML("lab1", "First Lab"))   // native YAML
	mustWriteFile(t, dir, "lab2.json", labJSON("lab2", "Second Lab")) // legacy JSON still read
	mustWriteFile(t, dir, "malformed.yml", `: : not valid`)
	mustWriteFile(t, dir, "noid.yml", "name: missing id field\n")
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
	cc.labs["starter1"] = labYAML("starter1", "Starter One")
	cc.labs["starter2"] = labYAML("starter2", "Starter Two")

	if err := fs.recordSeedLabIDs(); err != nil {
		t.Fatalf("recordSeedLabIDs: %v", err)
	}

	// Now the user creates a new lab in the GUI (not a seed).
	cc.labs["user-lab-1"] = labYAML("user-lab-1", "My Lab")

	count, err := fs.syncLabsOut()
	if err != nil {
		t.Fatalf("syncLabsOut: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 (only the non-seed lab)", count)
	}

	// Written as <id>.yml, verbatim (the guest's doc text is already formatted).
	if _, err := os.Stat(filepath.Join(labsDir, "user-lab-1.yml")); err != nil {
		t.Errorf("expected user-lab-1.yml to be written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(labsDir, "starter1.yml")); err == nil {
		t.Error("starter1.yml should NOT have been written (it's a seed lab)")
	}
	if _, err := os.Stat(filepath.Join(labsDir, "starter2.yml")); err == nil {
		t.Error("starter2.yml should NOT have been written (it's a seed lab)")
	}

	data, err := os.ReadFile(filepath.Join(labsDir, "user-lab-1.yml"))
	if err != nil {
		t.Fatalf("read written lab: %v", err)
	}
	if string(data) != labYAML("user-lab-1", "My Lab") {
		t.Errorf("written file not verbatim: %q", data)
	}
}

func TestSyncLabsOut_SeedIDCollisionDoesNotClobberExistingFile(t *testing.T) {
	labsDir := t.TempDir()
	cc := newStubControlClient()
	up := newStubUploader()
	fs := newFolderSync(t.TempDir(), labsDir, cc, up)

	cc.labs["starter1"] = labYAML("starter1", "Starter One")
	if err := fs.recordSeedLabIDs(); err != nil {
		t.Fatalf("recordSeedLabIDs: %v", err)
	}

	// Pre-existing user file that happens to collide with a seed id — must
	// not be touched. The guard in syncLabsOut is belt-and-suspenders:
	// isSeedID(id) already skips it before getLab is even called.
	existing := labYAML("starter1", "user's local copy - should not change")
	mustWriteFile(t, labsDir, "starter1.yml", existing)

	count, err := fs.syncLabsOut()
	if err != nil {
		t.Fatalf("syncLabsOut: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 (starter1 is a seed id)", count)
	}

	data, err := os.ReadFile(filepath.Join(labsDir, "starter1.yml"))
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

	cc.labs["../escape"] = `{"id":"../escape"}`
	cc.labs["good-id"] = labYAML("good-id", "Fine")

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
		raw    string
		wantID string
		wantOK bool
	}{
		{labYAML("abc", "x"), "abc", true},   // native YAML
		{`id: quoted`, "quoted", true},       // bare YAML id line
		{"name: R1\nid: 'q2'\n", "q2", true}, // quoted YAML id, not first line
		{labJSON("abc", "x"), "abc", true},   // legacy JSON
		{"name: no id here\n", "", false},    // YAML, no id
		{`{"name":"no id"}`, "", false},      // JSON, no id
		{`{"id":123}`, "", false},            // JSON id must be a string
		{`{"id":""}`, "", false},             // JSON empty id
		{`not a document`, "", false},
	}
	for _, c := range cases {
		id, ok := labDocID(c.raw)
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
		"lab1":      true,
		"my-lab_2":  true,
		"":          false,
		".":         false,
		"..":        false,
		"../escape": false,
		"a/b":       false,
		"a b":       false,
		"lab.json":  false,
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
