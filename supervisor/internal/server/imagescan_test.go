package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
)

// fakeImage builds a >1KB pseudo-image body. withL2Marker embeds the
// "linux_l2" build string the class sniffer keys on.
func fakeImage(seed byte, withL2Marker bool) []byte {
	b := bytes.Repeat([]byte{seed}, 4096)
	if withL2Marker {
		copy(b[2000:], "x86_64_crb_linux_l2-adventerprisek9-ms")
	}
	return b
}

func writeImageFile(t *testing.T, dir, name string, body []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func newScanServer(t *testing.T, imageDir string) *Server {
	t.Helper()
	return New(Config{ControlAddr: "127.0.0.1:0", ImageDir: imageDir, RunDir: t.TempDir(), Version: "test"})
}

func listImages(t *testing.T, s *Server) map[string]protocol.ImageInfo {
	t.Helper()
	resp := dispatch(t, s, "image.list", nil)
	if !resp.OK {
		t.Fatalf("image.list: %+v", resp.Error)
	}
	var r protocol.ImageListResult
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		t.Fatal(err)
	}
	out := map[string]protocol.ImageInfo{}
	for _, img := range r.Images {
		out[img.Filename] = img
	}
	return out
}

func readCacheFile(t *testing.T, imageDir string) map[string]imageCacheEntry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(imageDir, imageCacheName))
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	cache := map[string]imageCacheEntry{}
	if err := json.Unmarshal(data, &cache); err != nil {
		t.Fatalf("cache json: %v", err)
	}
	return cache
}

// TestRescanRegistersImages: a cold rescan (no sidecar cache) fingerprints
// every image file in ImageDir, classifies L2 vs L3, and writes the cache.
func TestRescanRegistersImages(t *testing.T) {
	dir := t.TempDir()
	l3 := fakeImage('a', false)
	writeImageFile(t, dir, "r1-l3.bin", l3)
	writeImageFile(t, dir, "sw1_l2.iol", fakeImage('b', true))

	s := newScanServer(t, dir)
	s.rescanImages()

	images := listImages(t, s)
	if len(images) != 2 {
		t.Fatalf("expected 2 registered images, got %+v", images)
	}
	wantSum := sha256.Sum256(l3)
	wantSha := hex.EncodeToString(wantSum[:])
	got := images["r1-l3.bin"]
	if got.SHA256 != wantSha || got.ID != wantSha[:16] || got.Class != "l3" || got.Size != int64(len(l3)) {
		t.Fatalf("r1-l3.bin: %+v (want sha %s, class l3)", got, wantSha)
	}
	if images["sw1_l2.iol"].Class != "l2" {
		t.Fatalf("sw1_l2.iol should sniff as l2: %+v", images["sw1_l2.iol"])
	}

	cache := readCacheFile(t, dir)
	if len(cache) != 2 || cache["r1-l3.bin"].SHA256 != wantSha {
		t.Fatalf("sidecar cache not written correctly: %+v", cache)
	}
}

// TestRescanUsesCache: a second boot must trust the sidecar for an unchanged
// (filename, size, mtime) and skip the re-hash. Proven by swapping the file
// body for different same-size content and restoring the mtime — the rescan
// must report the CACHED sha, i.e. it never re-read the file.
func TestRescanUsesCache(t *testing.T) {
	dir := t.TempDir()
	orig := fakeImage('a', false)
	path := writeImageFile(t, dir, "r1.bin", orig)

	s1 := newScanServer(t, dir)
	s1.rescanImages()
	origSum := sha256.Sum256(orig)
	origSha := hex.EncodeToString(origSum[:])

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	writeImageFile(t, dir, "r1.bin", fakeImage('z', false)) // same size, new content
	if err := os.Chtimes(path, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}

	s2 := newScanServer(t, dir)
	s2.rescanImages()
	got := listImages(t, s2)["r1.bin"]
	if got.SHA256 != origSha {
		t.Fatalf("expected cache hit (sha %s), got re-hash (%s)", origSha, got.SHA256)
	}
}

// TestRescanRehashesChangedFile: a changed mtime or size invalidates the
// cache entry — the rescan re-fingerprints and updates the sidecar.
func TestRescanRehashesChangedFile(t *testing.T) {
	dir := t.TempDir()
	path := writeImageFile(t, dir, "r1.bin", fakeImage('a', false))

	s1 := newScanServer(t, dir)
	s1.rescanImages()

	// Replace with different-size content (mtime alone can be too coarse to
	// differ within one test run on some filesystems).
	repl := fakeImage('b', true)[:2048]
	writeImageFile(t, dir, "r1.bin", repl)
	// Force a distinct mtime regardless of filesystem timestamp granularity.
	fi, _ := os.Stat(path)
	if err := os.Chtimes(path, fi.ModTime().Add(1e9), fi.ModTime().Add(1e9)); err != nil {
		t.Fatal(err)
	}

	s2 := newScanServer(t, dir)
	s2.rescanImages()
	got := listImages(t, s2)["r1.bin"]
	wantSum := sha256.Sum256(repl)
	wantSha := hex.EncodeToString(wantSum[:])
	if got.SHA256 != wantSha || got.Class != "l2" || got.Size != int64(len(repl)) {
		t.Fatalf("changed file not re-fingerprinted: %+v (want sha %s)", got, wantSha)
	}
	if cache := readCacheFile(t, dir); cache["r1.bin"].SHA256 != wantSha {
		t.Fatalf("sidecar not updated after re-hash: %+v", cache["r1.bin"])
	}
}

