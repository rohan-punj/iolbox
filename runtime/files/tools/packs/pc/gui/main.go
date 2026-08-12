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
	guiSocket := os.Getenv("IOLBOX_TOOL_SOCK")
	optionsPath := os.Getenv("IOLBOX_TOOL_OPTIONS")
	cliSocket := os.Getenv("IOLBOX_PC_CLI_SOCK")
	if guiSocket == "" || optionsPath == "" || cliSocket == "" {
		fail("IOLBOX_TOOL_SOCK, IOLBOX_TOOL_OPTIONS, and IOLBOX_PC_CLI_SOCK are required")
	}
	store := NewStore(optionsPath)
	if err := store.Load(); err != nil {
		fail("load state: %v", err)
	}
	app := NewApp(store)
	if err := os.MkdirAll(filepath.Dir(guiSocket), 0o700); err != nil {
		fail("create socket parent: %v", err)
	}
	if err := os.Remove(cliSocket); err != nil && !os.IsNotExist(err) {
		fail("remove stale CLI socket: %v", err)
	}
	cli, err := net.Listen("unix", cliSocket)
	if err != nil {
		fail("listen CLI socket: %v", err)
	}
	defer cli.Close()
	if err := os.Chmod(cliSocket, 0o600); err != nil {
		fail("chmod CLI socket: %v", err)
	}
	if err := os.Remove(guiSocket); err != nil && !os.IsNotExist(err) {
		fail("remove stale GUI socket: %v", err)
	}
	gui, err := net.Listen("unix", guiSocket)
	if err != nil {
		fail("listen GUI socket: %v", err)
	}
	defer gui.Close()
	if err := os.Chmod(guiSocket, 0o600); err != nil {
		fail("chmod GUI socket: %v", err)
	}
	go func() {
		for {
			conn, err := cli.Accept()
			if err != nil {
				return
			}
			go handleCLIConnection(conn, app)
		}
	}()
	if err := (&http.Server{Handler: app.routes()}).Serve(gui); err != nil && !strings.Contains(err.Error(), "closed network connection") {
		fail("serve GUI: %v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "pc-gui: "+format+"\n", args...)
	log.Print("fatal")
	os.Exit(1)
}
