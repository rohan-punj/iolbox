//go:build linux

package bcap

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/rohanpunj/iolab/supervisor/internal/relay"
)

// Capture runs tcpdump against a Linux bridge fabric link and re-serves its
// frames as a live pcapng TCP stream, while classifying and counting them.
type Capture struct {
	bridge string
	server *pcapngServer
	cmd    *exec.Cmd
	stderr *bytes.Buffer

	mu      sync.Mutex
	closed  bool
	frames  uint64
	bytes_  uint64
	protos  map[string]uint64
	doneErr error
}

// Start begins capturing bridgeName: it starts the pcapng TCP server on
// bind:port (bind "" defaults to loopback, port 0 picks an ephemeral port),
// then launches `sudo -n tcpdump -i <bridgeName> -w - -U -s 0 -n` and streams
// its stdout through parsePcapStream. Each frame is broadcast to connected
// pcapng clients and classified/counted. If tcpdump fails to start, the
// pcapng server is closed and the error is returned.
func Start(bridgeName, bind string, port int) (*Capture, error) {
	server, err := newPcapngServer(bind, port)
	if err != nil {
		return nil, fmt.Errorf("bcap: start pcapng server: %w", err)
	}

	cmd := exec.Command("sudo", "-n", "tcpdump", "-i", bridgeName, "-w", "-", "-U", "-s", "0", "-n")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		server.Close()
		return nil, fmt.Errorf("bcap: tcpdump stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		server.Close()
		return nil, fmt.Errorf("bcap: start tcpdump on %s: %w", bridgeName, err)
	}

	c := &Capture{
		bridge: bridgeName,
		server: server,
		cmd:    cmd,
		stderr: &stderr,
		protos: make(map[string]uint64),
	}

	go c.run(stdout)

	return c, nil
}

func (c *Capture) run(stdout io.Reader) {
	err := parsePcapStream(stdout, c.onFrame)
	c.mu.Lock()
	c.doneErr = err
	c.mu.Unlock()
}

func (c *Capture) onFrame(frame []byte, tsMicros uint64) {
	c.server.Broadcast(frame, tsMicros)

	label, _ := relay.Classify(frame)

	c.mu.Lock()
	c.frames++
	c.bytes_ += uint64(len(frame))
	c.protos[label]++
	c.mu.Unlock()
}

// Port is the actual TCP port the pcapng server listens on.
func (c *Capture) Port() int { return c.server.Port() }

// Stats returns the running frame count, byte count, and a copy of the
// per-protocol frame counts (safe for the caller to keep or diff against a
// later call).
func (c *Capture) Stats() (frames, bytesN uint64, protos map[string]uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	protosCopy := make(map[string]uint64, len(c.protos))
	for k, v := range c.protos {
		protosCopy[k] = v
	}
	return c.frames, c.bytes_, protosCopy
}

// Close stops the tcpdump process and the pcapng server. Idempotent.
func (c *Capture) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	var killErr error
	if c.cmd.Process != nil {
		killErr = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
	c.server.Close()
	return killErr
}
