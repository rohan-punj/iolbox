package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// console.go — the local "iolbox console" HTTP server (127.0.0.1:<console
// port>, default 4002). It owns backend lifecycle (start/stop) via a
// lifecycleController and serves a single embedded HTML page plus a small
// JSON API under /api/. Every handler is 127.0.0.1-only by virtue of the
// listener address (never 0.0.0.0) — same posture as the no-auth GUI on
// :4001 (see main.go's printDetection note).

//go:embed console_index.html
var consoleIndexHTML []byte

// consoleDeps bundles everything a console handler needs, all of it either
// pure config or an interface so tests can substitute fakes. exeDir is where
// launcher.json / images\ / labs\ live (mirrors the exe-relative layout used
// throughout qemu.go/foldersync.go).
type consoleDeps struct {
	exeDir string
	ranges portRanges

	lc *lifecycleController

	// guiProbe reports whether the guest GUI answers on :guiPort right now. A
	// tiny wrapper around waitForGUI's underlying single-shot HTTP GET (see
	// probeGUIOnce) so tests can fake it without a real qemu/guest.
	guiProbe func(guiPort int) bool

	// guestVersion best-effort dials the guest's /control WS and issues
	// hello, returning ("", false) if unreachable. Real implementation in
	// console_guest.go; faked in tests.
	guestVersion func(guiPort int) (version string, ok bool)

	// qemuImgPath returns the path to a bundled qemu-img.exe next to the
	// qemu binary, or "" if not found (disk sizing is then omitted).
	qemuImgPath func() string
	diskPath    func() string // resolves the qcow2 path the same way qemuBackend.locate() does
}

// consoleServer wires consoleDeps into an *http.Server with its mux.
type consoleServer struct {
	deps consoleDeps
	mux  *http.ServeMux
}

func newConsoleServer(deps consoleDeps) *consoleServer {
	cs := &consoleServer{deps: deps, mux: http.NewServeMux()}
	cs.mux.HandleFunc("/", cs.handleIndex)
	cs.mux.HandleFunc("/api/status", cs.handleStatus)
	cs.mux.HandleFunc("/api/start", cs.handleStart)
	cs.mux.HandleFunc("/api/stop", cs.handleStop)
	cs.mux.HandleFunc("/api/config", cs.handleConfig)
	cs.mux.HandleFunc("/api/images", cs.handleImages)
	return cs
}

func (cs *consoleServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cs.mux.ServeHTTP(w, r)
}

// handleIndex serves the single embedded page for "/" only (a stray 404 for
// any other unmatched path keeps this from swallowing typos as the SPA).
func (cs *consoleServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(consoleIndexHTML)
}

// writeJSON is the shared response helper: sets the content type, the status
// code, and encodes v — every /api/ handler funnels through this so the
// shape is consistent.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ---- GET /api/status ----------------------------------------------------

type statusQEMU struct {
	Running bool `json:"running"`
}

type statusGUI struct {
	Reachable bool `json:"reachable"`
}

type statusGuest struct {
	Version string `json:"version,omitempty"`
}

type statusDisk struct {
	Path        string `json:"path,omitempty"`
	VirtualSize int64  `json:"virtualSize,omitempty"`
	ActualSize  int64  `json:"actualSize,omitempty"`
}

type statusResponse struct {
	State string      `json:"state"` // stopped|starting|running|stopping
	Error string      `json:"error,omitempty"`
	QEMU  statusQEMU  `json:"qemu"`
	GUI   statusGUI   `json:"gui"`
	Guest statusGuest `json:"guest"`
	Disk  statusDisk  `json:"disk"`
}

