package wsbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/node"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
	"github.com/rohanpunj/iolbox/supervisor/internal/server"
	"github.com/rohanpunj/iolbox/supervisor/internal/tool"
	"github.com/rohanpunj/iolbox/supervisor/internal/ws"
)

// --- minimal WebSocket client helpers (test-only; mirrors internal/ws framing) ---

func dialWS(t *testing.T, url string) net.Conn {
	return dialWSHeaders(t, url, nil)
}

func dialAuthenticatedWS(t *testing.T, b *Bridge, url string) net.Conn {
	addr := strings.TrimPrefix(url, "ws://")
	if i := strings.Index(addr, "/"); i >= 0 {
		addr = addr[:i]
	}
	return dialWSHeaders(t, url, map[string]string{
		"Cookie": "iolbox_session=" + b.sessionToken,
		"Origin": "http://" + addr,
	})
}

func dialWSHeaders(t *testing.T, url string, headers map[string]string) net.Conn {
	t.Helper()
	addr := strings.TrimPrefix(url, "ws://")
	path := "/"
	if i := strings.Index(addr, "/"); i >= 0 {
		path = addr[i:]
		addr = addr[:i]
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n"
	for key, value := range headers {
		req += key + ": " + value + "\r\n"
	}
	req += "\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write handshake: %v", err)
	}
	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	if !strings.Contains(status, "101") {
		t.Fatalf("handshake failed: %s", status)
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read headers: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}
	return &bufReadConn{Conn: conn, br: br}
}

// bufReadConn lets us keep using net.Conn's interface after consuming the
// handshake through a bufio.Reader (so buffered bytes aren't lost).
type bufReadConn struct {
	net.Conn
	br *bufio.Reader
}

func (c *bufReadConn) Read(p []byte) (int, error) { return c.br.Read(p) }

func writeClientFrame(conn net.Conn, op ws.Opcode, payload []byte) error {
	var buf bytes.Buffer
	buf.WriteByte(0x80 | byte(op))
	n := len(payload)
	switch {
	case n < 126:
		buf.WriteByte(0x80 | byte(n))
	case n <= 0xFFFF:
		buf.WriteByte(0x80 | 126)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(n))
		buf.Write(ext[:])
	default:
		buf.WriteByte(0x80 | 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		buf.Write(ext[:])
	}
	var key [4]byte
	rand.Read(key[:])
	buf.Write(key[:])
	masked := make([]byte, n)
	copy(masked, payload)
	for i := range masked {
		masked[i] ^= key[i%4]
	}
	buf.Write(masked)
	_, err := conn.Write(buf.Bytes())
	return err
}

func readServerFrame(t *testing.T, conn net.Conn) (ws.Opcode, []byte) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(conn)
	b0, err := br.ReadByte()
	if err != nil {
		t.Fatalf("read frame byte0: %v", err)
	}
	b1, err := br.ReadByte()
	if err != nil {
		t.Fatalf("read frame byte1: %v", err)
	}
	op := ws.Opcode(b0 & 0x0F)
	length := uint64(b1 & 0x7F) // server frames are never masked
	switch length {
	case 126:
		var ext [2]byte
		io.ReadFull(br, ext[:])
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		io.ReadFull(br, ext[:])
		length = binary.BigEndian.Uint64(ext[:])
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(br, data); err != nil {
		t.Fatalf("read frame payload: %v", err)
	}
	return op, data
}

// --- /control end-to-end test ---

func TestControlEndpointRoundTrip(t *testing.T) {
	srv := server.New(server.Config{ControlAddr: "127.0.0.1:0", ImageDir: "/img", RunDir: "/run", Version: "test"})
	b := New(Config{Addr: "127.0.0.1:0"}, srv)

	ts := httptest.NewServer(b.server.Handler)
	defer ts.Close()

	wsURL := "ws://" + strings.TrimPrefix(ts.URL, "http://") + "/control"
	conn := dialAuthenticatedWS(t, b, wsURL)
	defer conn.Close()

	req := protocol.Request{ID: "1", Op: "hello", Args: mustJSON(protocol.HelloArgs{Client: "test"})}
	reqBytes, _ := json.Marshal(req)
	if err := writeClientFrame(conn, ws.OpText, reqBytes); err != nil {
		t.Fatalf("write request frame: %v", err)
	}

	op, data := readServerFrame(t, conn)
	if op != ws.OpText {
		t.Fatalf("expected text frame, got opcode %d", op)
	}
	var resp protocol.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal response: %v (data=%s)", err, data)
	}
	if !resp.OK || resp.ID != "1" {
		t.Fatalf("hello over ws failed: %+v", resp)
	}
	var hr protocol.HelloResult
	json.Unmarshal(resp.Result, &hr)
	if hr.Supervisor != "test" {
		t.Fatalf("hello result: %+v", hr)
	}
}

