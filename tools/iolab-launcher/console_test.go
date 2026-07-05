package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// newTestConsole builds a consoleServer wired to a temp exeDir and a
// lifecycleController driven by fakeRunnable, with the guest-facing probes
// stubbed so tests never touch a real qemu/guest. Returns the server and the
// controller (for driving state transitions from within a test).
func newTestConsole(t *testing.T) (*consoleServer, *lifecycleController, string) {
	t.Helper()
	dir := t.TempDir()

	var fr *fakeRunnable
	lc := newLifecycleController(func() (runnable, error) {
		fr = newFakeRunnable()
		return fr, nil
	})

	deps := consoleDeps{
		exeDir:       dir,
		ranges:       defaultPortRanges(),
		lc:           lc,
		guiProbe:     func(int) bool { return false },
		guestVersion: func(int) (string, bool) { return "", false },
		qemuImgPath:  func() string { return "" },
		diskPath:     func() string { return "" },
	}
	return newConsoleServer(deps), lc, dir
}

func decodeJSON(t *testing.T, body *bytes.Buffer, v any) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(v); err != nil {
		t.Fatalf("decode response body: %v (body=%q)", err, body.String())
	}
}

// ---- /api/status ----

func TestHandleStatus_MethodNotAllowed(t *testing.T) {
	cs, _, _ := newTestConsole(t)
	req := httptest.NewRequest(http.MethodPost, "/api/status", nil)
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestHandleStatus_StoppedByDefault(t *testing.T) {
	cs, _, _ := newTestConsole(t)
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp statusResponse
	decodeJSON(t, rec.Body, &resp)
	if resp.State != string(stateStopped) {
		t.Errorf("State = %q, want stopped", resp.State)
	}
	if resp.QEMU.Running {
		t.Error("QEMU.Running should be false when stopped")
	}
	if resp.GUI.Reachable {
		t.Error("GUI.Reachable should be false (guiProbe stubbed to false)")
	}
}

func TestHandleStatus_ReflectsRunningAndGuiReachable(t *testing.T) {
	dir := t.TempDir()
	var fr *fakeRunnable
	lc := newLifecycleController(func() (runnable, error) {
		fr = newFakeRunnable()
		return fr, nil
	})
	deps := consoleDeps{
		exeDir:       dir,
		ranges:       defaultPortRanges(),
		lc:           lc,
		guiProbe:     func(int) bool { return true },
		guestVersion: func(int) (string, bool) { return "1.2.3", true },
		qemuImgPath:  func() string { return "" },
		diskPath:     func() string { return "" },
	}
	cs := newConsoleServer(deps)

	if err := lc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-fr.startedCh
	waitForState(t, lc, stateRunning, time.Second)
	defer lc.Stop()

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, req)

	var resp statusResponse
	decodeJSON(t, rec.Body, &resp)
	if resp.State != string(stateRunning) {
		t.Errorf("State = %q, want running", resp.State)
	}
	if !resp.QEMU.Running {
		t.Error("QEMU.Running should be true")
	}
	if !resp.GUI.Reachable {
		t.Error("GUI.Reachable should be true")
	}
	if resp.Guest.Version != "1.2.3" {
		t.Errorf("Guest.Version = %q, want 1.2.3", resp.Guest.Version)
	}
}

func TestHandleStatus_SurfacesBackendError(t *testing.T) {
	dir := t.TempDir()
	wantErr := errors.New("qemu exploded")
	var fr *fakeRunnable
	lc := newLifecycleController(func() (runnable, error) {
		fr = newFakeRunnable()
		fr.retErr = wantErr
		return fr, nil
	})
	deps := consoleDeps{exeDir: dir, ranges: defaultPortRanges(), lc: lc,
		guiProbe: func(int) bool { return false }}
	cs := newConsoleServer(deps)

	if err := lc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-fr.startedCh
	close(fr.exitCh)

	deadline := time.Now().Add(time.Second)
	for {
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		rec := httptest.NewRecorder()
		cs.ServeHTTP(rec, req)
		var resp statusResponse
		decodeJSON(t, rec.Body, &resp)
		if resp.Error == wantErr.Error() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("status never surfaced backend error, last: %+v", resp)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ---- /api/start, /api/stop ----

func TestHandleStart_StartsBackend(t *testing.T) {
	cs, lc, _ := newTestConsole(t)
	req := httptest.NewRequest(http.MethodPost, "/api/start", nil)
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	waitForState(t, lc, stateRunning, time.Second)
	_ = lc.Stop()
}

func TestHandleStart_MethodNotAllowed(t *testing.T) {
	cs, _, _ := newTestConsole(t)
	req := httptest.NewRequest(http.MethodGet, "/api/start", nil)
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestHandleStart_ConflictWhenAlreadyRunning(t *testing.T) {
	cs, lc, _ := newTestConsole(t)
	req := httptest.NewRequest(http.MethodPost, "/api/start", nil)
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, req)
	waitForState(t, lc, stateRunning, time.Second)

	rec2 := httptest.NewRecorder()
	cs.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/api/start", nil))
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second start status = %d, want 409", rec2.Code)
	}
	_ = lc.Stop()
}

func TestHandleStop_StopsBackend(t *testing.T) {
	cs, lc, _ := newTestConsole(t)
	cs.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/start", nil))
	waitForState(t, lc, stateRunning, time.Second)

	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/stop", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("stop status = %d, want 202", rec.Code)
	}
	waitForState(t, lc, stateStopped, 2*time.Second)
}

