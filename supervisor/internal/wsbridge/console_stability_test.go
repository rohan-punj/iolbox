package wsbridge

import (
	"bufio"
	"context"
	"github.com/rohanpunj/iolbox/supervisor/internal/node"
	"github.com/rohanpunj/iolbox/supervisor/internal/ws"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

type observedConn struct {
	net.Conn
	started chan struct{}
	mu      sync.Mutex
	writes  int
}

func (c *observedConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.writes++
	if c.writes == 2 {
		close(c.started)
	}
	c.mu.Unlock()
	return c.Conn.Write(p)
}

type pipeHijacker struct{ net.Conn }

func (h *pipeHijacker) Header() http.Header         { return make(http.Header) }
func (h *pipeHijacker) Write(p []byte) (int, error) { return h.Conn.Write(p) }
func (h *pipeHijacker) WriteHeader(int)             {}
func (h *pipeHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return h.Conn, bufio.NewReadWriter(bufio.NewReader(h.Conn), bufio.NewWriter(h.Conn)), nil
}

func stalledConsoleWS(t *testing.T) (*ws.Conn, net.Conn, <-chan struct{}) {
	t.Helper()
	server, client := net.Pipe()
	observed := &observedConn{Conn: server, started: make(chan struct{})}
	go func() {
		reader := bufio.NewReader(client)
		for {
			line, err := reader.ReadString('\n')
			if err != nil || line == "\r\n" {
				return
			}
		}
	}()
	req, _ := http.NewRequest("GET", "http://localhost/console/1", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Sec-WebSocket-Version", "13")
	conn, err := ws.Accept(&pipeHijacker{observed}, req)
	if err != nil {
		t.Fatal(err)
	}
	return conn, client, observed.started
}

func TestConsoleSubscriptionCancellationInterruptsBlockedWrite(t *testing.T) {
	for _, reason := range []string{"unsubscribe", "eviction", "context"} {
		t.Run(reason, func(t *testing.T) {
			wsConn, client, started := stalledConsoleWS(t)
			defer client.Close()
			defer wsConn.Close()
			outR, outW := io.Pipe()
			inR, inW := io.Pipe()
			defer outR.Close()
			defer outW.Close()
			defer inR.Close()
			defer inW.Close()
			sub := node.NewSubscriptionForTest(&pipePty{r: outR, w: inW}, "")
			defer sub.Unsubscribe()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() { bridgeConsoleSub(ctx, wsConn, sub); close(done) }()
			if _, err := outW.Write([]byte("first")); err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("write did not block")
			}
			switch reason {
			case "context":
				cancel()
			case "unsubscribe":
				sub.Unsubscribe()
			case "eviction":
				for i := 0; i < 70; i++ {
					if _, err := outW.Write([]byte("overflow")); err != nil {
						t.Fatal(err)
					}
				}
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("blocked write outlived cancellation")
			}
			select {
			case <-sub.Done:
			case <-time.After(time.Second):
				t.Fatal("subscriber retained")
			}
		})
	}
}