func TestControlEndpointMultipleRequests(t *testing.T) {
	srv := server.New(server.Config{ControlAddr: "127.0.0.1:0", ImageDir: "/img", RunDir: "/run", Version: "test"})
	b := New(Config{Addr: "127.0.0.1:0"}, srv)
	ts := httptest.NewServer(b.server.Handler)
	defer ts.Close()

	wsURL := "ws://" + strings.TrimPrefix(ts.URL, "http://") + "/control"
	conn := dialAuthenticatedWS(t, b, wsURL)
	defer conn.Close()

	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("req-%d", i)
		req := protocol.Request{ID: id, Op: "status"}
		reqBytes, _ := json.Marshal(req)
		if err := writeClientFrame(conn, ws.OpText, reqBytes); err != nil {
			t.Fatalf("write: %v", err)
		}
		_, data := readServerFrame(t, conn)
		var resp protocol.Response
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.ID != id || !resp.OK {
			t.Fatalf("resp %d: %+v", i, resp)
		}
	}
}

// --- static GUI mux precedence tests ---

// TestStaticFallbackDoesNotShadowControl proves the "/" catch-all serving the
// embedded GUI never intercepts the WS routes: a proper /control handshake
// still upgrades (101), and /console/ still reaches its handler (404 for an
// unknown node), while an unrelated path is served the GUI's index.html.
func TestStaticFallbackDoesNotShadowControl(t *testing.T) {
	srv := server.New(server.Config{ControlAddr: "127.0.0.1:0", ImageDir: "/img", RunDir: "/run", Version: "test"})
	b := New(Config{Addr: "127.0.0.1:0"}, srv)
	ts := httptest.NewServer(b.server.Handler)
	defer ts.Close()

	// /control still upgrades to WebSocket (not swallowed by static handler).
	wsURL := "ws://" + strings.TrimPrefix(ts.URL, "http://") + "/control"
	conn := dialAuthenticatedWS(t, b, wsURL)
	conn.Close()

	// A plain GET to "/" is served the embedded GUI, not routed to /control.
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("GET / Content-Type = %q, want text/html*", ct)
	}
}

// TestConsoleNotShadowedByStatic confirms /console/ still hits the console
// handler (returning 404 for an unknown node) rather than the GUI fallback.
func TestConsoleNotShadowedByStatic(t *testing.T) {
	fake := &fakeControlServer{consolePorts: map[int]int{}}
	b := New(Config{Addr: "127.0.0.1:0"}, fake)
	ts := httptest.NewServer(b.server.Handler)
	defer ts.Close()

	status, err := httpGetUpgradeAttemptAuthenticated(ts.URL+"/console/42", b)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if !strings.Contains(status, "404") {
		t.Fatalf("expected 404 from console handler, got: %s", status)
	}
}

const testToolHost = "192.168.226.233:4001"

func newToolBridge(t *testing.T, upstream http.Handler, routes []tool.ProxyRoute) (*Bridge, *httptest.Server) {
	t.Helper()
	socket := filepath.Base(t.TempDir()) + "-gui.sock"
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Skipf("AF_UNIX is unavailable on this native test host: %v", err)
	}
	gui := &http.Server{Handler: upstream}
	go func() { _ = gui.Serve(listener) }()
	t.Cleanup(func() { _ = gui.Close() })
	t.Cleanup(func() { _ = os.Remove(socket) })

	fake := &fakeControlServer{proxySocket: socket, proxyRoutes: routes}
	b := New(Config{Addr: "127.0.0.1:0"}, fake)
	ts := httptest.NewServer(b.server.Handler)
	t.Cleanup(ts.Close)
	return b, ts
}

