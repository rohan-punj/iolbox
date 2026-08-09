package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	socketPath := os.Getenv("IOLBOX_TOOL_SOCK")
	if socketPath == "" || os.Getenv("IOLBOX_TOOL_OPTIONS") == "" {
		fail("IOLBOX_TOOL_SOCK and IOLBOX_TOOL_OPTIONS are required")
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
	if err := (&http.Server{Handler: NewApp().routes()}).Serve(listener); err != nil && !strings.Contains(err.Error(), "closed network connection") {
		fail("serve GUI: %v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "httpclient-gui: "+format+"\n", args...)
	os.Exit(1)
}
