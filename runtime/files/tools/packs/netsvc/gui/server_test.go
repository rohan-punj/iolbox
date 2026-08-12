package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBindFailureDegradesAllFourServicesAndHealthzOverUnix(t *testing.T) {
	occupied := make([]*net.UDPConn, 0, 4)
	ports := make([]int, 0, 4)
	for i := 0; i < 4; i++ {
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
		if err != nil {
			t.Fatal(err)
		}
		occupied = append(occupied, conn)
		ports = append(ports, conn.LocalAddr().(*net.UDPAddr).Port)
	}
	defer func() {
		for _, conn := range occupied {
			_ = conn.Close()
		}
	}()
	store := NewStore("")
	cfg := store.Snapshot()
	cfg.Ports = PortsConfig{DNS: ports[0], DHCP: ports[1], NTP: ports[2], TFTP: ports[3]}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	app := NewApp(store, filepath.Join(t.TempDir(), "options.json"))
	app.StartServices()
	defer app.Close()
	for _, name := range []string{"dns", "dhcp", "ntp", "tftp"} {
		if got := app.bindings[name].error(); got == "" {
			t.Fatalf("%s bind failure was not recorded", name)
		}
	}
	dashboard := httptestRequest(t, app.routes(), "GET", "/")
	if !strings.Contains(dashboard, "Bind failed:") {
		t.Fatalf("dashboard omitted bind failure: %s", dashboard)
	}
	if !strings.Contains(dashboard, "DHCP") || !strings.Contains(dashboard, "DNS") || !strings.Contains(dashboard, "NTP") || !strings.Contains(dashboard, "TFTP") {
		t.Fatalf("dashboard omitted a service tile")
	}

	if runtime.GOOS != "linux" {
		t.Skip("AF_UNIX HTTP transport test runs in the Linux pack environment")
	}
	socketPath := filepath.Join(t.TempDir(), "health.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: app.routes()}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Shutdown(context.Background()) }()
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return net.Dial("unix", socketPath) }}}
	deadline := time.Now().Add(time.Second)
	var response *http.Response
	for time.Now().Before(deadline) {
		response, err = client.Get("http://unix/healthz")
		if err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || string(body) != "ok\n" {
		t.Fatalf("healthz status=%d body=%q", response.StatusCode, body)
	}
}

func httptestRequest(t *testing.T, handler http.Handler, method, path string) string {
	t.Helper()
	req, err := http.NewRequest(method, "http://netsvc"+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := &responseRecorder{header: make(http.Header)}
	handler.ServeHTTP(rr, req)
	return rr.body.String()
}

type responseRecorder struct {
	header http.Header
	status int
	body   strings.Builder
}

func (r *responseRecorder) Header() http.Header    { return r.header }
func (r *responseRecorder) WriteHeader(status int) { r.status = status }
func (r *responseRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(b)
}
