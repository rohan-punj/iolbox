package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// foldersync.go — persists user data on the Windows filesystem across the
// now-ephemeral OS disk (see qemu.go: snapshot=on). Two folders next to the
// launcher exe:
//
//	images\   user drops .bin/.iol IOL image files here; each run they are
//	          uploaded + registered into the guest's image registry.
//	labs\     lab documents as <labId>.json; synced in at start and out
//	          periodically + at shutdown, so labs created/edited in the GUI
//	          land back on the Windows FS.
//
// All guest interaction goes through the supervisor's EXISTING APIs (HTTP
// upload + the /control WS verb channel) — no guest changes.

// controlClient is the set of /control verbs the sync engine needs. Backed by
// controlWSClient in production; stubbed in tests.
type controlClient interface {
	registerImage(guestPath string) error                // image.register
	saveLab(doc json.RawMessage) (id string, err error)   // lab.saveDoc
	listLabIDs() ([]string, error)                        // lab.listDocs -> each doc's "id"
	getLab(id string) (json.RawMessage, error)            // lab.getDoc
}

// imageUploader performs the plain-HTTP image upload.
type imageUploader interface {
	upload(filename string, body io.Reader) (guestPath string, err error) // POST /api/upload/image
}

// folderSync coordinates the images\/labs\ <-> guest sync for one launch.
type folderSync struct {
	imagesDir string
	labsDir   string

	client   controlClient
	uploader imageUploader

	seedMu  sync.Mutex
	seedIDs map[string]bool // guest's built-in starter lab ids, recorded once at connect

	writeMu sync.Mutex // guards labs\ file writes (syncLabsOut vs. concurrent calls)
}

func newFolderSync(imagesDir, labsDir string, client controlClient, uploader imageUploader) *folderSync {
	return &folderSync{
		imagesDir: imagesDir,
		labsDir:   labsDir,
		client:    client,
		uploader:  uploader,
		seedIDs:   make(map[string]bool),
	}
}

// ensureDirs creates the images/labs directories if missing and logs their
// absolute paths so the user knows where to drop files.
func ensureDirs(imagesDir, labsDir string) error {
	for _, d := range []string{imagesDir, labsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("cannot create %s: %w", d, err)
		}
	}
	logf("  images folder: %s  (drop .bin/.iol IOL images here)", imagesDir)
	logf("  labs folder:   %s  (saved labs persist here as <id>.json)", labsDir)
	return nil
}

// recordSeedLabIDs must be called immediately after connect, BEFORE pushing
// any labs in. It captures the guest's built-in starter labs (re-seeded every
// boot since the OS disk is ephemeral) so they are never written back into
// labs\ and never clobbered by a same-id user file.
func (fs *folderSync) recordSeedLabIDs() error {
	ids, err := fs.client.listLabIDs()
	if err != nil {
		return fmt.Errorf("recordSeedLabIDs: listLabIDs: %w", err)
	}
	fs.seedMu.Lock()
	defer fs.seedMu.Unlock()
	for _, id := range ids {
		fs.seedIDs[id] = true
	}
	return nil
}

func (fs *folderSync) isSeedID(id string) bool {
	fs.seedMu.Lock()
	defer fs.seedMu.Unlock()
	return fs.seedIDs[id]
}

// isImageFile reports whether name has a .bin/.iol extension (case-insensitive).
func isImageFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".bin" || ext == ".iol"
}

// syncImagesIn uploads + registers every *.bin/*.iol file in imagesDir.
// Continues past per-file errors, logging one line per image.
func (fs *folderSync) syncImagesIn() (count int, err error) {
	entries, err := os.ReadDir(fs.imagesDir)
	if err != nil {
		return 0, fmt.Errorf("syncImagesIn: read %s: %w", fs.imagesDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !isImageFile(e.Name()) {
			continue
		}
		name := e.Name()
		full := filepath.Join(fs.imagesDir, name)
		f, openErr := os.Open(full)
		if openErr != nil {
			logf("  image %s: FAILED to open: %v", name, openErr)
			continue
		}
		guestPath, upErr := fs.uploader.upload(name, f)
		f.Close()
		if upErr != nil {
			logf("  image %s: upload FAILED: %v", name, upErr)
			continue
		}
		if regErr := fs.client.registerImage(guestPath); regErr != nil {
			logf("  image %s: registered upload but image.register FAILED: %v", name, regErr)
			continue
		}
		logf("  image %s: uploaded + registered (%s)", name, guestPath)
		count++
	}
	return count, nil
}

// syncLabsIn reads every *.json in labsDir, validates it is a JSON object
// with an "id" field, and calls saveLab. Malformed files are skipped with a
// warning; sync continues.
func (fs *folderSync) syncLabsIn() (count int, err error) {
	entries, err := os.ReadDir(fs.labsDir)
	if err != nil {
		return 0, fmt.Errorf("syncLabsIn: read %s: %w", fs.labsDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		name := e.Name()
		full := filepath.Join(fs.labsDir, name)
		raw, readErr := os.ReadFile(full)
		if readErr != nil {
			logf("  lab %s: WARNING: cannot read: %v", name, readErr)
			continue
		}
		id, ok := labDocID(raw)
		if !ok {
			logf("  lab %s: WARNING: not a valid lab document (expected a JSON object with an \"id\" string field) — skipped", name)
			continue
		}
		if _, saveErr := fs.client.saveLab(json.RawMessage(raw)); saveErr != nil {
			logf("  lab %s (id=%s): saveLab FAILED: %v", name, id, saveErr)
			continue
		}
		logf("  lab %s: synced in (id=%s)", name, id)
		count++
	}
	return count, nil
}

// labDocID validates raw is a JSON object with a non-empty "id" string field
// and returns it.
func labDocID(raw []byte) (string, bool) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", false
	}
	idRaw, ok := doc["id"]
	if !ok {
		return "", false
	}
	var id string
	if err := json.Unmarshal(idRaw, &id); err != nil || id == "" {
		return "", false
	}
	return id, true
}

