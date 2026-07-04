package server

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/rohanpunj/iolbox/supervisor/internal/image"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
)

// The image registry (s.images) is in-memory only, so a supervisor restart or
// runtime reboot used to forget every registration even though the files
// survive in ImageDir. rescanImages derives the registry from the directory at
// startup instead: every *.bin/*.iol file is fingerprinted (sha256 + arch +
// L2/L3 class, same image.Inspect path image.register uses) and registered.
//
// Hashing a 300 MB IOL image on every boot would be wasteful, so fingerprints
// are cached in a sidecar JSON (imageCacheName) keyed by filename and
// validated by (size, mtime). image.register keeps the sidecar warm for files
// it inspects inside ImageDir, so even a freshly uploaded image is a cache hit
// on the next boot. The cache is rewritten from scratch on every rescan, which
// also prunes entries for deleted files.

// imageCacheName is the sidecar cache file inside ImageDir. Dot-prefixed and
// .json so it can never collide with the *.bin/*.iol scan set.
const imageCacheName = ".image-cache.json"

// imageCacheEntry is one cached fingerprint. Size+MTimeNs validate the entry
// against the current file; SHA256/Arch/Class reconstruct image.Info without
// re-reading the file.
type imageCacheEntry struct {
	Size    int64  `json:"size"`
	MTimeNs int64  `json:"mtimeNs"`
	SHA256  string `json:"sha256"`
	Arch    string `json:"arch"`
	Class   string `json:"class"`
}

// rescanImages registers every image file found in ImageDir, using the sidecar
// fingerprint cache to skip re-hashing unchanged files. Best-effort: a file
// that fails inspection is logged and skipped, and cache trouble only costs a
// re-hash. Called once from ListenAndServe before the listeners accept, so a
// reconnecting GUI always sees the full registry.
func (s *Server) rescanImages() {
	dir := s.cfg.ImageDir
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("image rescan: read %s: %v", dir, err)
		}
		return
	}

	cache := s.readImageCache()
	fresh := make(map[string]imageCacheEntry, len(entries))
	registered, hashed := 0, 0
	for _, e := range entries {
		if e.IsDir() || !isImageFilename(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(dir, e.Name())

		var info *image.Info
		if ent, ok := cache[e.Name()]; ok && ent.Size == fi.Size() && ent.MTimeNs == fi.ModTime().UnixNano() && len(ent.SHA256) == 64 {
			info = &image.Info{
				ID:       ent.SHA256[:16],
				Filename: e.Name(),
				SHA256:   ent.SHA256,
				Size:     ent.Size,
				Arch:     image.Arch(ent.Arch),
				Class:    image.Class(ent.Class),
			}
		} else {
			info, err = image.Inspect(path)
			if err != nil {
				log.Printf("image rescan: inspect %s: %v", path, err)
				continue
			}
			hashed++
		}

		fresh[e.Name()] = imageCacheEntry{
			Size:    fi.Size(),
			MTimeNs: fi.ModTime().UnixNano(),
			SHA256:  info.SHA256,
			Arch:    string(info.Arch),
			Class:   string(info.Class),
		}
		s.mu.Lock()
		s.images[info.ID] = *info
		s.mu.Unlock()
		registered++
	}

	s.writeImageCache(fresh)
	if registered > 0 || len(cache) > 0 {
		log.Printf("image rescan: registered %d image(s) from %s (%d hashed, %d from cache)", registered, dir, hashed, registered-hashed)
	}
}

// cacheRegisteredImage seeds the sidecar cache for an image.register that
// inspected a file inside ImageDir, so the next boot's rescan re-registers it
// without a re-hash. Registrations of paths outside ImageDir are ignored: the
// rescan (and lab.start's image-path resolution) only ever looks in ImageDir.
func (s *Server) cacheRegisteredImage(path string, info *image.Info) {
	dir := s.cfg.ImageDir
	if dir == "" {
		return
	}
	pDir, err1 := filepath.Abs(filepath.Dir(path))
	iDir, err2 := filepath.Abs(dir)
	if err1 != nil || err2 != nil || pDir != iDir {
		return
	}
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	cache := s.readImageCacheLocked()
	cache[info.Filename] = imageCacheEntry{
		Size:    fi.Size(),
		MTimeNs: fi.ModTime().UnixNano(),
		SHA256:  info.SHA256,
		Arch:    string(info.Arch),
		Class:   string(info.Class),
	}
	s.writeImageCacheLocked(cache)
}

// inspectOrTrustHint resolves the image.Info for an image.register call,
// skipping the sha256 hash when the request carries a trustworthy hint (see
// protocol.ImageRegisterArgs). The hint is trusted ONLY when a cheap os.Stat
// of args.Path shows the file's current size and mtime still match
// HintSize/HintMTimeNs exactly — the same (size, mtime) validation
// rescanImages uses for its own sidecar cache. Any mismatch, missing hint, or
// stat error falls back to a full image.Inspect, so a stale or wrong hint can
// never bind an incorrect fingerprint to a changed file.
func inspectOrTrustHint(args protocol.ImageRegisterArgs) (*image.Info, error) {
	if args.HintSize > 0 && args.HintMTimeNs > 0 && len(args.HintSHA256) == 64 {
		if fi, err := os.Stat(args.Path); err == nil &&
			fi.Size() == args.HintSize && fi.ModTime().UnixNano() == args.HintMTimeNs {
			return &image.Info{
				ID:       args.HintSHA256[:16],
				Filename: filepath.Base(args.Path),
				SHA256:   args.HintSHA256,
				Size:     args.HintSize,
				Arch:     image.Arch(args.HintArch),
				Class:    image.Class(args.HintClass),
			}, nil
		}
	}
	return image.Inspect(args.Path)
}

// isImageFilename mirrors the upload sanitizer's extension rule (see
// wsbridge.sanitizeImageName): a non-empty stem plus a .bin or .iol suffix.
// This keeps .partial upload temps, the sidecar cache, and stray files out of
// the scan set.
func isImageFilename(name string) bool {
	lower := strings.ToLower(name)
	return (len(lower) > len(".bin") && strings.HasSuffix(lower, ".bin")) ||
		(len(lower) > len(".iol") && strings.HasSuffix(lower, ".iol"))
}

func (s *Server) readImageCache() map[string]imageCacheEntry {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	return s.readImageCacheLocked()
}

func (s *Server) writeImageCache(cache map[string]imageCacheEntry) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.writeImageCacheLocked(cache)
}

// readImageCacheLocked loads the sidecar cache; any error (missing file,
// corrupt JSON) yields an empty map — the penalty is only a re-hash.
func (s *Server) readImageCacheLocked() map[string]imageCacheEntry {
	cache := map[string]imageCacheEntry{}
	data, err := os.ReadFile(filepath.Join(s.cfg.ImageDir, imageCacheName))
	if err != nil {
		return cache
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		return map[string]imageCacheEntry{}
	}
	return cache
}

// writeImageCacheLocked persists the sidecar cache via temp-file + rename so a
// crash mid-write never leaves corrupt JSON (a corrupt cache would silently
// force a full re-hash on the next boot). Best-effort: failures are logged.
func (s *Server) writeImageCacheLocked(cache map[string]imageCacheEntry) {
	final := filepath.Join(s.cfg.ImageDir, imageCacheName)
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		log.Printf("image cache: marshal: %v", err)
		return
	}
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		log.Printf("image cache: write %s: %v", tmp, err)
		return
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		log.Printf("image cache: rename %s: %v", final, err)
	}
}
