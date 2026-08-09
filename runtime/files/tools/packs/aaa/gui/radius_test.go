package main

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRadiusAccessRequestAcceptRejectAndSecret(t *testing.T) {
	store := NewStore("")
	store.Set(Config{SharedSecret: "known-secret", Protocol: "radius", Users: []User{{Username: "alice", Password: "correct", Service: "login", PrivLvl: 15}}})
	server := NewRadiusServer(store)
	var auth [16]byte
	for i := range auth {
		auth[i] = byte(i + 1)
	}
	cases := []struct {
		name       string
		password   string
		secret     string
		wantResult byte
	}{
		{"valid user", "correct", "known-secret", radiusAccessAccept},
		{"bad password", "wrong", "known-secret", radiusAccessReject},
		{"wrong shared secret", "correct", "different-secret", radiusAccessReject},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := server.handlePacket(buildAccessRequest(7, auth, "alice", tc.password, tc.secret), "127.0.0.1:12345")
			if len(response) == 0 {
				t.Fatal("expected a RADIUS response")
			}
			packet, err := parseRadiusPacket(response)
			if err != nil {
				t.Fatal(err)
			}
			if packet.code != tc.wantResult {
				t.Fatalf("response code = %d, want %d", packet.code, tc.wantResult)
			}
		})
	}
}

func TestHealthzOverUnixSocket(t *testing.T) {
	for _, pathName := range []string{"/healthz"} {
		t.Run(pathName, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "aaa.sock")
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
			req := httptest.NewRequest(http.MethodGet, "http://unix"+pathName, nil)
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
		})
	}
}

func TestStoreAtomicSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "options.json")
	store := NewStore(path)
	want := Config{SharedSecret: "s", Protocol: "radius", Users: []User{{Username: "u", Password: "p"}}}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	loaded := NewStore(path)
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	if got := loaded.Snapshot(); got.SharedSecret != want.SharedSecret || len(got.Users) != 1 {
		t.Fatalf("loaded config = %+v", got)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary options file remains: %v", err)
	}
}

// TestStoreLoadBareEmptyObjectKeepsSharedSecretDefault is the regression test
// for a real bug found live: the supervisor writes a bare "{}" options.json
// for any node with no saved pack options yet (endpointOptionsPayload), which
// decodes to an all-zero-value Config and must not wipe SharedSecret's
// default the way an explicit empty-string save legitimately would leave it
// unset — TacacsServer.Serve refuses to start with no key at all, and the
// only reason this appeared in the shipped default was Load() blindly
// accepting the zero value.
func TestStoreLoadBareEmptyObjectKeepsSharedSecretDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "options.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	cfg := store.Snapshot()
	if cfg.SharedSecret == "" {
		t.Fatal("SharedSecret is empty after loading a bare {} options file")
	}
	if _, ok := tacacsKey(cfg); !ok {
		t.Fatal("tacacsKey resolved no key from a freshly-created node's default options file")
	}
}
