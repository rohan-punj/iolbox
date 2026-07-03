//go:build linux

package node

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/rohanpunj/iolab/supervisor/internal/telnet"
)

// Process is a spawned node process with its lifecycle state.
//
// Two console models coexist (see Spawn):
//
//   - IOL: runs under a controlling pty (ptmx), and the supervisor binds a
//     per-node telnet server on Spec.ConsolePort (ln) that bridges clients to
//     the pty master.
//
//   - VPCS: is its OWN telnet server (it opens Spec.ConsolePort itself) and
//     DAEMONIZES (forks; the launched process exits immediately). There is no
//     pty and the supervisor does NOT bind ConsolePort. The launched process is
//     put in its own process group so Stop can kill the whole group (the
//     daemonized child included). ptmx and ln stay nil for VPCS.
type Process struct {
	Spec    Spec
	Machine *Machine

	mu   sync.Mutex
	cmd  *exec.Cmd
	ptmx *os.File     // pty master (IOL only); the node's console I/O flows through this
	ln   net.Listener // telnet console listener on Spec.ConsolePort (IOL only)
	pgid int          // process group id to kill on Stop (VPCS: the daemonized group)
	done chan struct{}
}

// Spawn launches the node described by spec, dispatching to the console model
// that node kind needs. IOL runs under a supervisor-owned pty + telnet bridge;
// VPCS is its own telnet server and daemonizes. See spawnIOL / spawnVPCS.
func Spawn(spec Spec, m *Machine) (*Process, error) {
	if err := os.MkdirAll(spec.WorkDir, 0o755); err != nil {
		return nil, fmt.Errorf("node %d: workdir: %w", spec.NodeID, err)
	}
	switch spec.Kind {
	case "iol":
		return spawnIOL(spec, m)
	case "vpcs":
		return spawnVPCS(spec, m)
	default:
		return nil, fmt.Errorf("node: unknown kind %q", spec.Kind)
	}
}

// spawnIOL launches an IOL node.
//
// Console model (P0-confirmed): real IOL uses stdin/stdout on a controlling pty
// for its console and opens NO TCP port. So spawnIOL allocates a pty, runs the
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
func spawnIOL(spec Spec, m *Machine) (*Process, error) {
	argv := spec.IOLArgv()
	env := append(os.Environ(), spec.Environ()...)

	// Bind the telnet console listener BEFORE spawning so a client that dials
	// immediately after node.console never races the accept loop. Bind host
	// is configurable (Spec.ConsoleBind) so native telnet from the GUI host
	// can reach the console; the wsbridge always dials via loopback either way.
	bind := spec.ConsoleBind
	if bind == "" {
		bind = "127.0.0.1"
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(spec.ConsolePort)))
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

// vpcsConsoleReadyTimeout bounds how long spawnVPCS waits for the daemonized
// vpcs to open its telnet console port before giving up.
const vpcsConsoleReadyTimeout = 5 * time.Second

