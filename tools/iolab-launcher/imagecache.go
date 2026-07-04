package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

// imagecache.go — a Windows-side sidecar that lets a re-uploaded-but-unchanged
// IOL image skip the guest's expensive sha256 re-hash on every launch.
//
// Why this lives on the Windows side, not the guest: the guest's ImageDir is
// wiped every boot (qemu.go: snapshot=on discards guest writes), so nothing
// persisted only in the guest survives across launches. The launcher's
// images\ folder is the one thing that IS stable across boots, so it's the
// natural place to remember "I already know this file's fingerprint."
//
// The flow:
//  1. Before uploading a file, look up its cached fingerprint by
//     (filename, size, mtime) of the SOURCE file in images\.
//  2. Upload it with that same source mtime attached (?mtime=<ns>), so the
//     file the guest ends up with has the identical (size, mtime) it had on
//     the previous boot.
//  3. Call image.register with hint* fields set to the cached fingerprint.
//     The supervisor only trusts the hint if ITS OWN stat of the guest file
//     still shows the matching (size, mtime) — see
//     supervisor/internal/server/imagescan.go:inspectOrTrustHint. A wrong or
//     stale hint is therefore harmless: the guest just re-hashes.
//  4. Whatever fingerprint the register call returns (hinted or freshly
//     hashed) is written back into the cache keyed by the SOURCE file's
//     current (size, mtime), so the next boot is a hit.
//
// This never trusts a hint blindly: correctness rests entirely on the
// server-side (size, mtime) re-validation, exactly like the existing
// rescanImages sidecar cache. A changed source file gets a different mtime
// or size and simply misses the cache, falling back to a real hash — never a
// stale binding.

// imageCacheFileName is the sidecar cache written into the launcher's
// images\ folder. Named distinctly from the guest-side ".image-cache.json"
// (imagescan.go) even though the two never coexist in the same directory, to
// avoid any confusion when reading logs or file listings.
const imageCacheFileName = ".iolbox-image-cache.json"

// imageFingerprint is one cached (filename, size, mtime) -> fingerprint
// entry, mirroring the guest-side imageCacheEntry shape closely enough to
// round-trip through image.register's hint fields.
type imageFingerprint struct {
	Size    int64  `json:"size"`
	MTimeNs int64  `json:"mtimeNs"`
	SHA256  string `json:"sha256"`
	Arch    string `json:"arch"`
	Class   string `json:"class"`
}

// imageFingerprintCache is the in-memory form of the sidecar, keyed by
// filename. Not safe for concurrent use — syncImagesIn processes files
// sequentially.
type imageFingerprintCache map[string]imageFingerprint

// loadImageFingerprintCache reads the sidecar from dir. Any error (missing
// file, corrupt JSON) yields an empty cache — the only penalty is a re-hash.
func loadImageFingerprintCache(dir string) imageFingerprintCache {
	cache := imageFingerprintCache{}
	data, err := os.ReadFile(filepath.Join(dir, imageCacheFileName))
	if err != nil {
		return cache
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		return imageFingerprintCache{}
	}
	return cache
}

// save persists the cache into dir via temp-file + rename, so a crash
// mid-write never leaves corrupt JSON behind.
func (c imageFingerprintCache) save(dir string) error {
	final := filepath.Join(dir, imageCacheFileName)
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// lookup returns the cached fingerprint for name if its (size, mtimeNs)
// still match, i.e. the source file has not changed since it was cached.
func (c imageFingerprintCache) lookup(name string, size, mtimeNs int64) (imageFingerprint, bool) {
	ent, ok := c[name]
	if !ok || ent.Size != size || ent.MTimeNs != mtimeNs || len(ent.SHA256) != 64 {
		return imageFingerprint{}, false
	}
	return ent, true
}

// put records/overwrites the fingerprint for name.
func (c imageFingerprintCache) put(name string, ent imageFingerprint) {
	c[name] = ent
}

// sha256File hashes a file's contents. Used only by tests to compute an
// expected fingerprint independently of production code paths.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
