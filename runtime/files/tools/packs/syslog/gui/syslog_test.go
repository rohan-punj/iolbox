package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseSyslogRealShapedInputs(t *testing.T) {
	structured := `<165>1 2026-08-09T12:34:56.123Z host app 123 ID47 [meta msg="embedded space with \"quote\" and \\ slash \] end"][other x="two words"] payload here`
	cases := []struct {
		name         string
		raw          string
		wantFacility int
		wantSeverity int
		wantDevice   string
		wantHostname string
		wantTag      string
		wantMessage  string
		wantPriority bool
		wantSD       string
	}{
		{
			name:         "Cisco with sequence and fractional unsynced timestamp",
			raw:          `<190>123: *Mar  1 00:01:02.345: %SYS-5-CONFIG_I: Configured from console by console`,
			wantFacility: 23, wantSeverity: 6, wantDevice: "*Mar  1 00:01:02.345",
			wantHostname: "192.0.2.10", wantTag: "%SYS-5-CONFIG_I", wantMessage: "Configured from console by console", wantPriority: true,
		},
		{
			name:         "Cisco without sequence",
			raw:          `<190>*Mar  1 00:01:02.345: %SYS-5-CONFIG_I: Configured from console by console`,
			wantFacility: 23, wantSeverity: 6, wantDevice: "*Mar  1 00:01:02.345",
			wantHostname: "192.0.2.10", wantTag: "%SYS-5-CONFIG_I", wantMessage: "Configured from console by console", wantPriority: true,
		},
		{
			// Found live against a real IOL router (17.18.02): the syslog
			// UDP payload carried TWO stacked sequence-number prefixes, not
			// the single one RFC 3164 / a naive Cisco parser anticipates.
			name:         "Cisco with double-stacked sequence numbers (real hardware)",
			raw:          `<189>96: 000091: *Aug  9 15:31:14.447: %SEC_LOGIN-5-LOGIN_SUCCESS: Login Success [user: labuser] [Source: LOCAL] [localport: 0] at 15:31:14 UTC Sun Aug 9 2026`,
			wantFacility: 23, wantSeverity: 5, wantDevice: "*Aug  9 15:31:14.447",
			wantHostname: "192.0.2.10", wantTag: "%SEC_LOGIN-5-LOGIN_SUCCESS",
			wantMessage:  "Login Success [user: labuser] [Source: LOCAL] [localport: 0] at 15:31:14 UTC Sun Aug 9 2026",
			wantPriority: true,
		},
		{
			name:         "RFC 3164 padded day",
			raw:          `<165>Mar  1 12:34:56 router sshd[42]: Accepted password`,
			wantFacility: 20, wantSeverity: 5, wantDevice: "Mar  1 12:34:56",
			wantHostname: "router", wantTag: "sshd", wantMessage: "Accepted password", wantPriority: true,
		},
		{
			name:         "RFC 5424 structured data with escapes",
			raw:          structured,
			wantFacility: 20, wantSeverity: 5, wantDevice: "2026-08-09T12:34:56.123Z",
			wantHostname: "host", wantTag: "app", wantMessage: "payload here", wantPriority: true,
			wantSD: `[meta msg="embedded space with \"quote\" and \\ slash \] end"][other x="two words"]`,
		},
		{
			name:         "RFC 5424 nil structured data",
			raw:          `<34>1 - host app - - - hello`,
			wantFacility: 4, wantSeverity: 2, wantDevice: "-",
			wantHostname: "host", wantTag: "app", wantMessage: "hello", wantPriority: true,
			wantSD: "-",
		},
		{
			name:         "no PRI is retained as unparsed",
			raw:          `Mar  1 12:34:56 router sshd: Accepted password`,
			wantFacility: 0, wantSeverity: 0, wantDevice: "", wantHostname: "192.0.2.10",
			wantTag: "", wantMessage: `Mar  1 12:34:56 router sshd: Accepted password`, wantPriority: false,
		},
		{
			name:         "bare non-syslog string",
			raw:          `not a syslog line`,
			wantFacility: 0, wantSeverity: 0, wantDevice: "", wantHostname: "192.0.2.10",
			wantTag: "", wantMessage: `not a syslog line`, wantPriority: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseSyslog(tc.raw, "192.0.2.10")
			if got.Raw != tc.raw {
				t.Errorf("Raw = %q, want %q", got.Raw, tc.raw)
			}
			if got.Facility != tc.wantFacility || got.Severity != tc.wantSeverity || got.priorityParsed != tc.wantPriority {
				t.Errorf("PRI fields = facility %d severity %d parsed %v, want %d %d %v", got.Facility, got.Severity, got.priorityParsed, tc.wantFacility, tc.wantSeverity, tc.wantPriority)
			}
			if got.DeviceTime != tc.wantDevice || got.Hostname != tc.wantHostname || got.Tag != tc.wantTag || got.Message != tc.wantMessage {
				t.Errorf("fields = device %q host %q tag %q message %q, want device %q host %q tag %q message %q", got.DeviceTime, got.Hostname, got.Tag, got.Message, tc.wantDevice, tc.wantHostname, tc.wantTag, tc.wantMessage)
			}
			if got.structuredData != tc.wantSD {
				t.Errorf("structured data = %q, want %q", got.structuredData, tc.wantSD)
			}
		})
	}
}