// TestRescanSkipsNonImageFiles: .partial upload temps, the sidecar itself,
// stray files, and subdirectories never enter the registry, and stale cache
// entries for deleted files are pruned on rewrite.
func TestRescanSkipsNonImageFiles(t *testing.T) {
	dir := t.TempDir()
	writeImageFile(t, dir, "good.bin", fakeImage('a', false))
	writeImageFile(t, dir, "upload.bin.partial", fakeImage('b', false))
	writeImageFile(t, dir, "notes.txt", []byte("not an image"))
	writeImageFile(t, dir, ".bin", fakeImage('c', false)) // degenerate dotfile
	if err := os.Mkdir(filepath.Join(dir, "sub.bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-seed a cache entry for a file that no longer exists.
	stale, _ := json.Marshal(map[string]imageCacheEntry{"gone.bin": {Size: 1, SHA256: "x"}})
	if err := os.WriteFile(filepath.Join(dir, imageCacheName), stale, 0o644); err != nil {
		t.Fatal(err)
	}

	s := newScanServer(t, dir)
	s.rescanImages()

	images := listImages(t, s)
	if len(images) != 1 || images["good.bin"].Filename == "" {
		t.Fatalf("only good.bin should register, got %+v", images)
	}
	cache := readCacheFile(t, dir)
	if _, ok := cache["gone.bin"]; ok || len(cache) != 1 {
		t.Fatalf("stale cache entries must be pruned: %+v", cache)
	}
}

// TestRescanEmptyOrMissingDir: no ImageDir configured, or a dir that doesn't
// exist yet (fresh runtime before the first upload), is a silent no-op.
func TestRescanEmptyOrMissingDir(t *testing.T) {
	s := newScanServer(t, filepath.Join(t.TempDir(), "does-not-exist"))
	s.rescanImages() // must not panic or create anything
	if n := len(listImages(t, s)); n != 0 {
		t.Fatalf("expected empty registry, got %d", n)
	}

	s2 := newScanServer(t, "")
	s2.rescanImages()
	if n := len(listImages(t, s2)); n != 0 {
		t.Fatalf("expected empty registry with no ImageDir, got %d", n)
	}
}

// TestRegisterSeedsCache: image.register on a file inside ImageDir writes the
// sidecar entry, so the first boot after an upload is already a cache hit. A
// register of a path OUTSIDE ImageDir must not pollute the cache.
func TestRegisterSeedsCache(t *testing.T) {
	dir := t.TempDir()
	body := fakeImage('a', true)
	path := writeImageFile(t, dir, "up.bin", body)

	s := newScanServer(t, dir)
	resp := dispatch(t, s, "image.register", protocol.ImageRegisterArgs{Path: path})
	if !resp.OK {
		t.Fatalf("image.register: %+v", resp.Error)
	}

	wantSum := sha256.Sum256(body)
	wantSha := hex.EncodeToString(wantSum[:])
	cache := readCacheFile(t, dir)
	ent, ok := cache["up.bin"]
	if !ok || ent.SHA256 != wantSha || ent.Class != "l2" || ent.Size != int64(len(body)) {
		t.Fatalf("register must seed the sidecar cache: %+v", cache)
	}

	// Outside ImageDir: registered in memory, but no cache entry.
	other := t.TempDir()
	outPath := writeImageFile(t, other, "elsewhere.bin", fakeImage('b', false))
	if resp := dispatch(t, s, "image.register", protocol.ImageRegisterArgs{Path: outPath}); !resp.OK {
		t.Fatalf("image.register outside dir: %+v", resp.Error)
	}
	if cache := readCacheFile(t, dir); len(cache) != 1 {
		t.Fatalf("outside-dir register must not enter the cache: %+v", cache)
	}
}

// TestRescanCorruptCache: a truncated/garbage sidecar degrades to a full
// re-hash, never a failure.
func TestRescanCorruptCache(t *testing.T) {
	dir := t.TempDir()
	body := fakeImage('a', false)
	writeImageFile(t, dir, "r1.bin", body)
	if err := os.WriteFile(filepath.Join(dir, imageCacheName), []byte("{corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := newScanServer(t, dir)
	s.rescanImages()
	wantSum := sha256.Sum256(body)
	wantSha := hex.EncodeToString(wantSum[:])
	if got := listImages(t, s)["r1.bin"]; got.SHA256 != wantSha {
		t.Fatalf("corrupt cache must fall back to hashing: %+v", got)
	}
	if cache := readCacheFile(t, dir); cache["r1.bin"].SHA256 != wantSha {
		t.Fatalf("cache must be rewritten after corruption: %+v", cache)
	}
}