func newToolRequest(t *testing.T, b *Bridge, ts *httptest.Server, method, requestPath string, cookie bool, origin string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+requestPath, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = testToolHost
	if cookie {
		req.AddCookie(&http.Cookie{Name: "iolbox_session", Value: b.sessionToken})
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	return req
}

func doToolRequest(t *testing.T, b *Bridge, ts *httptest.Server, method, requestPath string, cookie bool, origin string) *http.Response {
	t.Helper()
	resp, err := http.DefaultClient.Do(newToolRequest(t, b, ts, method, requestPath, cookie, origin))
	if err != nil {
		t.Fatalf("tool request: %v", err)
	}
	return resp
}

func TestToolProxyRejectsForeignOriginWithValidCookie(t *testing.T) {
	b, ts := newToolBridge(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), []tool.ProxyRoute{{Prefix: "/", AllowWS: true}})
	resp := doToolRequest(t, b, ts, http.MethodGet, "/tool/7/attacks/arp_spoof", true, "http://evil.example")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin status = %d, want 403", resp.StatusCode)
	}
}

func TestToolProxyRejectsMissingSessionForHTTPAndWebSocket(t *testing.T) {
	b, ts := newToolBridge(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), []tool.ProxyRoute{{Prefix: "/", AllowWS: true}})
	resp := doToolRequest(t, b, ts, http.MethodGet, "/tool/7/", false, "http://"+testToolHost)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing-session HTTP status = %d, want 401", resp.StatusCode)
	}
	status := websocketStatus(t, "ws://"+strings.TrimPrefix(ts.URL, "http://")+"/tool/7", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("missing-session WS status = %d, want 401", status)
	}
}

func TestToolProxyAcceptsCookieAndRequestHostOrigin(t *testing.T) {
	b, ts := newToolBridge(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}), []tool.ProxyRoute{{Prefix: "/", AllowWS: true}})
	resp := doToolRequest(t, b, ts, http.MethodGet, "/tool/7/", true, "http://"+testToolHost)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("valid cookie/origin status = %d, want 200", resp.StatusCode)
	}
}

func TestToolProxyRejectsWebSocketWithoutAllowWSRoute(t *testing.T) {
	b, ts := newToolBridge(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusSwitchingProtocols)
	}), []tool.ProxyRoute{{Prefix: "/", AllowWS: false}})
	status := websocketStatus(t, "ws://"+strings.TrimPrefix(ts.URL, "http://")+"/tool/7/", map[string]string{
		"Cookie": "iolbox_session=" + b.sessionToken,
		"Origin": "http://" + strings.TrimPrefix(ts.URL, "http://"),
	})
	if status != http.StatusForbidden && status != http.StatusBadRequest {
		t.Fatalf("disallowed WS status = %d, want 400 or 403", status)
	}
}

func TestToolProxyTraversalNeverReachesControl(t *testing.T) {
	fake := &fakeControlServer{}
	b := New(Config{Addr: "127.0.0.1:0"}, fake)
	for _, requestTarget := range []string{
		"http://" + testToolHost + "/tool/7/../control",
		"http://" + testToolHost + "/tool/7/%2e%2e/control",
	} {
		req := httptest.NewRequest(http.MethodGet, requestTarget, nil)
		req.Host = testToolHost
		req.AddCookie(&http.Cookie{Name: "iolbox_session", Value: b.sessionToken})
		req.Header.Set("Referer", "http://"+testToolHost+"/")
		if !strings.Contains(req.URL.Path, "..") {
			t.Fatalf("test request Path %q lost traversal segment", req.URL.Path)
		}
		if strings.Contains(requestTarget, "%2e%2e") && !strings.Contains(req.URL.RawPath, "%2e%2e") {
			t.Fatalf("encoded test request RawPath %q lost encoded traversal", req.URL.RawPath)
		}
		rr := httptest.NewRecorder()
		b.server.Handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("traversal %q status = %d, want 404", requestTarget, rr.Code)
		}
	}
}

func TestToolProxyHTMLResponseHasFrameAncestorsCSP(t *testing.T) {
	b, ts := newToolBridge(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><body>ok</body></html>")
	}), []tool.ProxyRoute{{Prefix: "/", AllowWS: true}})
	resp := doToolRequest(t, b, ts, http.MethodGet, "/tool/7/", true, "http://"+testToolHost)
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Security-Policy"); got != "frame-ancestors 'self'" {
		t.Fatalf("CSP = %q, want frame-ancestors policy", got)
	}
}