// spawnVPCS launches a VPCS node (bundled vpcs 0.8.3).
//
// VPCS is a DIFFERENT process model from IOL (confirmed on the VM):
//   - It is its OWN telnet console server: `-p <ConsolePort>` makes vpcs open and
//     listen on that TCP port itself and serve a `VPCS>` prompt. So the
//     supervisor must NOT bind ConsolePort and must NOT run vpcs under a pty.
//   - It DAEMONIZES: the launched process forks and the parent exits immediately
//     (~success), leaving the real vpcs running detached. So reaping the launcher
//     process must NOT mark the node crashed/stopped.
//
// spawnVPCS therefore: starts vpcs in its OWN process group (so Stop can kill the
// whole group — the daemonized child that outlives the launcher included), waits
// for ConsolePort to accept TCP (proof vpcs is up), then reports running. The WS
// /console/{nodeId} bridge and external telnet dial ConsolePort directly, which
// vpcs serves — nothing else in the console path changes.
func spawnVPCS(spec Spec, m *Machine) (*Process, error) {
	argv, err := spec.VPCSArgv(fmt.Sprintf("pc%d", spec.NodeID))
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = spec.WorkDir
	cmd.Env = os.Environ()
	// Own process group so Stop can signal the whole group; the daemonized vpcs
	// child inherits the group and is killed with it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if !m.To(StateStarting) {
		return nil, fmt.Errorf("node %d: not in a startable state", spec.NodeID)
	}
	if err := cmd.Start(); err != nil {
		m.To(StateCrashed)
		return nil, fmt.Errorf("node %d: vpcs start: %w", spec.NodeID, err)
	}
	pgid := cmd.Process.Pid // Setpgid makes the child its own group leader (pgid == pid)

	p := &Process{
		Spec:    spec,
		Machine: m,
		cmd:     cmd,
		pgid:    pgid,
		done:    make(chan struct{}),
	}

	// Reap the launcher: vpcs daemonizes, so this returns almost immediately with
	// success. Do NOT mark the node stopped/crashed on that reap — the real vpcs
	// lives on in the process group. Readiness is decided by the console port.
	go func() { _ = cmd.Wait() }()

	// Wait for vpcs to open its telnet console port, then report running. If it
	// never comes up, the process group is killed and the node is marked crashed.
	if !waitConsoleReady(spec.ConsolePort, vpcsConsoleReadyTimeout) {
		_ = killVPCS(pgid, spec.ConsolePort)
		m.To(StateCrashed)
		return nil, fmt.Errorf("node %d: vpcs console :%d not ready within %s", spec.NodeID, spec.ConsolePort, vpcsConsoleReadyTimeout)
	}
	return p, nil
}

// waitConsoleReady polls a loopback TCP port until it accepts a connection or
// the timeout elapses. Used to detect that a daemonizing node (VPCS) has opened
// its own telnet console server.
func waitConsoleReady(port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	addr := "127.0.0.1:" + strconv.Itoa(port)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// killGroup sends SIGKILL to an entire process group (negative pid). Used to
// reap a daemonized VPCS and any children it forked.
func killGroup(pgid int) error {
	if pgid <= 0 {
		return nil
	}
	return syscall.Kill(-pgid, syscall.SIGKILL)
}

// pidListeningOn returns the pid of a process LISTENing on the given loopback
// TCP port, or 0 if none is found (or on any parse error — best-effort). It maps
// the port to its socket inode via /proc/net/tcp, then scans /proc/<pid>/fd for
// a symlink to socket:[inode]. Used only as a fallback to kill a double-fork
// daemonized vpcs that escaped its process group via setsid().
func pidListeningOn(port int) int {
	inode := listenSocketInode(port)
	if inode == "" {
		return 0
	}
	target := "socket:[" + inode + "]"
	procs, err := filepath.Glob("/proc/[0-9]*/fd/*")
	if err != nil {
		return 0
	}
	for _, fd := range procs {
		if link, lerr := os.Readlink(fd); lerr == nil && link == target {
			// /proc/<pid>/fd/<n> -> extract <pid>.
			rest := strings.TrimPrefix(fd, "/proc/")
			if slash := strings.IndexByte(rest, '/'); slash > 0 {
				if pid, perr := strconv.Atoi(rest[:slash]); perr == nil {
					return pid
				}
			}
		}
	}
	return 0
}

// listenSocketInode scans /proc/net/tcp (+tcp6) for a socket in the LISTEN state
// (st == 0x0A) whose local port equals port, returning its inode as a decimal
// string, or "" if not found. The local address column is "HEXIP:HEXPORT".
func listenSocketInode(port int) string {
	want := strings.ToUpper(strconv.FormatInt(int64(port), 16))
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Scan() // header
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			// fields: sl local_address rem_address st ... (inode at index 9)
			if len(fields) < 10 {
				continue
			}
			if fields[3] != "0A" { // 0x0A == TCP_LISTEN
				continue
			}
			local := fields[1]
			colon := strings.LastIndexByte(local, ':')
			if colon < 0 {
				continue
			}
			hexPort := strings.TrimLeft(local[colon+1:], "0")
			if hexPort == "" {
				hexPort = "0"
			}
			if hexPort == want {
				f.Close()
				return fields[9]
			}
		}
		f.Close()
	}
	return ""
}