func TestReceiverDatagramsAndTruncation(t *testing.T) {
	receiver := NewReceiver(20)
	if err := receiver.Start(0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = receiver.Close() })

	addr := receiverUDPAddr(t, receiver)
	sender, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sender.Close() })
	for _, message := range []string{"<134>Mar  1 00:00:01 router link: one", "<134>Mar  1 00:00:02 router link: two", "<134>Mar  1 00:00:03 router link: three"} {
		if _, err := sender.Write([]byte(message)); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, time.Second, func() bool { return len(receiver.Entries()) == 3 })
	entries := receiver.Entries()
	for i, entry := range entries {
		if entry.SourceIP != "127.0.0.1" {
			t.Errorf("entry %d SourceIP = %q", i, entry.SourceIP)
		}
		if i > 0 && entry.Received.Before(entries[i-1].Received) {
			t.Errorf("Received timestamps are not monotonic: %v before %v", entry.Received, entries[i-1].Received)
		}
	}

	exact := bytes.Repeat([]byte("e"), maxDatagramSize)
	oversized := bytes.Repeat([]byte("o"), maxDatagramSize+1)
	if _, err := sender.Write(exact); err != nil {
		t.Fatal(err)
	}
	if _, err := sender.Write(oversized); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return len(receiver.Entries()) == 5 })
	entries = receiver.Entries()
	if entries[3].truncated {
		t.Error("exactly-8192-byte datagram was marked truncated")
	}
	if !entries[4].truncated {
		t.Error("oversized datagram was not marked truncated")
	}
	if len(entries[3].Raw) != maxDatagramSize || len(entries[4].Raw) != maxDatagramSize {
		t.Errorf("stored raw lengths = %d and %d, want both %d", len(entries[3].Raw), len(entries[4].Raw), maxDatagramSize)
	}
}

func TestReceiverRingCap(t *testing.T) {
	const maxEntries = 4
	receiver := NewReceiver(maxEntries)
	if err := receiver.Start(0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = receiver.Close() })
	addr := receiverUDPAddr(t, receiver)
	sender, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sender.Close() })
	for i := 0; i < maxEntries+10; i++ {
		message := "<134>Mar  1 00:00:01 router cap: entry-" + strconv.Itoa(i)
		if _, err := sender.Write([]byte(message)); err != nil {
			t.Fatal(err)
		}
		want := i + 1
		if want > maxEntries {
			want = maxEntries
		}
		waitFor(t, time.Second, func() bool {
			entries := receiver.Entries()
			return len(entries) >= want && strings.Contains(entries[len(entries)-1].Message, "entry-"+strconv.Itoa(i))
		})
	}
	entries := receiver.Entries()
	if len(entries) != maxEntries {
		t.Fatalf("ring length = %d, want %d", len(entries), maxEntries)
	}
	if !strings.Contains(entries[0].Message, "entry-10") {
		t.Errorf("oldest retained message = %q, want entry-10", entries[0].Message)
	}
}

func TestFilteredLogFragment(t *testing.T) {
	app := NewApp(NewStore(""))
	app.receiver.ring.Add(parseSyslog(`<131>Mar  1 12:00:00 router link: LINK down`, "10.0.0.1"))
	app.receiver.ring.Add(parseSyslog(`<134>Mar  1 12:00:01 router link: LINK informational`, "10.0.0.1"))
	app.receiver.ring.Add(parseSyslog(`<165>Mar  1 12:00:02 router ssh: Accepted login`, "10.0.0.1"))

	request := httptest.NewRequest(http.MethodGet, "/frag/log?q=link&sev=4", nil)
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("filtered response status = %d", response.Code)
	}
	body := response.Body.String()
	if !strings.Contains(body, "LINK down") || strings.Contains(body, "LINK informational") || strings.Contains(body, "Accepted login") {
		t.Fatalf("filtered body = %q", body)
	}

	request = httptest.NewRequest(http.MethodGet, "/frag/log?q=", nil)
	response = httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)
	body = response.Body.String()
	for _, message := range []string{"LINK down", "LINK informational", "Accepted login"} {
		if !strings.Contains(body, message) {
			t.Errorf("empty-q response omitted %q", message)
		}
	}
}