// handleStatus assembles the full status snapshot. Every probe here is
// deliberately best-effort and non-blocking-ish (short timeouts): a slow or
// down guest must never hang the console page.
func (cs *consoleServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}

	state, runErr := cs.deps.lc.Status()
	resp := statusResponse{State: string(state)}
	if runErr != nil {
		resp.Error = runErr.Error()
	}
	resp.QEMU.Running = state == stateRunning || state == stateStarting || state == stateStopping

	if cs.deps.guiProbe != nil {
		resp.GUI.Reachable = cs.deps.guiProbe(cs.deps.ranges.guiPort)
	}
	if resp.GUI.Reachable && cs.deps.guestVersion != nil {
		if v, ok := cs.deps.guestVersion(cs.deps.ranges.guiPort); ok {
			resp.Guest.Version = v
		}
	}

	if cs.deps.diskPath != nil {
		if dp := cs.deps.diskPath(); dp != "" {
			resp.Disk.Path = dp
			if cs.deps.qemuImgPath != nil {
				if img := cs.deps.qemuImgPath(); img != "" {
					if vsize, asize, err := qemuImgInfo(img, dp); err == nil {
						resp.Disk.VirtualSize = vsize
						resp.Disk.ActualSize = asize
					}
					// A failed qemu-img probe just omits sizes — never fails
					// the whole status response over a display-only field.
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// probeGUIOnce is the real (non-test) guiProbe: a single short-timeout HTTP
// GET, exactly like one iteration of waitForGUI's polling loop but without
// the loop — status wants an instant answer, not a wait.
func probeGUIOnce(host string, port int) bool {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://%s:%d/", host, port))
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode < 500
}

// fetchGuestVersion dials the guest's /control WS and issues hello,
// best-effort. Mirrors wsControlClient usage in foldersync.go but as a single
// short-lived connection (status polling, not a long-lived session).
func fetchGuestVersion(guiPort int) (string, bool) {
	addr := fmt.Sprintf("127.0.0.1:%d", guiPort)
	conn, err := dialControlWS(addr)
	if err != nil {
		return "", false
	}
	defer conn.Close()
	raw, err := conn.requestTimeout("hello", map[string]string{"client": "iolbox-console/1.0"}, 2*time.Second)
	if err != nil {
		return "", false
	}
	var out struct {
		Supervisor string `json:"supervisor"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Supervisor == "" {
		return "", false
	}
	return out.Supervisor, true
}

// qemuImgInfo shells `qemu-img info --output=json <disk>` and extracts the
// virtual-size/actual-size fields. Kept minimal — v1 is display-only per the
// kickoff brief, no resize support.
func qemuImgInfo(qemuImg, disk string) (virtualSize, actualSize int64, err error) {
	out, err := exec.Command(qemuImg, "info", "--output=json", disk).Output()
	if err != nil {
		return 0, 0, err
	}
	var info struct {
		VirtualSize int64 `json:"virtual-size"`
		ActualSize  int64 `json:"actual-size"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return 0, 0, err
	}
	return info.VirtualSize, info.ActualSize, nil
}

// defaultQemuImgPath locates qemu-img.exe next to qemu-system-x86_64.exe,
// using the same exeDir-relative layout as qemuBackend.locate(). Returns ""
// if not found — the bundle does not currently ship qemu-img, so this is
// expected to miss until it's added; status/disk sizing degrades gracefully.
func defaultQemuImgPath(exeDir string) string {
	p := filepath.Join(exeDir, "qemu", "qemu-img.exe")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// defaultDiskPath locates iolbox-disk.qcow2 next to the exe, same default
// qemuBackend.locate() uses (no --disk override plumbed through here since
// the console reads the currently-configured disk, not a dev override).
func defaultDiskPath(exeDir string) string {
	p := filepath.Join(exeDir, "iolbox-disk.qcow2")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// ---- POST /api/start / /api/stop -----------------------------------------

func (cs *consoleServer) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	if err := cs.deps.lc.Start(); err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"state": "starting"})
}

func (cs *consoleServer) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	cs.deps.lc.StopAsync()
	writeJSON(w, http.StatusAccepted, map[string]string{"state": "stopping"})
}

// ---- GET/PUT /api/config --------------------------------------------------

func (cs *consoleServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := loadLauncherConfig(cs.deps.exeDir)
		writeJSON(w, http.StatusOK, cfg)
	case http.MethodPut:
		var cfg launcherConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}
		if cfg.CPUs <= 0 {
			cfg.CPUs = defaultLauncherConfig().CPUs
		}
		if cfg.RAMMB <= 0 {
			cfg.RAMMB = defaultLauncherConfig().RAMMB
		}
		cfg.Deployment = normalizeDeployment(cfg.Deployment)
		if err := saveLauncherConfig(cs.deps.exeDir, cfg); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "could not save config: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "GET or PUT only")
	}
}

// ---- /api/images ----------------------------------------------------------

type imageInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// handleImages dispatches by method: GET lists, POST uploads (multipart),
// DELETE removes by ?name=. All operate on <exeDir>\images — the SAME
// directory foldersync.go syncs from at boot, so a file dropped here shows up
// on the next launch even without the guest currently running.
func (cs *consoleServer) handleImages(w http.ResponseWriter, r *http.Request) {
	imagesDir := filepath.Join(cs.deps.exeDir, "images")
	switch r.Method {
	case http.MethodGet:
		cs.listImages(w, imagesDir)
	case http.MethodPost:
		cs.uploadImage(w, r, imagesDir)
	case http.MethodDelete:
		cs.deleteImage(w, r, imagesDir)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "GET, POST, or DELETE only")
	}
}

