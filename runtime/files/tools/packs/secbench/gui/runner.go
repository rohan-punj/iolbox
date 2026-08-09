package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// venvPython / attacksDir are baked into the image layout by the Dockerfile.
const venvPython = "/opt/iolbox/tools/venv/bin/python"
const attacksDir = "/opt/iolbox/tools/packs/secbench/attacks"

// labIface is the ONLY network interface any attack helper is ever allowed to
// touch. It is not user-configurable from the GUI on purpose (see PATTERN.md
// safety rule): recon/sniff and every attack module bind to this constant.
const labIface = "eth1"

// ringBuffer keeps the last N stdout/stderr lines from a helper process.
type ringBuffer struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func newRing(max int) *ringBuffer { return &ringBuffer{max: max} }

func (r *ringBuffer) add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	if len(r.lines) > r.max {
		r.lines = r.lines[len(r.lines)-r.max:]
	}
}

func (r *ringBuffer) tail(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n >= len(r.lines) {
		out := make([]string, len(r.lines))
		copy(out, r.lines)
		return out
	}
	out := make([]string, n)
	copy(out, r.lines[len(r.lines)-n:])
	return out
}

// Runner supervises a single attack/recon helper process: start (spawn, lab
// NIC only), stop (kill), running state, and its output ring buffer. Unlike
// the AAA-style daemon pattern, a Runner does NOT auto-respawn â€” attacks run
// until the operator stops them or they finish (--count reached).
type Runner struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	running   bool
	startedAt time.Time
	log       *ringBuffer
}

func newRunner() *Runner { return &Runner{log: newRing(400)} }

func (r *Runner) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

func (r *Runner) StartedAt() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.startedAt
}

func (r *Runner) Tail(n int) []string { return r.log.tail(n) }

// Note surfaces a supervisor-side message (e.g. "refused: eth1 missing")
// straight into the module's own log view, so a failed Start is visible in
// the GUI and not just the container's stdout.
func (r *Runner) Note(msg string) { r.log.add("[supervisor] " + msg) }

// start launches the helper. Callers MUST build args starting with a
// hardcoded "--iface eth1" (see Supervisor.Start below) â€” start() itself
// does not touch the iface, it just execs whatever argv it is given.
func (r *Runner) start(argv []string) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return fmt.Errorf("already running")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		r.mu.Unlock()
		return err
	}
	r.cmd = cmd
	r.running = true
	r.startedAt = time.Now()
	r.mu.Unlock()
	r.log.add("[supervisor] started: " + strings.Join(argv, " "))

	var wg sync.WaitGroup
	for _, pipe := range []io.Reader{stdout, stderr} {
		wg.Add(1)
		go func(p io.Reader) {
			defer wg.Done()
			sc := bufio.NewScanner(p)
			sc.Buffer(make([]byte, 64*1024), 1024*1024)
			for sc.Scan() {
				r.log.add(sc.Text())
			}
		}(pipe)
	}
	go func() {
		wg.Wait()
		_ = cmd.Wait()
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
		r.log.add("[supervisor] exited")
	}()
	return nil
}

func (r *Runner) stop() {
	r.mu.Lock()
	cmd := r.cmd
	r.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// Supervisor owns one Runner per module, keyed by ModuleDef.Key.
type Supervisor struct {
	mu      sync.Mutex
	runners map[string]*Runner
}

func newSupervisor() *Supervisor {
	s := &Supervisor{runners: map[string]*Runner{}}
	for _, m := range moduleDefs {
		s.runners[m.Key] = newRunner()
	}
	return s
}

func (s *Supervisor) get(key string) *Runner {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runners[key]
}

// hasLabIface reports whether eth1 currently exists on this container. When
// it does not (node not yet wired into a lab, or wired with 0 links), every
// attack/recon module is disabled â€” there is nothing safe to bind to.
func hasLabIface() bool {
	ifc, err := net.InterfaceByName(labIface)
	return err == nil && ifc != nil
}

// stripIfaceFlag removes any "--iface"/"-i" token (long "--iface=value" form,
// or "--iface value" / "-i value" two-token form) from a caller-supplied argv
// slice. Used by Start below so the Raw-args field (server.go moduleStart,
// cfg.RawArgs) can never smuggle an alternate interface past the hardcoded
// lock â€” see the ENFORCEMENT POINT comment on Start.
func stripIfaceFlag(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--iface" || a == "-i":
			i++ // also drop the following value token, if any
		case strings.HasPrefix(a, "--iface="):
			// long "=" form carries its own value, nothing extra to skip
		default:
			out = append(out, a)
		}
	}
	return out
}

// Start spawns module `key`'s helper with the given extra CLI args.
//
// ===== SAFETY / eth1-lock ENFORCEMENT POINT =====
// This is the one and only place a helper process is exec'd. `--iface` is
// hardcoded to the labIface constant ("eth1"). Any `--iface`/`-i` token in the
// caller-supplied extra args (RawArgs on the Raw tab â€” the only path a user
// can inject arbitrary flags through) is stripped BEFORE appending, because
// Python's argparse keeps the LAST occurrence of a flag: appending extra args
// after "--iface eth1" would otherwise let a later "--iface eth2" silently
// win. Stripping (rather than relying on ordering) also protects any helper
// that instead uses a "first occurrence wins" parser. Every helper
// additionally calls common.enforce_lab_iface() at startup as defense-in-depth
// (attacks/common.py, now a hard allowlist of eth1 only). Start refuses
// outright if eth1 does not exist on the container.
func (s *Supervisor) Start(key string, extra []string) error {
	m := moduleByKey(key)
	if m == nil {
		return fmt.Errorf("no such module %q", key)
	}
	if !hasLabIface() {
		return fmt.Errorf("eth1 (lab NIC) is not present on this node â€” wire it into a lab topology first")
	}
	r := s.get(key)
	if r == nil {
		return fmt.Errorf("no runner for %q", key)
	}
	extra = stripIfaceFlag(extra)
	argv := append([]string{venvPython, attacksDir + "/" + m.Script, "--iface", labIface}, extra...)
	return r.start(argv)
}

func (s *Supervisor) Stop(key string) {
	if r := s.get(key); r != nil {
		r.stop()
	}
}

// StopAll is the dashboard's big red "Stop all attacks" button.
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.runners {
		r.stop()
	}
}

// RunningCount is used by the dashboard summary.
func (s *Supervisor) RunningCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.runners {
		if r.IsRunning() {
			n++
		}
	}
	return n
}