func TestSessionGateRejectsUnauthenticatedSharedRoutes(t *testing.T) {
	b := New(Config{Addr: "127.0.0.1:0"}, &fakeControlServer{})
	ts := httptest.NewServer(b.server.Handler)
	defer ts.Close()
	for _, requestPath := range []string{"/control", "/console/7", "/capture/7"} {
		status := websocketStatus(t, "ws://"+strings.TrimPrefix(ts.URL, "http://")+requestPath, nil)
		if status != http.StatusUnauthorized {
			t.Errorf("unauthenticated %s status = %d, want 401", requestPath, status)
		}
	}
}

func TestToolProxyAcceptsBearerTokenWithoutCookie(t *testing.T) {
	b, ts := newToolBridge(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), []tool.ProxyRoute{{Prefix: "/", AllowWS: true}})
	req := newToolRequest(t, b, ts, http.MethodGet, "/tool/7/", false, "http://"+testToolHost)
	req.Header.Set("Authorization", "Bearer "+b.sessionToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("bearer request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bearer status = %d, want 200", resp.StatusCode)
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	b := New(Config{Addr: "127.0.0.1:0"}, &fakeControlServer{})
	ts := httptest.NewServer(b.server.Handler)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("SPA request: %v", err)
	}
	defer resp.Body.Close()
	var session *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "iolbox_session" {
			session = cookie
			break
		}
	}
	if session == nil {
		t.Fatal("SPA response did not set iolbox_session")
	}
	if !session.HttpOnly || session.SameSite != http.SameSiteStrictMode || session.Path != "/" || session.Secure {
		t.Fatalf("session cookie = %#v, want HttpOnly; SameSite=Strict; Path=/ without Secure on HTTP", session)
	}
}

func TestToolProxyStripsSessionAndForwardedHeadersButKeepsOtherCookies(t *testing.T) {
	var received *http.Request
	b, ts := newToolBridge(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r
		w.WriteHeader(http.StatusOK)
	}), []tool.ProxyRoute{{Prefix: "/", AllowWS: true}})
	req := newToolRequest(t, b, ts, http.MethodGet, "/tool/7/", true, "http://"+testToolHost)
	req.Header.Set("Cookie", "iolbox_session="+b.sessionToken+"; other=kept")
	req.Header.Set("X-Forwarded-For", "198.51.100.1")
	req.Header.Set("X-Forwarded-Host", "evil.example")
	req.Header.Set("Forwarded", "for=198.51.100.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("header sanitization request: %v", err)
	}
	resp.Body.Close()
	if received == nil {
		t.Fatal("upstream did not receive request")
	}
	if received.Header.Get("Accept-Encoding") != "" || received.Header.Get("X-Forwarded-For") != "" || received.Header.Get("X-Forwarded-Host") != "" || received.Header.Get("Forwarded") != "" {
		t.Fatalf("sanitized upstream headers = %#v", received.Header)
	}
	if received.Header.Get("Cookie") != "other=kept" {
		t.Fatalf("upstream Cookie = %q, want unrelated cookie only", received.Header.Get("Cookie"))
	}
}

