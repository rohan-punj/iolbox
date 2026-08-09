package main

import (
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func waitForAddr(t *testing.T, web *WebService) string {
	t.Helper()
	for i := 0; i < 100; i++ {
		if addr := web.Addr(); addr != "" {
			return addr
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("web server did not bind")
	return ""
}

func portForTest(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func getBody(t *testing.T, port int) (int, string, error) {
	t.Helper()
	response, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/")
	if err != nil {
		return 0, "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	return response.StatusCode, string(body), err
}

func TestWebServeAndAccessLog(t *testing.T) {
	web := NewWeb(Config{ListenPort: 0, IndexHTML: "<strong>configured page</strong>", ExtraPaths: map[string]string{}})
	serveDone := make(chan error, 1)
	go func() { serveDone <- web.Serve() }()
	addr := waitForAddr(t, web)
	port := addr[strings.LastIndex(addr, ":")+1:]
	response, err := http.Get("http://127.0.0.1:" + port + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || string(body) != "<strong>configured page</strong>" {
		t.Fatalf("response = %d %q", response.StatusCode, body)
	}
	if logs := web.Logs(); len(logs) != 1 || logs[0].Path != "/" || logs[0].Status != http.StatusOK {
		t.Fatalf("access log = %+v", logs)
	}
	_ = web.Close()
	if err := <-serveDone; err != nil {
		t.Fatal(err)
	}
}

func TestWebPortChangeRestart(t *testing.T) {
	first := portForTest(t)
	second := portForTest(t)
	web := NewWeb(Config{ListenPort: first, IndexHTML: "first", ExtraPaths: map[string]string{}})
	go func() { _ = web.Serve() }()
	waitForAddr(t, web)
	if status, body, err := getBody(t, first); err != nil || status != http.StatusOK || body != "first" {
		t.Fatalf("first listener = %d %q (%v)", status, body, err)
	}
	web.mu.Lock()
	web.cfg.IndexHTML = "second"
	web.mu.Unlock()
	if err := web.Restart(second); err != nil {
		t.Fatal(err)
	}
	if status, body, err := getBody(t, second); err != nil || status != http.StatusOK || body != "second" {
		t.Fatalf("second listener = %d %q (%v)", status, body, err)
	}
	if _, _, err := getBody(t, first); err == nil {
		t.Fatal("old port still accepted a request after restart")
	}
	_ = web.Close()
}

func TestWebRejectsPrivilegedPort(t *testing.T) {
	web := NewWeb(Config{ListenPort: 80, IndexHTML: "", ExtraPaths: map[string]string{}})
	if err := web.Restart(80); err == nil {
		t.Fatal("expected privileged port refusal")
	}
}
