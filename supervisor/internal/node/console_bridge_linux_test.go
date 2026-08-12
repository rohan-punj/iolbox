//go:build linux

package node

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestPCConsoleBridgeBroadcastsToTwoClients(t *testing.T) {
	cliPeer, cliBridge := net.Pipe()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := NewConsoleBridge(cliBridge, "PC1", ln)
	defer p.Stop()
	clients := make([]net.Conn, 2)
	for i := range clients {
		clients[i], err = net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer clients[i].Close()
	}
	if _, err := cliPeer.Write([]byte("PC> ready\r\n")); err != nil {
		t.Fatal(err)
	}
	for i, client := range clients {
		_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 256)
		n, err := client.Read(buf)
		if err != nil {
			t.Fatalf("client %d: %v", i, err)
		}
		if !bytes.Contains(buf[:n], []byte("PC> ready")) {
			t.Fatalf("client %d got %q", i, buf[:n])
		}
	}
	_ = cliPeer.Close()
}
