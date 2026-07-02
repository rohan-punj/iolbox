package wsbridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// uploadURL builds the upload endpoint URL with a properly-escaped filename so
// separators/metacharacters survive transport intact for the server to sanitize.
func uploadURL(base, filename string) string {
	return base + "/api/upload/image?filename=" + url.QueryEscape(filename)
}

// newUploadBridge returns a bridge + httptest server whose upload endpoint
// writes into a fresh temp dir, plus that dir. The control server is a no-op
// fake since the upload path never touches it.
func newUploadBridge(t *testing.T, imageDir string) *httptest.Server {
	t.Helper()
	b := New(Config{Addr: "127.0.0.1:0", ImageDir: imageDir}, &fakeControlServer{consolePorts: map[int]int{}})
	ts := httptest.NewServer(b.server.Handler)
	t.Cleanup(ts.Close)
	return ts
}

func TestUploadImageHappyPath(t *testing.T) {
	dir := t.TempDir()
	ts := newUploadBridge(t, dir)

	payload := []byte("fake-iol-image-bytes")
	resp, err := http.Post(uploadURL(ts.URL, "router.bin"), "application/octet-stream", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var r struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "router.bin")
	if r.Path != want {
		t.Fatalf("path = %q, want %q", r.Path, want)
	}
	got, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("written bytes differ: %q", got)
	}
	// No .partial left behind on success.
	if _, err := os.Stat(want + ".partial"); !os.IsNotExist(err) {
		t.Fatalf(".partial must not remain, stat err = %v", err)
	}
}

func TestUploadImagePathTraversalRejected(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "images")
	ts := newUploadBridge(t, dir)

	// Traversal names whose final component lacks a .bin/.iol extension are a
	// flat 400; the important property is that the directory prefix is stripped
	// (path.Base over both / and \), so nothing lands outside the image dir.
	for _, name := range []string{"../../etc/passwd", "/etc/passwd", "../../passwd"} {
		resp, err := http.Post(uploadURL(ts.URL, name), "application/octet-stream", strings.NewReader("x"))
		if err != nil {
			t.Fatalf("post %q: %v", name, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("filename %q: status = %d, want 400 (body %s)", name, resp.StatusCode, body)
		}
	}

	// A Windows-style path with a valid .bin basename reduces to that basename
	// and IS accepted — but only ever inside the image dir, never at the escaped
	// location the traversal prefix names.
	resp, err := http.Post(uploadURL(ts.URL, `..\..\windows\evil.bin`), "application/octet-stream", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("backslash basename should reduce to evil.bin and succeed, got %d", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.bin")); err != nil {
		t.Fatalf("evil.bin must be written INSIDE the image dir: %v", err)
	}
	// Nothing escaped one level up to the traversal target.
	if _, err := os.Stat(filepath.Join(parent, "evil.bin")); !os.IsNotExist(err) {
		t.Fatalf("no file may escape the image dir, stat err = %v", err)
	}
}

func TestUploadImageBadExtensionRejected(t *testing.T) {
	dir := t.TempDir()
	ts := newUploadBridge(t, dir)

	for _, name := range []string{"image.txt", "noext", "image.bin.exe", ".bin", "bad name.bin", ""} {
		resp, err := http.Post(uploadURL(ts.URL, name), "application/octet-stream", strings.NewReader("x"))
		if err != nil {
			t.Fatalf("post %q: %v", name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("filename %q: status = %d, want 400", name, resp.StatusCode)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("no files should be written for rejected names, found %d", len(entries))
	}
}

func TestUploadImageAcceptsIolExtensionCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	ts := newUploadBridge(t, dir)

	resp, err := http.Post(ts.URL+"/api/upload/image?filename=L3-ADVENTERPRISE.IOL", "application/octet-stream", strings.NewReader("iol"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(dir, "L3-ADVENTERPRISE.IOL")); err != nil {
		t.Fatalf("file not written: %v", err)
	}
}

func TestUploadImageNoImageDir503(t *testing.T) {
	ts := newUploadBridge(t, "") // empty ImageDir disables uploads

	resp, err := http.Post(ts.URL+"/api/upload/image?filename=x.bin", "application/octet-stream", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

// errReader yields n good bytes then fails, simulating a client that aborts
// mid-upload (the handler must clean up the .partial and not rename it).
type errReader struct {
	data []byte
	off  int
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, errors.New("simulated connection reset")
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}

func TestUploadImagePartialCleanedUpOnAbortedBody(t *testing.T) {
	dir := t.TempDir()
	ts := newUploadBridge(t, dir)

	// A ContentLength larger than the body we deliver makes the server's read
	// error (unexpected EOF / reset) surface as a copy failure, so the handler
	// takes the cleanup branch.
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/upload/image?filename=aborted.bin", &errReader{data: []byte("partial-data")})
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = 1 << 20 // claim 1 MiB but the reader dies after 12 bytes

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// A transport-level error is also an acceptable outcome; what matters is
		// that no file survived. Fall through to the filesystem assertions.
		t.Logf("client Do error (acceptable): %v", err)
	} else {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Fatalf("aborted upload must not report 200")
		}
	}

	// Neither the final file nor the .partial may survive an aborted body. The
	// server observes the reset and runs its cleanup asynchronously to the
	// client's Do returning, so poll briefly for the .partial to disappear.
	partial := filepath.Join(dir, "aborted.bin.partial")
	gone := false
	for i := 0; i < 200; i++ {
		if _, err := os.Stat(partial); os.IsNotExist(err) {
			gone = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !gone {
		t.Fatalf(".partial must be cleaned up after an aborted body")
	}
	if _, err := os.Stat(filepath.Join(dir, "aborted.bin")); !os.IsNotExist(err) {
		t.Fatalf("final file must not exist, stat err = %v", err)
	}
}

func TestUploadImageWrongMethod(t *testing.T) {
	dir := t.TempDir()
	ts := newUploadBridge(t, dir)

	resp, err := http.Get(ts.URL + "/api/upload/image?filename=x.bin")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}
