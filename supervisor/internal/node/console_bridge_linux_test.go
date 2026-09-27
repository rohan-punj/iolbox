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
		var received []byte
		// TCP may split negotiation, title, and console output across reads.
		for !bytes.Contains(received, []byte("PC> ready")) {
			n, err := client.Read(buf)
			received = append(received, buf[:n]...)
			if bytes.Contains(received, []byte("PC> ready")) {
				break
			}
			if err != nil {
				t.Fatalf("client %d: %v; received %q", i, err, received)
			}
		}
	}
	_ = cliPeer.Close()
}
