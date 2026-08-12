//go:build linux

package server

import (
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestPullPCStateOverUnixHTTP(t *testing.T) {
	path := t.TempDir() + "/gui.sock"
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method", 405)
			return
		}
		fmt.Fprint(w, `{"pc":{"dhcp":true,"savedCommands":["show ip"]},"rev":3}`)
	}))
	state, err := pullPCStateSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	if !state.DHCP || len(state.SavedCommands) != 1 {
		t.Fatalf("state = %#v", state)
	}
	_ = time.Second
}