func (cs *consoleServer) listImages(w http.ResponseWriter, imagesDir string) {
	entries, err := os.ReadDir(imagesDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, []imageInfo{})
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "cannot list images: "+err.Error())
		return
	}
	out := make([]imageInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !isImageFile(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, imageInfo{Name: e.Name(), Size: fi.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

// maxImageUploadMemory bounds the in-memory part of multipart parsing;
// large image bodies spill to a temp file automatically past this (standard
// mime/multipart behavior), so this is just a modest buffer, not a size cap.
const maxImageUploadMemory = 32 << 20 // 32 MiB

// uploadImage saves the uploaded file into imagesDir and, if the guest is
// currently reachable, ALSO live-pushes it via the same upload+image.register
// path foldersync.go uses at boot — so a mid-session upload shows up in the
// running GUI immediately instead of only on the next launch.
func (cs *consoleServer) uploadImage(w http.ResponseWriter, r *http.Request, imagesDir string) {
	if err := r.ParseMultipartForm(maxImageUploadMemory); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid multipart form: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "missing multipart field \"file\": "+err.Error())
		return
	}
	defer file.Close()

	name := filepath.Base(header.Filename)
	if name == "" || name == "." || name == string(filepath.Separator) {
		writeJSONError(w, http.StatusBadRequest, "invalid filename")
		return
	}
	if !isImageFile(name) {
		writeJSONError(w, http.StatusBadRequest, "only .bin/.iol files are accepted")
		return
	}

	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "cannot create images dir: "+err.Error())
		return
	}
	dest := filepath.Join(imagesDir, name)
	out, err := os.Create(dest)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "cannot write image: "+err.Error())
		return
	}
	size, copyErr := io.Copy(out, file)
	closeErr := out.Close()
	if copyErr != nil {
		writeJSONError(w, http.StatusInternalServerError, "cannot write image: "+copyErr.Error())
		return
	}
	if closeErr != nil {
		writeJSONError(w, http.StatusInternalServerError, "cannot write image: "+closeErr.Error())
		return
	}

	pushed := false
	if cs.deps.guiProbe != nil && cs.deps.guiProbe(cs.deps.ranges.guiPort) {
		if err := livePushImage(cs.deps.ranges.guiPort, dest, name); err == nil {
			pushed = true
		}
		// A live-push failure is non-fatal: the file is safely on disk and
		// will sync in on the next launch regardless.
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"name":       name,
		"size":       size,
		"livePushed": pushed,
	})
}

// livePushImage uploads+registers a single image against an already-running
// guest, reusing the exact same interfaces foldersync.go uses at boot
// (httpImageUploader + wsControlClient.registerImage), just invoked once
// on-demand instead of for the whole images\ directory.
func livePushImage(guiPort int, path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", guiPort)
	uploader := newHTTPImageUploader(baseURL)
	guestPath, err := uploader.upload(name, f, fi.ModTime().UnixNano())
	if err != nil {
		return err
	}

	addr := fmt.Sprintf("127.0.0.1:%d", guiPort)
	conn, err := dialControlWS(addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	client := &wsControlClient{ws: conn}
	_, err = client.registerImage(guestPath, imageFingerprint{}, false)
	return err
}

func (cs *consoleServer) deleteImage(w http.ResponseWriter, r *http.Request, imagesDir string) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "missing ?name=")
		return
	}
	name = filepath.Base(name) // guard against path traversal
	if !isImageFile(name) {
		writeJSONError(w, http.StatusBadRequest, "not an image filename")
		return
	}
	target := filepath.Join(imagesDir, name)
	if err := os.Remove(target); err != nil {
		if os.IsNotExist(err) {
			writeJSONError(w, http.StatusNotFound, "no such image: "+name)
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "cannot delete image: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": name})
}

// ---- server lifecycle -----------------------------------------------------

// runConsoleServer starts the HTTP server on addr and blocks until ctx is
// cancelled, then shuts it down gracefully. addr is 127.0.0.1:<port> only —
// never exposed beyond localhost (same posture as the GUI's hostfwd).
func runConsoleServer(ctx context.Context, addr string, cs *consoleServer) error {
	srv := &http.Server{Addr: addr, Handler: cs}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
}

// parseConsolePort extracts the numeric port from an addr string like
// "127.0.0.1:4002", falling back to 0 (meaning "unknown") on a parse
// failure — used only for log messages, never for binding.
func parseConsolePort(addr string) int {
	_, portStr, err := splitHostPortSafe(addr)
	if err != nil {
		return 0
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return 0
	}
	return p
}

func splitHostPortSafe(addr string) (host, port string, err error) {
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return "", "", fmt.Errorf("no port in %q", addr)
	}
	return addr[:i], addr[i+1:], nil
}
