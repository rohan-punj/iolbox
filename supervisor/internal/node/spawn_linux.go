//go:build linux

package node

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"

	"github.com/creack/pty"

	"github.com/rohanpunj/iolab/supervisor/internal/telnet"
)

// Process is a spawned node process with its lifecycle state, its controlling
// pty, and the per-node telnet console server bridging that pty to a TCP port.
type Process struct {
	Spec    Spec
	Machine *Machine

	mu   sync.Mutex
	cmd  *exec.Cmd
	ptmx *os.File     // pty master; the node's console I/O flows through this
	ln   net.Listener // telnet console listener on Spec.ConsolePort
	done chan struct{}
}

// Spawn launches the node described by spec.
//
// Console model (P0-confirmed): real IOL uses stdin/stdout on a controlling pty
// for its console and opens NO TCP port. So Spawn allocates a pty, runs the
// process attached to the pty *slave* as its controlling terminal (creack/pty's
// Start sets setsid + setctty), and keeps the pty *master* for the node's whole
// lifetime. A per-node telnet server on spec.ConsolePort (loopback) accepts TCP
// clients and pumps bytes between the client and the pty master. Because the
// master persists independently of any console connection, sequential clients
// can disconnect and reconnect without disturbing IOL — exactly what the manual
// P0 test proved with socat's PTY,setsid,ctty bridge.
//
// IOL reads NETMAP, iourc, and its NVRAM file from its cwd (spec.WorkDir = the
// shared lab dir for IOL); the caller has already written those (prepareLabDir).
//
// State: stopped->starting immediately; a background waiter moves it to crashed
// on unexpected exit. Once the pty and console listener are up the node is
// reported running (the process is attached to the pty and any output flows to
// connected clients); the caller emits node.console + node.state.
func Spawn(spec Spec, m *Machine) (*Process, error) {
	var argv []string
	var env []string
	switch spec.Kind {
	case "iol":
		argv = spec.IOLArgv()
		env = append(os.Environ(), spec.Environ()...)
	case "vpcs":
		var err error
		argv, err = spec.VPCSArgv(fmt.Sprintf("pc%d", spec.NodeID))
		if err != nil {
			return nil, err
		}
		env = os.Environ()
	default:
		return nil, fmt.Errorf("node: unknown kind %q", spec.Kind)
	}

	if err := os.MkdirAll(spec.WorkDir, 0o755); err != nil {
		return nil, fmt.Errorf("node %d: workdir: %w", spec.NodeID, err)
	}

	// Bind the telnet console listener BEFORE spawning so a client that dials
	// immediately after node.console never races the accept loop.
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(spec.ConsolePort))
	if err != nil {
		return nil, fmt.Errorf("node %d: console listen :%d: %w", spec.NodeID, spec.ConsolePort, err)
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = spec.WorkDir
	cmd.Env = env

	if !m.To(StateStarting) {
		_ = ln.Close()
		return nil, fmt.Errorf("node %d: not in a startable state", spec.NodeID)
	}

	// pty.Start allocates a pty, wires cmd's stdin/stdout/stderr to the slave,
	// and sets SysProcAttr{Setsid:true, Setctty:true} so the slave is the
	// process's controlling terminal — the IOL console.
	ptmx, err := pty.Start(cmd)
	if err != nil {
		m.To(StateCrashed)
		_ = ln.Close()
		return nil, fmt.Errorf("node %d: pty start: %w", spec.NodeID, err)
	}

	p := &Process{
		Spec:    spec,
		Machine: m,
		cmd:     cmd,
		ptmx:    ptmx,
		ln:      ln,
		done:    make(chan struct{}),
	}
	go p.serveConsole()
	go p.wait()
	return p, nil
}

// serveConsole accepts telnet clients on the console port and bridges each to
// the pty master. One active client at a time (a Cisco console is single-user);
// a new client preempts nothing on the pty — the master persists — and simply
// gets the live stream. The loop runs until the listener is closed (Stop).
func (p *Process) serveConsole() {
	for {
		conn, err := p.ln.Accept()
		if err != nil {
			return // listener closed on Stop
		}
		p.bridgeConsole(conn)
	}
}

