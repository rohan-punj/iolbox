package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// freePort returns a port that is currently unused (listener opened then
// closed immediately) so a later listener can bind to the same number.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func TestWaitForGUI_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	host, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ok := waitForGUI(ctx, host, port, 2*time.Second, nil)
	if !ok {
		t.Fatal("expected waitForGUI to return true when the server responds 200")
	}
}

func TestWaitForGUI_PollsThroughInitialFailure(t *testing.T) {
	port := freePort(t)

	var srv *httptest.Server
	// Bring the listener up shortly after the probe starts, on the same port
	// that was just confirmed free — simulates the guest GUI coming up after
	// the hostfwd/TCP side is already reachable.
	go func() {
		time.Sleep(700 * time.Millisecond)
		l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			return
		}
		srv = &httptest.Server{Listener: l, Config: &http.Server{Handler: http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})}}
		srv.Start()
	}()
	defer func() {
		if srv != nil {
			srv.Close()
		}
	}()

	start := time.Now()
	ok := waitForGUI(context.Background(), "127.0.0.1", port, 5*time.Second, nil)
	elapsed := time.Since(start)

	if !ok {
		t.Fatal("expected waitForGUI to eventually return true once the server comes up")
	}
	// Must not have returned instantly — it should have polled past the
	// initial connection-refused period before the listener came up.
	if elapsed < 600*time.Millisecond {
		t.Errorf("expected waitForGUI to poll for a while before succeeding, returned after %s", elapsed)
	}
}

func TestWaitForGUI_ProcDoneAborts(t *testing.T) {
	port := freePort(t) // nothing listens here

	procDone := make(chan struct{})
	go func() {
		time.Sleep(200 * time.Millisecond)
		close(procDone)
	}()

	start := time.Now()
	ok := waitForGUI(context.Background(), "127.0.0.1", port, 5*time.Second, procDone)
	elapsed := time.Since(start)

	if ok {
		t.Fatal("expected waitForGUI to return false when procDone closes before the server comes up")
	}
	if elapsed > 2*time.Second {
		t.Errorf("expected waitForGUI to abort quickly after procDone closed, took %s", elapsed)
	}
}

func TestWaitForGUI_CtxCancel(t *testing.T) {
	port := freePort(t) // nothing listens here

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	ok := waitForGUI(ctx, "127.0.0.1", port, 5*time.Second, nil)
	elapsed := time.Since(start)

	if ok {
		t.Fatal("expected waitForGUI to return false when ctx is cancelled before the server comes up")
	}
	if elapsed > 2*time.Second {
		t.Errorf("expected waitForGUI to abort quickly after ctx cancel, took %s", elapsed)
	}
}

func TestWaitForGUI_Timeout(t *testing.T) {
	port := freePort(t) // nothing listens here

	start := time.Now()
	ok := waitForGUI(context.Background(), "127.0.0.1", port, 1500*time.Millisecond, nil)
	elapsed := time.Since(start)

	if ok {
		t.Fatal("expected waitForGUI to return false on timeout")
	}
	if elapsed > 4*time.Second {
		t.Errorf("expected waitForGUI to respect the timeout budget, took %s", elapsed)
	}
}
