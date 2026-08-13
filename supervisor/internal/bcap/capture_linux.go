//go:build linux

package bcap

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/relay"
	"github.com/rohanpunj/iolbox/supervisor/internal/tool"
)

// closeWaitTimeout bounds how long Close() will block on the tcpdump
// process's exit. Found live (finding #13): Close() killed only the `sudo`
// wrapper (cmd.Process), never the tcpdump grandchild it execs — tcpdump
// survived as an orphan still holding the inherited stdout pipe open, so
// cmd.Wait() (which Go's os/exec blocks on until that pipe reaches EOF)
// hung forever. That hang happened while handleCaptureStop held the
// server's single serializedHandler mutex, wedging every other control-plane
// request behind it — including a fresh page reload's hello handshake,
// which is why the GUI showed "Connecting" forever with no way to recover
// short of a process restart. Setpgid below fixes the root cause (the whole
// sudo+tcpdump process group is killed together, not just sudo); this
// timeout is the self-heal backstop so a future variant of the same failure
// mode can never wedge the control plane again — it leaks the goroutine
// waiting on the process instead of blocking the caller.
const closeWaitTimeout = 3 * time.Second

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
	// New process group so Close() can kill sudo AND the tcpdump child it
	// execs in one signal (-pid) — sudo forks tcpdump rather than exec'ing
	// over itself, and without this the two share iolbox-supervisor's own
	// process group, so killing only cmd.Process (sudo) leaves tcpdump
	// running as an orphan. See closeWaitTimeout's doc comment.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		server.Close()
		return nil, fmt.Errorf("bcap: tcpdump stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// Start and register atomically: the subreaper must never see this direct
	// child as an unregistered orphan between fork+exec and registration.
	if err := tool.Registry.StartAndAdd(cmd.Start, func() int { return cmd.Process.Pid }); err != nil {
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
		// Negative pid signals the whole process group (sudo + tcpdump),
		// set up via Setpgid in Start — see its comment. Falls back to
		// killing just cmd.Process if the group signal itself errors (e.g.
		// the group already reaped), same as the pre-existing behavior.
		if err := syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL); err != nil {
			killErr = c.cmd.Process.Kill()
		}
	}

	waitDone := make(chan struct{})
	go func() {
		_ = c.cmd.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(closeWaitTimeout):
		log.Printf("bcap: tcpdump on %s did not exit within %s after kill — leaking the wait, not blocking the caller", c.bridge, closeWaitTimeout)
	}

	if c.cmd.Process != nil {
		tool.Registry.Remove(c.cmd.Process.Pid)
	}
	c.server.Close()
	return killErr
}