func TestHandleStop_MethodNotAllowed(t *testing.T) {
	cs, _, _ := newTestConsole(t)
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/stop", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// ---- /api/config ----

func TestHandleConfig_GetReturnsDefaults(t *testing.T) {
	cs, _, _ := newTestConsole(t)
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var cfg launcherConfig
	decodeJSON(t, rec.Body, &cfg)
	if cfg != defaultLauncherConfig() {
		t.Errorf("config = %+v, want defaults %+v", cfg, defaultLauncherConfig())
	}
}

func TestHandleConfig_PutThenGetRoundTrips(t *testing.T) {
	cs, _, dir := newTestConsole(t)

	body, _ := json.Marshal(launcherConfig{CPUs: 2, RAMMB: 2048, Deployment: "wsl"})
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var saved launcherConfig
	decodeJSON(t, rec.Body, &saved)
	if saved.CPUs != 2 || saved.RAMMB != 2048 || saved.Deployment != "wsl" {
		t.Errorf("saved config = %+v", saved)
	}

	// GET should reflect the same values, and the file should exist on disk.
	rec2 := httptest.NewRecorder()
	cs.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var got launcherConfig
	decodeJSON(t, rec2.Body, &got)
	if got != saved {
		t.Errorf("GET after PUT = %+v, want %+v", got, saved)
	}
	if _, err := os.Stat(filepath.Join(dir, launcherConfigFileName)); err != nil {
		t.Errorf("expected launcher.json on disk: %v", err)
	}
}

func TestHandleConfig_PutRejectsBadJSON(t *testing.T) {
	cs, _, _ := newTestConsole(t)
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader([]byte("{not json"))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleConfig_PutNormalizesBadValues(t *testing.T) {
	cs, _, _ := newTestConsole(t)
	// Zero cpus/ram and an unknown deployment should fall back to sane values
	// rather than erroring or persisting garbage.
	body, _ := json.Marshal(launcherConfig{CPUs: 0, RAMMB: -1, Deployment: "vmware"})
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body)))
	var saved launcherConfig
	decodeJSON(t, rec.Body, &saved)
	if saved.CPUs != defaultLauncherConfig().CPUs {
		t.Errorf("CPUs = %d, want default", saved.CPUs)
	}
	if saved.RAMMB != defaultLauncherConfig().RAMMB {
		t.Errorf("RAMMB = %d, want default", saved.RAMMB)
	}
	if saved.Deployment != string(backendQEMU) {
		t.Errorf("Deployment = %q, want qemu (vmware is not selectable in v1)", saved.Deployment)
	}
}