// killVPCS reaps a daemonized vpcs as reliably as we can:
//  1. SIGKILL the process group we launched it in (Setpgid). If vpcs forked but
//     did NOT setsid, the daemon is still in this group and dies here.
//  2. Whatever pid is now LISTENing on its console port (via /proc), if any.
//  3. DETERMINISTIC: any vpcs process whose argv carries this node's unique
//     "-p <port>" console flag. VPCS double-forks AND setsid()s, escaping the
//     group (step 1 finds an empty group) and, once it has re-daemonized or a
//     duplicate stole the bind, it may no longer own the LISTEN socket step 2
//     keys on — so those two miss real orphans (observed: VPCS spinning at
//     100% CPU after lab.stop). The console port is unique per node and appears
//     verbatim in the daemon's argv, so matching it reaps the exact process
//     regardless of session/group/socket state.
//
// All steps are best-effort; a missing/already-dead pid is not an error.
func killVPCS(pgid, port int) error {
	err := killGroup(pgid)
	if pid := pidListeningOn(port); pid > 0 && pid != pgid {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	for _, pid := range pidsWithVPCSConsolePort(port) {
		if pid != pgid {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	return err
}

// pidsWithVPCSConsolePort returns every pid whose argv is a vpcs invocation
// carrying "-p <port>" (this node's unique console flag). It scans
// /proc/<pid>/cmdline (NUL-separated argv) for a token "vpcs" followed later by
// "-p" then the exact port. Best-effort: unreadable procs are skipped.
func pidsWithVPCSConsolePort(port int) []int {
	want := strconv.Itoa(port)
	var out []int
	entries, err := filepath.Glob("/proc/[0-9]*/cmdline")
	if err != nil {
		return nil
	}
	for _, path := range entries {
		raw, rerr := os.ReadFile(path)
		if rerr != nil || len(raw) == 0 {
			continue
		}
		args := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
		isVPCS := false
		for _, a := range args {
			if a == "vpcs" || strings.HasSuffix(a, "/vpcs") {
				isVPCS = true
				break
			}
		}
		if !isVPCS {
			continue
		}
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "-p" && args[i+1] == want {
				rest := strings.TrimPrefix(path, "/proc/")
				if slash := strings.IndexByte(rest, '/'); slash > 0 {
					if pid, perr := strconv.Atoi(rest[:slash]); perr == nil {
						out = append(out, pid)
					}
				}
				break
			}
		}
	}
	return out
}

// serveConsole accepts telnet clients on the console port and bridges each to
// the pty master. One active client at a time (a Cisco console is single-user);
// a new client preempts nothing on the pty — the master persists — and simply
// gets the live stream. The loop runs until the listener is closed (Stop).
//
// The listener is snapshotted ONCE under p.mu into a local before the loop:
// teardown() nils p.ln under the same lock, so re-reading p.ln each iteration
// would race — a node that exits immediately at spawn (e.g. VPCS misconfigured)
// triggers teardown right as this goroutine starts, and an unsynchronized
// p.ln.Accept() then nil-derefs and panics the whole supervisor. Once teardown
// closes the captured listener, Accept returns an error and the loop exits.
func (p *Process) serveConsole() {
	p.mu.Lock()
	ln := p.ln
	p.mu.Unlock()
	if ln == nil {
		return // already torn down
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed on Stop/teardown
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

// Stop terminates the process and marks the node stopped, then tears down any
// pty + console listener. For a daemonized VPCS (pgid set) it kills the whole
// process group so the detached vpcs child dies too; killing the launcher pid
// alone would leave an orphan.
func (p *Process) Stop() error {
	p.mu.Lock()
	cmd := p.cmd
	pgid := p.pgid
	p.mu.Unlock()

	p.Machine.To(StateStopped)

	var err error
	if pgid > 0 {
		// VPCS: kill the process group AND (fallback) whatever now owns the
		// console port, so a setsid()-daemonized vpcs leaves no orphan.
		err = killVPCS(pgid, p.Spec.ConsolePort)
	} else if cmd != nil && cmd.Process != nil {
		err = cmd.Process.Kill()
	}
	p.teardown()
	return err
}
