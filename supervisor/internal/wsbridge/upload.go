package wsbridge

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// maxUploadBytes caps an accepted image upload at 4 GiB. IOL images are tens of
// MB and CML/qcow disks a few GB; anything larger is a client bug or abuse, and
// the cap bounds the .partial file a malicious client could grow on the VM.
const maxUploadBytes = 4 << 30

// safeImageName matches a conservative filename: letters, digits, dot, dash,
// underscore only. Anything else (spaces, path separators, shell metacharacters)
// is rejected outright rather than escaped, so the name is safe to join onto
// ImageDir. Path components are already stripped before this check.
var safeImageName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// handleUploadImage streams a raw .bin/.iol image body to <ImageDir>/<filename>,
// writing to a <filename>.partial temp first and renaming on success so a failed
// or aborted transfer never leaves a truncated file registered. It does NOT
// register the image: the GUI calls image.register over WS with the returned
// path afterwards.
//
//	POST /api/upload/image?filename=<basename>
//	Content-Type: application/octet-stream, body = raw file bytes.
//	200 {"path":"/abs/path"} on success; 4xx/5xx {"error":"..."} otherwise.
func (b *Bridge) handleUploadImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeUploadError(w, http.StatusMethodNotAllowed, "method not allowed (use POST)")
		return
	}
	if b.cfg.ImageDir == "" {
		writeUploadError(w, http.StatusServiceUnavailable, "image uploads disabled (no image dir configured)")
		return
	}

	name, ok := sanitizeImageName(r.URL.Query().Get("filename"))
	if !ok {
		writeUploadError(w, http.StatusBadRequest, "invalid filename (want a plain <name>.bin or <name>.iol basename)")
		return
	}

	if err := os.MkdirAll(b.cfg.ImageDir, 0o755); err != nil {
		writeUploadError(w, http.StatusInternalServerError, "create image dir: "+err.Error())
		return
	}

	finalPath := filepath.Join(b.cfg.ImageDir, name)
	partialPath := finalPath + ".partial"

	f, err := os.OpenFile(partialPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		writeUploadError(w, http.StatusInternalServerError, "create upload file: "+err.Error())
		return
	}

	// Cap the accepted body; MaxBytesReader surfaces overrun as a read error so
	// the copy below fails and the .partial is cleaned up (no oversized file left).
	body := http.MaxBytesReader(w, r.Body, maxUploadBytes)
	_, copyErr := io.Copy(f, body)
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(partialPath)
		msg := "upload failed"
		if copyErr != nil {
			msg = "upload body: " + copyErr.Error()
		} else {
			msg = "flush upload: " + closeErr.Error()
		}
		writeUploadError(w, http.StatusBadRequest, msg)
		return
	}

	if err := os.Rename(partialPath, finalPath); err != nil {
		_ = os.Remove(partialPath)
		writeUploadError(w, http.StatusInternalServerError, "finalize upload: "+err.Error())
		return
	}

	writeUploadJSON(w, http.StatusOK, map[string]string{"path": finalPath})
}

// sanitizeImageName strips any path components (both / and \ separators, so a
// Windows-style client path can't smuggle a directory in) and validates the
// remaining basename against safeImageName plus a .bin/.iol extension check. It
// returns the clean name and ok=false on any rejection.
func sanitizeImageName(raw string) (string, bool) {
	// path.Base handles '/'; also collapse '\' first so "..\\evil.bin" or an
	// absolute Windows path reduces to its final element regardless of separator.
	raw = strings.ReplaceAll(raw, "\\", "/")
	name := path.Base(raw)
	if name == "" || name == "." || name == ".." {
		return "", false
	}
	if !safeImageName.MatchString(name) {
		return "", false
	}
	lower := strings.ToLower(name)
	// Require a non-empty stem before the extension, so a bare ".bin"/".iol"
	// (a degenerate dotfile) is rejected, not silently accepted.
	if !(len(lower) > len(".bin") && strings.HasSuffix(lower, ".bin")) &&
		!(len(lower) > len(".iol") && strings.HasSuffix(lower, ".iol")) {
		return "", false
	}
	return name, true
}

// writeUploadError writes a JSON {"error":...} body with the given status.
func writeUploadError(w http.ResponseWriter, status int, msg string) {
	writeUploadJSON(w, status, map[string]string{"error": msg})
}

// writeUploadJSON writes v as a JSON body with the given status.
func writeUploadJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