// syncLabsOut lists the guest's current labs and writes every non-seed one to
// labs\<id>.json as pretty JSON. If a labs\ file with a seed id already
// exists on disk, it is never clobbered (warning logged instead — a rare id
// collision). Safe to call concurrently with itself (guarded by writeMu); not
// safe to call concurrently with syncLabsIn against the SAME files in a
// pathological race, but the launcher only calls syncLabsIn once at startup
// before the periodic/final syncLabsOut begins.
func (fs *folderSync) syncLabsOut() (count int, err error) {
	ids, err := fs.client.listLabIDs()
	if err != nil {
		return 0, fmt.Errorf("syncLabsOut: listLabIDs: %w", err)
	}

	fs.writeMu.Lock()
	defer fs.writeMu.Unlock()

	for _, id := range ids {
		if fs.isSeedID(id) {
			continue
		}
		if !isPlainLabID(id) {
			logf("  lab id %q: WARNING: unsafe id for a filename — skipped", id)
			continue
		}
		doc, getErr := fs.client.getLab(id)
		if getErr != nil {
			logf("  lab %s: getLab FAILED: %v", id, getErr)
			continue
		}
		target := filepath.Join(fs.labsDir, id+".json")

		docID, ok := labDocID(doc)
		if ok && fs.isSeedID(docID) {
			logf("  lab %s: WARNING: id collides with a seed lab id — not overwriting", id)
			continue
		}

		pretty, indentErr := prettyJSON(doc)
		if indentErr != nil {
			logf("  lab %s: WARNING: could not pretty-print: %v", id, indentErr)
			pretty = doc
		}
		if writeErr := writeFileAtomic(target, pretty); writeErr != nil {
			logf("  lab %s: write FAILED (%s): %v", id, target, writeErr)
			continue
		}
		count++
	}
	return count, nil
}

// isPlainLabID guards against path traversal / odd characters ending up in a
// filename derived from a guest-supplied lab id.
func isPlainLabID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func prettyJSON(raw json.RawMessage) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeFileAtomic writes via a temp file + rename so a crash mid-write never
// leaves a half-written labs\<id>.json.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---- Real implementations against the WS client + net/http -------------

// wsControlClient adapts controlWSClient to the controlClient interface used
// by folderSync.
type wsControlClient struct {
	ws *controlWSClient
}

func (c *wsControlClient) registerImage(guestPath string) error {
	_, err := c.ws.request("image.register", map[string]string{"path": guestPath})
	return err
}

func (c *wsControlClient) saveLab(doc json.RawMessage) (string, error) {
	res, err := c.ws.request("lab.saveDoc", map[string]json.RawMessage{"lab": doc})
	if err != nil {
		return "", err
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return "", fmt.Errorf("lab.saveDoc: unexpected result shape: %w", err)
	}
	return out.ID, nil
}

func (c *wsControlClient) listLabIDs() ([]string, error) {
	res, err := c.ws.request("lab.listDocs", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Labs []json.RawMessage `json:"labs"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, fmt.Errorf("lab.listDocs: unexpected result shape: %w", err)
	}
	ids := make([]string, 0, len(out.Labs))
	for _, doc := range out.Labs {
		if id, ok := labDocID(doc); ok {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (c *wsControlClient) getLab(id string) (json.RawMessage, error) {
	res, err := c.ws.request("lab.getDoc", map[string]string{"labId": id})
	if err != nil {
		return nil, err
	}
	var out struct {
		Lab json.RawMessage `json:"lab"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, fmt.Errorf("lab.getDoc: unexpected result shape: %w", err)
	}
	return out.Lab, nil
}