// bridgeConsole pumps bytes between one telnet client and the pty master until
// the client disconnects (or the node exits). The pty master is NOT closed when
// the client leaves, so the next client reconnects to the same live console.
//
// Telnet: we present a minimal server. On connect we volunteer WILL ECHO + WILL
// SGA so a line-mode telnet/xterm client switches to character-at-a-time and
// lets the remote (IOL) own echo — matching a real Cisco console. Inbound IAC
// negotiation from the client is consumed (and answered) by telnet.Negotiator
// so it never leaks into the pty; the cleaned bytes are written to the pty.
// pty->client is raw (IOL emits no IAC of its own over a pty).
func (p *Process) bridgeConsole(conn net.Conn) {
	defer conn.Close()

	// Snapshot the master under lock; teardown nils p.ptmx, and closing the
	// captured *os.File unblocks these reads/writes with an error either way.
	p.mu.Lock()
	ptmx := p.ptmx
	p.mu.Unlock()
	if ptmx == nil {
		return // node already torn down
	}

	// Volunteer server-side echo + suppress-go-ahead so the client goes into
	// char mode. (A dumb raw client that ignores IAC still works: these bytes
	// are valid telnet commands it will simply discard.)
	_, _ = conn.Write([]byte{
		telnet.IAC, telnet.WILL, telnet.OptEcho,
		telnet.IAC, telnet.WILL, telnet.OptSGA,
	})

	var once sync.Once
	stop := make(chan struct{})
	closeOnce := func() { once.Do(func() { close(stop) }) }

	// pty -> client (raw). NOTE: if the client disconnects, this read stays
	// blocked until the pty next produces a byte (then conn.Write fails and it
	// exits) or the node stops (teardown closes the master). With one client at
	// a time this is benign; P0 may add a select/deadline if console churn under
	// load proves it worth the complexity.
	go func() {
		defer closeOnce()
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if _, werr := conn.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// client -> pty: strip/answer any IAC the client sends, forward clean bytes.
	go func() {
		defer closeOnce()
		neg := telnet.NewNegotiator()
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				clean := neg.Feed(buf[:n])
				if reply := neg.Reply(); reply != nil {
					if _, werr := conn.Write(reply); werr != nil {
						return
					}
				}
				if len(clean) > 0 {
					if _, werr := ptmx.Write(clean); werr != nil {
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()

	select {
	case <-stop:
	case <-p.done:
	}
}

// wait reaps the process and updates state on exit, then tears down the console.
func (p *Process) wait() {
	err := p.cmd.Wait()
	// If we deliberately stopped it, state is already/soon Stopped; only mark
	// crashed when still running/starting.
	switch p.Machine.State() {
	case StateStopped:
		// expected shutdown
	default:
		if err != nil {
			p.Machine.To(StateCrashed)
		} else {
			p.Machine.To(StateStopped)
		}
	}
	p.teardown()
}

// teardown closes the pty master, the console listener, and signals bridges to
// unblock. Idempotent.
func (p *Process) teardown() {
	p.mu.Lock()
	ptmx, ln, done := p.ptmx, p.ln, p.done
	p.ptmx, p.ln = nil, nil
	p.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		default:
			close(done)
		}
	}
	if ptmx != nil {
		_ = ptmx.Close()
	}
	if ln != nil {
		_ = ln.Close()
	}
}

// PID returns the OS process id, or 0 if not started.
func (p *Process) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// Stop terminates the process and marks the node stopped, then tears down the
// pty + console listener.
func (p *Process) Stop() error {
	p.mu.Lock()
	cmd := p.cmd
	p.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		p.teardown()
		return nil
	}
	p.Machine.To(StateStopped)
	err := cmd.Process.Kill()
	p.teardown()
	return err
}