func TestHandleConfig_MethodNotAllowed(t *testing.T) {
	cs, _, _ := newTestConsole(t)
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/config", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// ---- /api/images ----

func TestHandleImages_ListEmptyDir(t *testing.T) {
	cs, _, _ := newTestConsole(t)
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/images", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var list []imageInfo
	decodeJSON(t, rec.Body, &list)
	if len(list) != 0 {
		t.Errorf("expected empty list, got %v", list)
	}
}

func multipartUpload(t *testing.T, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}

func TestHandleImages_UploadListDelete(t *testing.T) {
	cs, _, dir := newTestConsole(t)

	body, contentType := multipartUpload(t, "switch.bin", []byte("fake-iol-image-bytes"))
	req := httptest.NewRequest(http.MethodPost, "/api/images", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var uploadResp struct {
		Name       string `json:"name"`
		Size       int64  `json:"size"`
		LivePushed bool   `json:"livePushed"`
	}
	decodeJSON(t, rec.Body, &uploadResp)
	if uploadResp.Name != "switch.bin" {
		t.Errorf("Name = %q, want switch.bin", uploadResp.Name)
	}
	if uploadResp.Size != int64(len("fake-iol-image-bytes")) {
		t.Errorf("Size = %d, want %d", uploadResp.Size, len("fake-iol-image-bytes"))
	}
	if uploadResp.LivePushed {
		t.Error("LivePushed should be false (guiProbe stubbed to false)")
	}

	// File should be on disk.
	if data, err := os.ReadFile(filepath.Join(dir, "images", "switch.bin")); err != nil || string(data) != "fake-iol-image-bytes" {
		t.Errorf("file on disk: data=%q err=%v", data, err)
	}

	// List should now show it.
	rec2 := httptest.NewRecorder()
	cs.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/api/images", nil))
	var list []imageInfo
	decodeJSON(t, rec2.Body, &list)
	if len(list) != 1 || list[0].Name != "switch.bin" {
		t.Fatalf("list = %v, want [switch.bin]", list)
	}

	// Delete it.
	rec3 := httptest.NewRecorder()
	cs.ServeHTTP(rec3, httptest.NewRequest(http.MethodDelete, "/api/images?name=switch.bin", nil))
	if rec3.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200, body=%s", rec3.Code, rec3.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "images", "switch.bin")); !os.IsNotExist(err) {
		t.Errorf("expected file removed, stat err = %v", err)
	}

	// Deleting again should 404.
	rec4 := httptest.NewRecorder()
	cs.ServeHTTP(rec4, httptest.NewRequest(http.MethodDelete, "/api/images?name=switch.bin", nil))
	if rec4.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", rec4.Code)
	}
}

func TestHandleImages_UploadRejectsNonImageExtension(t *testing.T) {
	cs, _, _ := newTestConsole(t)
	body, contentType := multipartUpload(t, "notes.txt", []byte("hello"))
	req := httptest.NewRequest(http.MethodPost, "/api/images", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleImages_UploadMissingFilePart(t *testing.T) {
	cs, _, _ := newTestConsole(t)
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("notfile", "x")
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/images", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleImages_DeleteMissingNameParam(t *testing.T) {
	cs, _, _ := newTestConsole(t)
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/images", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleImages_DeletePathTraversalGuarded(t *testing.T) {
	cs, _, dir := newTestConsole(t)
	// Create a sentinel file OUTSIDE images\ that a naive path join could reach.
	sentinel := filepath.Join(dir, "sentinel.bin")
	if err := os.WriteFile(sentinel, []byte("do-not-delete"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/images?name=..%2Fsentinel.bin", nil))
	// filepath.Base strips the traversal, so this should 404 (no such file
	// literally named "..%2Fsentinel.bin" inside images\), NOT succeed against
	// the sentinel outside images\.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (traversal must not resolve outside images\\)", rec.Code)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("sentinel file must survive: %v", err)
	}
}

func TestHandleImages_MethodNotAllowed(t *testing.T) {
	cs, _, _ := newTestConsole(t)
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/api/images", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// ---- index page ----

func TestHandleIndex_ServesEmbeddedPage(t *testing.T) {
	cs, _, _ := newTestConsole(t)
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("iolbox console")) {
		t.Error("expected embedded page to mention 'iolbox console'")
	}
}

func TestHandleIndex_404ForUnknownPath(t *testing.T) {
	cs, _, _ := newTestConsole(t)
	rec := httptest.NewRecorder()
	cs.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// ---- runConsoleServer against a real listener (end-to-end smoke) ----

func TestRunConsoleServer_ServesOverRealListenerAndShutsDownOnCtxCancel(t *testing.T) {
	cs, _, _ := newTestConsole(t)
	port := freePort(t)
	addr := "127.0.0.1:" + strconv.Itoa(port)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- runConsoleServer(ctx, addr, cs) }()

	// Wait for the server to come up.
	var resp *http.Response
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err = http.Get("http://" + addr + "/api/status")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never came up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runConsoleServer returned error after cancel: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runConsoleServer did not shut down after ctx cancel")
	}
}