func TestToolHTMLRewriterAttributesAndLocation(t *testing.T) {
	prefix := "/tool/7/"
	body := []byte(`<base href="/wrong/"><a href="/foo">x</a><img src="/img.png"><form action="/submit"><input hx-get="/get" hx-post="/post" hx-delete="/delete"></form>`)
	rewritten, err := rewriteHTML(body, prefix)
	if err != nil {
		t.Fatalf("rewriteHTML: %v", err)
	}
	got := string(rewritten)
	for _, want := range []string{
		`href="/tool/7/"`, `href="/tool/7/foo"`, `src="/tool/7/img.png"`,
		`action="/tool/7/submit"`, `hx-get="/tool/7/get"`,
		`hx-post="/tool/7/post"`, `hx-delete="/tool/7/delete"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rewritten HTML missing %q: %s", want, got)
		}
	}
	if got := rewriteRootURL("/redirect", prefix); got != "/tool/7/redirect" {
		t.Fatalf("rewritten Location = %q, want /tool/7/redirect", got)
	}
}

func TestToolHTMLRewriterPassesNonHTMLAndOverCapBodies(t *testing.T) {
	for _, test := range []struct {
		name string
		body []byte
		head http.Header
		len  int64
	}{
		{name: "non-html", body: []byte(`/asset`), head: http.Header{"Content-Type": []string{"application/javascript"}}, len: 6},
		{name: "over-cap", body: bytes.Repeat([]byte("x"), toolHTMLRewriteCap+1), head: http.Header{"Content-Type": []string{"text/html"}}, len: toolHTMLRewriteCap + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := &http.Response{Header: test.head, ContentLength: test.len, Body: io.NopCloser(bytes.NewReader(test.body))}
			if err := rewriteHTMLResponse(response, "/tool/7/"); err != nil {
				t.Fatalf("rewriteHTMLResponse: %v", err)
			}
			got, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if !bytes.Equal(got, test.body) {
				t.Fatalf("body changed for %s", test.name)
			}
		})
	}
}

func TestToolHTMLRewriterPassesEncodedHTMLOpaque(t *testing.T) {
	body := []byte(`<a href="/must-not-change">x</a>`)
	response := &http.Response{
		Header:        http.Header{"Content-Type": []string{"text/html"}, "Content-Encoding": []string{"gzip"}},
		ContentLength: int64(len(body)),
		Body:          io.NopCloser(bytes.NewReader(body)),
	}
	if err := rewriteHTMLResponse(response, "/tool/7/"); err != nil {
		t.Fatalf("rewriteHTMLResponse: %v", err)
	}
	got, _ := io.ReadAll(response.Body)
	if !bytes.Equal(got, body) {
		t.Fatalf("encoded HTML was rewritten: %q", got)
	}
}

func TestToolHTMLRewriterDechunksAndSetsCorrectLength(t *testing.T) {
	body := []byte(`<a href="/foo">x</a>`)
	response := &http.Response{
		Header:           http.Header{"Content-Type": []string{"text/html"}, "Transfer-Encoding": []string{"chunked"}},
		ContentLength:    -1,
		TransferEncoding: []string{"chunked"},
		Body:             io.NopCloser(bytes.NewReader(body)),
	}
	if err := rewriteHTMLResponse(response, "/tool/7/"); err != nil {
		t.Fatalf("rewriteHTMLResponse: %v", err)
	}
	got, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(got), `href="/tool/7/foo"`) {
		t.Fatalf("chunked HTML was not rewritten: %q", got)
	}
	if response.ContentLength != int64(len(got)) || response.Header.Get("Content-Length") != strconv.Itoa(len(got)) || len(response.TransferEncoding) != 0 || response.Header.Get("Transfer-Encoding") != "" {
		t.Fatalf("dechunked response metadata = length %d, header %q, transfer %v", response.ContentLength, response.Header.Get("Content-Length"), response.TransferEncoding)
	}
}

func websocketStatus(t *testing.T, wsURL string, headers map[string]string) int {
	t.Helper()
	addr := strings.TrimPrefix(wsURL, "ws://")
	path := "/"
	if i := strings.Index(addr, "/"); i >= 0 {
		path = addr[i:]
		addr = addr[:i]
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("WS dial: %v", err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n", path, addr)
	for key, value := range headers {
		fmt.Fprintf(conn, "%s: %s\r\n", key, value)
	}
	fmt.Fprint(conn, "\r\n")
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("WS status: %v", err)
	}
	var status int
	if _, err := fmt.Sscanf(line, "HTTP/1.1 %d", &status); err != nil {
		t.Fatalf("parse WS status %q: %v", line, err)
	}
	return status
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// --- console bridge test ---

func TestConsoleEndpointTelnetNegotiationAndData(t *testing.T) {
	// Fake telnet server: on accept, sends IAC WILL ECHO, IAC WILL SGA, then
	// "login: " and echoes back whatever it receives after that, so the test
	// can assert both negotiation stripping and bidirectional data flow.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.Write([]byte{telnetIAC, telnetWILL, telnetOptEcho})
		c.Write([]byte{telnetIAC, telnetWILL, telnetOptSGA})
		c.Write([]byte("login: "))
		// The bridge answers our WILL ECHO/WILL SGA with IAC DO ECHO/IAC DO
		// SGA negotiation replies before the test's keystroke arrives; skip
		// those (any read consisting solely of IAC-prefixed bytes) and only
		// echo the first read that looks like real application data.
		buf := make([]byte, 256)
		for {
			n, err := c.Read(buf)
			if err != nil {
				return
			}
			if n > 0 && buf[0] == telnetIAC {
				continue // negotiation reply, not a keystroke
			}
			c.Write(buf[:n]) // echo back what the client typed
			return
		}
	}()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	fake := &fakeControlServer{consolePorts: map[int]int{7: port}}
	b := New(Config{Addr: "127.0.0.1:0"}, fake)
	ts := httptest.NewServer(b.server.Handler)
	defer ts.Close()

	wsURL := "ws://" + strings.TrimPrefix(ts.URL, "http://") + "/console/7"
	conn := dialAuthenticatedWS(t, b, wsURL)
	defer conn.Close()

	// First frame(s) from the bridge should be clean application data with
	// IAC negotiation stripped: "login: " with no 0xFF bytes.
	op, data := readServerFrame(t, conn)
	if op != ws.OpBinary {
		t.Fatalf("expected binary frame, got opcode %d", op)
	}
	if bytes.Contains(data, []byte{0xFF}) {
		t.Fatalf("IAC byte leaked into client frame: %v", data)
	}
	if !bytes.Contains(data, []byte("login: ")) {
		t.Fatalf("expected greeting text, got %q", data)
	}

	// Send a keystroke; expect it echoed back cleanly.
	if err := writeClientFrame(conn, ws.OpBinary, []byte("admin\n")); err != nil {
		t.Fatalf("write keystroke: %v", err)
	}
	op, data = readServerFrame(t, conn)
	if op != ws.OpBinary || string(data) != "admin\n" {
		t.Fatalf("echo: op=%d data=%q", op, data)
	}
}

// TestConsoleResizeFrameNotForwardedAsRawBytes is the regression test for a
// live bug: a resize control frame on the VPCS raw-telnet path used to write
// a NAWS subnegotiation straight into the node's telnet socket, but our
// Negotiator never actually agrees to NAWS (it only ever agrees to SGA — see
// telnet.Negotiator.handleOption), so this was an un-negotiated, unsolicited
// sequence VPCS's small telnet parser could fail to fully consume — leaking a
// trailing byte into the command line as a phantom keystroke at the VPCS
// prompt, repeating every time the console's ResizeObserver fired (even from
// layout jitter with no actual size change). The fix makes resize a pure
// parse-and-ignore no-op on this path too (see validateResizeFrame), matching
// bridgeConsoleSub's already-established IOL behavior. This test proves NO
// bytes reach the telnet socket for a resize frame, while a real keystroke
// still does.
func TestConsoleResizeFrameNotForwardedAsRawBytes(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	received := make(chan []byte, 8)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 256)
		for {
			n, rerr := c.Read(buf)
			if n > 0 {
				got := make([]byte, n)
				copy(got, buf[:n])
				received <- got
			}
			if rerr != nil {
				return
			}
		}
	}()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	fake := &fakeControlServer{consolePorts: map[int]int{7: port}}
	b := New(Config{Addr: "127.0.0.1:0"}, fake)
	ts := httptest.NewServer(b.server.Handler)
	defer ts.Close()

	wsURL := "ws://" + strings.TrimPrefix(ts.URL, "http://") + "/console/7"
	conn := dialAuthenticatedWS(t, b, wsURL)
	defer conn.Close()

	if err := writeClientFrame(conn, ws.OpText, []byte(`{"resize":{"cols":80,"rows":24}}`)); err != nil {
		t.Fatalf("write resize frame: %v", err)
	}
	// A real keystroke right after proves the connection is alive and the
	// fake server DOES receive data in general — only the resize frame itself
	// must produce nothing.
	if err := writeClientFrame(conn, ws.OpBinary, []byte("x")); err != nil {
		t.Fatalf("write keystroke: %v", err)
	}

	select {
	case got := <-received:
		if string(got) != "x" {
			t.Fatalf("expected only the keystroke to reach the telnet socket, got %q (resize leaked?)", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("keystroke never reached the telnet socket")
	}

	select {
	case extra := <-received:
		t.Fatalf("unexpected extra bytes reached the telnet socket after the keystroke: %q", extra)
	case <-time.After(200 * time.Millisecond):
		// nothing else arrived — good.
	}
}

// TestConsoleEndpointInProcessSubscription is the v0.3.0 Phase 2 regression
// test: when ConsoleSubscribe returns a live subscription (simulating a
// running IOL node's hub), handleConsole must use the in-process path
// (bridgeConsoleSub) — no TCP dial, no telnet.Negotiator of its own — and the
// client must see clean application bytes plus a working bidirectional
// keystroke path, identical in externally-observable behavior to the
// TCP-dial path exercised by TestConsoleEndpointTelnetNegotiationAndData.
func TestConsoleEndpointInProcessSubscription(t *testing.T) {
	// A fake "pty": write to serverSide.outW to simulate node output; read
	// from serverSide.inR to observe what the browser typed reaching the pty.
	outR, outW := io.Pipe() // node output -> hub read side
	inR, inW := io.Pipe()   // hub write side -> "pty" input
	pty := &pipePty{r: outR, w: inW}

	sub := node.NewSubscriptionForTest(pty, "")

	fake := &fakeControlServer{
		consolePorts:  map[int]int{},
		subscriptions: map[int]*node.Subscription{7: sub},
	}
	b := New(Config{Addr: "127.0.0.1:0"}, fake)
	ts := httptest.NewServer(b.server.Handler)
	defer ts.Close()

	wsURL := "ws://" + strings.TrimPrefix(ts.URL, "http://") + "/console/7"
	conn := dialAuthenticatedWS(t, b, wsURL)
	defer conn.Close()

	// Feed some "node output" through the fake pty.
	go func() { outW.Write([]byte("R1#")) }()

	op, data := readServerFrame(t, conn)
	if op != ws.OpBinary {
		t.Fatalf("expected binary frame, got opcode %d", op)
	}
	// No IAC bytes: the in-process path never runs its own Negotiator, and the
	// hub's Subscribe never sends the telnet preamble to in-process
	// subscribers (see consoleHub.registerLocked's wantTelnetPreamble=false).
	if bytes.Contains(data, []byte{0xFF}) {
		t.Fatalf("IAC byte leaked into in-process subscriber's client frame: %v", data)
	}
	if !bytes.Contains(data, []byte("R1#")) {
		t.Fatalf("expected node output, got %q", data)
	}

	// Keystrokes flow browser -> ws -> subscription.Write -> pty.
	if err := writeClientFrame(conn, ws.OpBinary, []byte("show version\r")); err != nil {
		t.Fatalf("write keystroke: %v", err)
	}
	buf := make([]byte, 64)
	n, err := inR.Read(buf)
	if err != nil {
		t.Fatalf("read from fake pty: %v", err)
	}
	if string(buf[:n]) != "show version\r" {
		t.Fatalf("fake pty received %q, want %q", buf[:n], "show version\r")
	}
}

// pipePty adapts a pair of io.Pipe halves to the io.ReadWriter a consoleHub
// wants for its pty, for the in-process subscription test above.
type pipePty struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (p *pipePty) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *pipePty) Write(b []byte) (int, error) { return p.w.Write(b) }

func TestConsoleEndpointUnknownNode(t *testing.T) {
	fake := &fakeControlServer{consolePorts: map[int]int{}}
	b := New(Config{Addr: "127.0.0.1:0"}, fake)
	ts := httptest.NewServer(b.server.Handler)
	defer ts.Close()

	resp, err := httpGetUpgradeAttemptAuthenticated(ts.URL+"/console/99", b)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if !strings.Contains(resp, "404") {
		t.Fatalf("expected 404 for unknown node, got: %s", resp)
	}
}

// --- capture bridge tests ---

// TestCaptureEndpointStreamsBytes stands up a fake capture server that, on
// accept, writes a fixed pcapng-shaped byte blob; the bridge should upgrade to
// WebSocket and deliver that blob to the client as binary frames.
func TestCaptureEndpointStreamsBytes(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// A recognizable byte stream standing in for the pcapng SHB + a frame.
	payload := []byte{0x0a, 0x0d, 0x0d, 0x0a, 0xDE, 0xAD, 0xBE, 0xEF}
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.Write(payload)
		// Keep the connection open briefly so the frame is delivered before EOF.
		time.Sleep(200 * time.Millisecond)
	}()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	fake := &fakeControlServer{capturePorts: map[int]int{5: port}}
	b := New(Config{Addr: "127.0.0.1:0"}, fake)
	ts := httptest.NewServer(b.server.Handler)
	defer ts.Close()

	wsURL := "ws://" + strings.TrimPrefix(ts.URL, "http://") + "/capture/5"
	conn := dialAuthenticatedWS(t, b, wsURL)
	defer conn.Close()

	op, data := readServerFrame(t, conn)
	if op != ws.OpBinary {
		t.Fatalf("expected binary frame, got opcode %d", op)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("capture stream = %v, want %v", data, payload)
	}
}

// TestCaptureEndpointNoActiveCapture confirms a link with no active capture is
// rejected with a 404 and a JSON error body, before any WS upgrade.
func TestCaptureEndpointNoActiveCapture(t *testing.T) {
	fake := &fakeControlServer{capturePorts: map[int]int{}}
	b := New(Config{Addr: "127.0.0.1:0"}, fake)
	ts := httptest.NewServer(b.server.Handler)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/capture/42", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "iolbox_session", Value: b.sessionToken})
	req.Header.Set("Referer", ts.URL+"/")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error == "" {
		t.Fatalf("expected non-empty error message, got %+v", body)
	}
}

// httpGetUpgradeAttempt issues a plain (non-websocket) GET and returns the
// raw status line, enough to check the 404 path without a full handshake.
func httpGetUpgradeAttempt(url string) (string, error) {
	return httpGetUpgradeAttemptHeaders(url, nil)
}

func httpGetUpgradeAttemptAuthenticated(url string, b *Bridge) (string, error) {
	addr := strings.TrimPrefix(url, "http://")
	if i := strings.Index(addr, "/"); i >= 0 {
		addr = addr[:i]
	}
	return httpGetUpgradeAttemptHeaders(url, map[string]string{
		"Cookie":  "iolbox_session=" + b.sessionToken,
		"Referer": "http://" + addr + "/",
	})
}

func httpGetUpgradeAttemptHeaders(url string, headers map[string]string) (string, error) {
	addr := strings.TrimPrefix(url, "http://")
	path := "/"
	if i := strings.Index(addr, "/"); i >= 0 {
		path = addr[i:]
		addr = addr[:i]
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n", path, addr)
	for key, value := range headers {
		fmt.Fprintf(conn, "%s: %s\r\n", key, value)
	}
	fmt.Fprint(conn, "\r\n")
	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	return status, err
}

// fakeControlServer implements ControlServer without a real lab/server.Server,
// for testing the console/capture bridges in isolation. subscriptions, if
// non-nil, makes ConsoleSubscribe return a canned *node.Subscription for the
// given node id (simulating a running IOL node's hub); a node id with no
// entry falls back to nil, exactly like Server.ConsoleSubscribe does for a
// VPCS node or one with no running hub — exercising handleConsole's fallback
// to the TCP dial path.
type fakeControlServer struct {
	consolePorts  map[int]int
	capturePorts  map[int]int
	subscriptions map[int]*node.Subscription
	proxySocket   string
	proxyRoutes   []tool.ProxyRoute
}

func (f *fakeControlServer) ServeConn(ctx context.Context, rwc io.ReadWriteCloser) {}

func (f *fakeControlServer) ConsolePort(nodeID int) (int, bool) {
	p, ok := f.consolePorts[nodeID]
	return p, ok
}

func (f *fakeControlServer) ConsoleSubscribe(nodeID int) *node.Subscription {
	return f.subscriptions[nodeID]
}

func (f *fakeControlServer) CapturePort(linkID int) (int, bool) {
	p, ok := f.capturePorts[linkID]
	return p, ok
}

func (f *fakeControlServer) ToolProxyTarget(_ int) (string, []tool.ProxyRoute, bool) {
	if f.proxySocket == "" {
		return "", nil, false
	}
	return f.proxySocket, f.proxyRoutes, true
}

// Telnet constants duplicated here (rather than importing internal/telnet's
// unexported details) to keep this test focused on wire bytes as an external
// telnet peer would send them.
const (
	telnetIAC     = 255
	telnetWILL    = 251
	telnetOptEcho = 1
	telnetOptSGA  = 3
)