func TestSettingsSaveRebindsListenerAndPreservesOldOnFailure(t *testing.T) {
	app := NewApp(NewStore(""))
	if err := app.receiver.Start(0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.receiver.Close() })
	oldPort := receiverPort(t, app.receiver)

	blocker, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	blockedPort := blocker.LocalAddr().(*net.UDPAddr).Port
	request := httptest.NewRequest(http.MethodPost, "/settings/save", strings.NewReader("port="+strconv.Itoa(blockedPort)+"&max_entries=10"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)
	_ = blocker.Close()
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "UDP listener was not restarted") {
		t.Fatalf("failed rebind response = %d %q", response.Code, response.Body.String())
	}
	if got := receiverPort(t, app.receiver); got != oldPort {
		t.Fatalf("listener moved after failed bind: got %d, want %d", got, oldPort)
	}

	newPort := freeUDPPort(t)
	request = httptest.NewRequest(http.MethodPost, "/settings/save", strings.NewReader("port="+strconv.Itoa(newPort)+"&max_entries=10"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "UDP listener was not restarted") {
		t.Fatalf("successful rebind response = %d %q", response.Code, response.Body.String())
	}
	if got := receiverPort(t, app.receiver); got != newPort {
		t.Fatalf("listener port = %d, want %d", got, newPort)
	}

	sender, err := net.DialUDP("udp", nil, receiverUDPAddr(t, app.receiver))
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	if _, err := sender.Write([]byte("<134>Mar  1 00:00:01 router settings: rebound")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return len(app.receiver.Entries()) == 1 })
}

func TestHealthzOverUnixSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "syslog.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("Unix sockets unavailable: %v", err)
	}
	server := &http.Server{Handler: NewApp(NewStore("")).routes()}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close(); _ = listener.Close() })
	var conn net.Conn
	for i := 0; i < 20; i++ {
		conn, err = net.DialTimeout("unix", path, time.Second)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	req := httptest.NewRequest(http.MethodGet, "http://unix/healthz", nil)
	if err := req.Write(conn); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "ok") {
		t.Fatalf("health response = %d %q", resp.StatusCode, body)
	}
}

func TestStoreAtomicSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "options.json")
	store := NewStore(path)
	want := Config{ListenPort: 514, MaxEntries: 12}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	loaded := NewStore(path)
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	if got := loaded.Snapshot(); got != want {
		t.Fatalf("loaded config = %+v, want %+v", got, want)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary options file remains: %v", err)
	}
}

func TestBuildRootfsSyslogStagingShape(t *testing.T) {
	guiDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Clean(filepath.Join(guiDir, "..", "..", "..", "..", "..", ".."))
	script, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "build-rootfs.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(script), "for pack in aaa webserver httpclient syslog netsvc pc;"); got != 3 {
		t.Fatalf("syslog build-rootfs loops = %d, want 3", got)
	}

	var manifest struct {
		GUI struct {
			Bin string `json:"bin"`
		} `json:"gui"`
	}
	manifestBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "files", "tools", "packs", "syslog", "pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.GUI.Bin != "syslog-gui" {
		t.Fatalf("gui.bin = %q, want syslog-gui", manifest.GUI.Bin)
	}

	stage := filepath.Join(t.TempDir(), "packs", "syslog")
	if err := os.MkdirAll(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "pack.json"), manifestBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(stage, manifest.GUI.Bin)
	cmd := exec.Command("go", "build", "-trimpath", "-o", binaryPath, ".")
	cmd.Dir = guiDir
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("linux staging build: %v\n%s", err, output)
	}
	if err := os.Chmod(binaryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o755 {
		t.Fatalf("staged mode = %o, want 755", info.Mode().Perm())
	}
	binaryBytes, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(binaryBytes) < 20 || !bytes.Equal(binaryBytes[:4], []byte{0x7f, 'E', 'L', 'F'}) || binaryBytes[4] != 2 || binary.LittleEndian.Uint16(binaryBytes[18:20]) != 0x3e {
		t.Fatal("staged binary is not ELF linux/amd64")
	}
}

// receiverUDPAddr returns a deterministic IPv4 loopback destination for
// dialing the receiver in tests. The receiver always binds net.IPv4zero
// (syslog.go's replaceListener), so its LocalAddr().String() is literally
// "0.0.0.0:<port>" — re-resolving that string via net.ResolveUDPAddr on the
// ambiguous "udp" network lets Go's address-family heuristic pick IPv6
// (favoriteAddrFamily in the stdlib prefers ::1 for an unspecified
// destination IP on some hosts), which then connects to a loopback address
// the IPv4-only listener never sees traffic arrive from as 127.0.0.1 — this
// is host-dependent (confirmed diverging between a Windows dev machine and
// an Ubuntu 24.04 builder) rather than a flake. Force IPv4 explicitly to
// match what the listener actually binds.
func receiverUDPAddr(t *testing.T, receiver *Receiver) *net.UDPAddr {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", receiver.Addr())
	if err != nil {
		t.Fatal(err)
	}
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: addr.Port}
}

func receiverPort(t *testing.T, receiver *Receiver) int {
	t.Helper()
	return receiverUDPAddr(t, receiver).Port
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	port := listener.LocalAddr().(*net.UDPAddr).Port
	_ = listener.Close()
	return port
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition did not become true before timeout")
}
