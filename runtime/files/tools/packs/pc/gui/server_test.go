package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStateSharingCLIToGUIAndBack(t *testing.T) {
	app := testApp(t)
	if got := dispatchLine(app, "ip 10.0.0.1/24 10.0.0.254"); !strings.Contains(got, "Address configured") {
		t.Fatalf("CLI address = %q", got)
	}
	if got := dispatchLine(app, "show ip"); !strings.Contains(got, "10.0.0.1/24") {
		t.Fatal(got)
	}
	r := httptest.NewRecorder()
	app.routes().ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/", nil))
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), "10.0.0.1") {
		t.Fatalf("dashboard = %d %s", r.Code, r.Body.String())
	}
	form := strings.NewReader("ip=192.0.2.10&prefix=24&gateway=192.0.2.1&dhcp=on&saved_commands=ping+192.0.2.1%0Ashow+ip")
	r = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/save", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	app.routes().ServeHTTP(r, req)
	if r.Code != http.StatusSeeOther {
		t.Fatalf("save status = %d", r.Code)
	}
	if !app.store.Snapshot().PC.DHCP {
		t.Fatal("GUI save did not update DHCP state")
	}
	if got := dispatchLine(app, "show ip"); !strings.Contains(got, "192.0.2.10/24") {
		t.Fatal(got)
	}
}

func TestStateEndpointGETOnly(t *testing.T) {
	app := testApp(t)
	r := httptest.NewRecorder()
	app.routes().ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/_iolbox/state", nil))
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"pc"`) {
		t.Fatalf("GET state = %d %s", r.Code, r.Body.String())
	}
	r = httptest.NewRecorder()
	app.routes().ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/_iolbox/state", nil))
	if r.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST state = %d", r.Code)
	}
	_, _ = io.Copy(io.Discard, r.Result().Body)
}

func TestStateEndpointOverUnixSocket(t *testing.T) {
	path := t.TempDir() + "/gui.sock"
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("AF_UNIX unavailable on this test host: %v", err)
	}
	app := testApp(t)
	go http.Serve(ln, app.routes())
	defer ln.Close()
	client := &http.Client{Transport: &http.Transport{DialContext: func(_ context.Context, _, _ string) (net.Conn, error) { return net.Dial("unix", path) }}}
	resp, err := client.Get("http://iolbox/_iolbox/state")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("state status = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"pc"`) || !strings.Contains(string(body), `"rev"`) {
		t.Fatalf("state body = %s", body)
	}
}