// httpImageUploader implements imageUploader against
// POST http://127.0.0.1:<gui>/api/upload/image?filename=<basename>.
type httpImageUploader struct {
	baseURL string // e.g. "http://127.0.0.1:4001"
	client  *http.Client
}

func newHTTPImageUploader(baseURL string) *httpImageUploader {
	return &httpImageUploader{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (u *httpImageUploader) upload(filename string, body io.Reader) (string, error) {
	url := fmt.Sprintf("%s/api/upload/image?filename=%s", u.baseURL, filename)
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := u.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errOut struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &errOut) == nil && errOut.Error != "" {
			return "", fmt.Errorf("upload %s: HTTP %d: %s", filename, resp.StatusCode, errOut.Error)
		}
		return "", fmt.Errorf("upload %s: HTTP %d: %s", filename, resp.StatusCode, string(respBody))
	}

	var out struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", fmt.Errorf("upload %s: unexpected response shape: %w", filename, err)
	}
	return out.Path, nil
}

// ---- Lifecycle wiring shared by both backends ---------------------------

// syncSession owns the WS control connection + folderSync for one launch and
// runs the periodic syncLabsOut loop. Both qemuBackend and wslBackend can
// drive the same helper since they expose the same GUI port and supervisor
// APIs.
type syncSession struct {
	fs   *folderSync
	conn *controlWSClient
}

// defaultSyncDirs resolves the default <exeDir>\images and <exeDir>\labs
// paths, honoring --images-dir/--labs-dir overrides.
func defaultSyncDirs(exeDir, imagesOverride, labsOverride string) (imagesDir, labsDir string) {
	imagesDir = imagesOverride
	if imagesDir == "" {
		imagesDir = filepath.Join(exeDir, "images")
	}
	labsDir = labsOverride
	if labsDir == "" {
		labsDir = filepath.Join(exeDir, "labs")
	}
	return imagesDir, labsDir
}

// startSyncSession connects to ws://127.0.0.1:<guiPort>/control (retrying for
// a few seconds since the GUI just came up), then runs ensureDirs,
// recordSeedLabIDs, syncImagesIn, and syncLabsIn in sequence. On connect
// failure after retries, it logs a warning and returns (nil, nil) — the
// caller should treat that as "sync disabled for this run" and NOT fail the
// launch (the GUI still works without folder sync).
func startSyncSession(ctx context.Context, guiPort int, imagesDir, labsDir string) (*syncSession, error) {
	if err := ensureDirs(imagesDir, labsDir); err != nil {
		return nil, err
	}

	addr := fmt.Sprintf("127.0.0.1:%d", guiPort)
	baseURL := fmt.Sprintf("http://%s", addr)

	var conn *controlWSClient
	var lastErr error
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, lastErr = dialControlWS(addr)
		if lastErr == nil {
			break
		}
		if time.Now().After(deadline) {
			logf("WARNING: could not connect to the control channel for folder sync (%v) — continuing without sync.", lastErr)
			return nil, nil
		}
		select {
		case <-ctx.Done():
			return nil, nil
		case <-time.After(1 * time.Second):
		}
	}

	client := &wsControlClient{ws: conn}
	uploader := newHTTPImageUploader(baseURL)
	fs := newFolderSync(imagesDir, labsDir, client, uploader)

	if err := fs.recordSeedLabIDs(); err != nil {
		logf("WARNING: could not record seed lab ids (%v) — continuing without sync.", err)
		conn.Close()
		return nil, nil
	}

	imgCount, err := fs.syncImagesIn()
	if err != nil {
		logf("WARNING: syncImagesIn failed: %v", err)
	}
	labCount, err := fs.syncLabsIn()
	if err != nil {
		logf("WARNING: syncLabsIn failed: %v", err)
	}
	logf("Folder sync: synced %d image(s), %d lab(s) in from %s / %s", imgCount, labCount, imagesDir, labsDir)

	return &syncSession{fs: fs, conn: conn}, nil
}

// runPeriodicSyncOut runs syncLabsOut every interval until ctx is cancelled or
// stop is closed (e.g. the guest process exited on its own). Meant to be run
// in its own goroutine.
func (s *syncSession) runPeriodicSyncOut(ctx context.Context, interval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			if _, err := s.fs.syncLabsOut(); err != nil {
				logf("WARNING: periodic syncLabsOut failed: %v", err)
			}
		}
	}
}

// finalSyncOut runs one last syncLabsOut (called on the shutdown path, while
// the guest is still alive) and logs the result.
func (s *syncSession) finalSyncOut() {
	count, err := s.fs.syncLabsOut()
	if err != nil {
		logf("WARNING: final syncLabsOut failed: %v", err)
		return
	}
	logf("Folder sync: synced %d lab(s) out to %s", count, s.fs.labsDir)
}

// close closes the underlying WS connection.
func (s *syncSession) close() {
	if s.conn != nil {
		s.conn.Close()
	}
}
