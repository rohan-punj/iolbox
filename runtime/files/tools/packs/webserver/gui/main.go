package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	socketPath := os.Getenv("IOLBOX_TOOL_SOCK")
	optionsPath := os.Getenv("IOLBOX_TOOL_OPTIONS")
	if socketPath == "" || optionsPath == "" {
		fail("IOLBOX_TOOL_SOCK and IOLBOX_TOOL_OPTIONS are required")
	}
	store := NewStore(optionsPath)
	if err := store.Load(); err != nil {
		fail("load options: %v", err)
	}
	app := NewApp(store)
	go func() {
		if err := app.web.Serve(); err != nil {
			log.Printf("webserver: listener stopped: %v", err)
		}
	}()
	if !hasLabIface() {
		log.Printf("webserver: eth1 is not wired yet; the service will be reachable after the node gets a lab address")
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		fail("create socket parent: %v", err)
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		fail("remove stale socket: %v", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		fail("listen on %s: %v", socketPath, err)
	}
	defer listener.Close()
	if err := os.Chmod(socketPath, 0o600); err != nil {
		fail("chmod socket: %v", err)
	}
	defer app.web.Close()
	if err := (&http.Server{Handler: app.routes()}).Serve(listener); err != nil && !strings.Contains(err.Error(), "closed network connection") {
		fail("serve GUI: %v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "webserver-gui: "+format+"\n", args...)
	os.Exit(1)
}
