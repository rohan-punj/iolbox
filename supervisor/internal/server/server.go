// Package server binds the control protocol to a TCP listener and orchestrates
// the lab: it holds loaded lab state, allocates console/capture ports, wires
// links via the static-tap Linux-bridge fabric, and spawns nodes. It runs on
// any OS (node/fabric have Linux-only cores with stubs), so the control plane
// logic is testable on the dev box; the data plane only functions inside the
// Linux runtime.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/rohanpunj/iolbox/supervisor/internal/egress"
	"github.com/rohanpunj/iolbox/supervisor/internal/extnet"
	"github.com/rohanpunj/iolbox/supervisor/internal/image"
	"github.com/rohanpunj/iolbox/supervisor/internal/node"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
	"github.com/rohanpunj/iolbox/supervisor/internal/tool"
)

// Config configures a Server.
type Config struct {
	// ControlAddr is the bind address (must be a loopback host).
	ControlAddr string
	// ImageDir is where images are registered from.
	ImageDir string
	// RunDir is the base for per-lab working directories.
	RunDir string
	// LabsDir is where the durable lab-document store persists saved lab copies
	// (one <id>.json per lab). Created on first save. Empty defaults to
	// /opt/iolbox/labs.
	LabsDir string
	// IourcPath is the runtime's generated IOU license file, copied into each
	// lab's shared dir so co-located IOL instances find it. Empty defaults to
	// <ImageDir>/../iourc then /opt/iolbox/iourc (see prepareLabDir).
	IourcPath string
	// ConsoleBind is the host the per-node IOL console listeners bind (VPCS
	// binds its own console on all interfaces already). Default loopback;
	// set 0.0.0.0 so a native telnet client on the GUI host can dial
	// <vm-ip>:<consolePort> directly — same trust boundary as -ws-addr.
	ConsoleBind string
	// CaptureBind is the host each link's pcapng capture listener binds (the
	// tcpdump-on-bridge bcap server). Default loopback (the wsbridge dials via
	// loopback); set 0.0.0.0 so a native Wireshark on the GUI host can attach
	// with `wireshark -k -i TCP@<vm-ip>:<capturePort>`. Set via -capture-bind.
	CaptureBind string
	// ToolPacksDir is where the supervisor discovers installed tool-pack
	// manifests. Empty defaults to /opt/iolbox/tools/packs.
	ToolPacksDir string
	// StateDir is where durable tool identity and object state are stored.
	// Empty defaults to /var/lib/iolbox.
	StateDir string
	// Runtime/Arch are advertised in the hello handshake.
	Runtime string
	Arch    string
	Version string
	// DisableI386 is a deployment-provided capability restriction. It is set by
	// the Apple Silicon macOS drop-in after the Rosetta canary qualifies the
	// guest. A zero value deliberately preserves the legacy non-Mac contract.
	DisableI386 bool
	// Egress is the -egress flag value ("auto"|"slirp"|"routed"). "auto" (the
	// default) runs the egress detector at startup; the resolved "slirp"/"routed"
	// value is advertised in hello so the GUI can badge the NAT node when it can't
	// pass ICMP/traceroute. Empty defaults to "auto".
	Egress string
}

// Server is the supervisor control server.
type Server struct {
	cfg  Config
	disp *protocol.Dispatcher

	consolePorts *node.PortAllocator
	capturePorts *node.PortAllocator
	udpPorts     *node.PortAllocator
	natSubnets   *extnet.SubnetAllocator

	// caps reports which external-net node kinds (nat/mgmt) the runtime supports,
	// detected once at startup. Advertised in hello; enforced at lab.start.
	caps extnet.Capabilities
	// toolCaps reports whether the runtime can safely host tool process trees.
	// It is populated by InitRuntime and remains zero when startup cannot
	// establish the required delegation.
	toolCaps tool.Capabilities
	// toolRoot is the delegated cgroup root used by tool cages.
	toolRoot tool.CgroupRoot
	// toolPacks is the validated installed-pack registry used by tool handlers.
	toolPacks []tool.Pack
	// pcPack is the supervisor-owned built-in netprobe pack. It is deliberately
	// absent from toolPacks so ordinary tool.listPacks/config.pack cannot select
	// it.
	pcPack   tool.Pack
	pcPackOK bool
	// toolStop stops the supervisor-scope orphan reaper, when runtime startup
	// reached the point at which the reaper was started.
	toolStop func()

	// egress is the resolved internet-egress capability ("slirp" or "routed"),
	// from the -egress flag / auto-detection at startup. Advertised in hello so
	// the GUI can badge the NAT node when it can't pass ICMP/traceroute.
	egress string

	mu     sync.Mutex
	images map[string]image.Info // by id
	lab    *loadedLab

	// labMu serializes every control-plane operation that can observe or mutate
	// the loaded lab. Dispatch runs one goroutine per connection, and fault
	// timers plus the console/capture bridge call paths can enter the same state
	// outside that dispatcher. Keeping one lock at this boundary prevents two
	// lab lifecycles from interleaving kernel operations or publishing one lab
	// before the previous lab has finished tearing down.
	labMu sync.Mutex

	// cacheMu serializes read-modify-write cycles on the sidecar image
	// fingerprint cache in ImageDir (see imagescan.go).
	cacheMu sync.Mutex

	// broadcaster fans events out to connected clients.
	bc *broadcaster
}

// New builds a Server with default port bases (console 9000+, capture 5500+,
// udp 10000+ per docs/protocol.md).
func New(cfg Config) *Server {
	if cfg.Version == "" {
		cfg.Version = "0.1.0"
	}
	if cfg.Runtime == "" {
		cfg.Runtime = "debian-slim-12"
	}
	if cfg.Arch == "" {
		cfg.Arch = "x86_64"
	}
	if cfg.LabsDir == "" {
		cfg.LabsDir = "/opt/iolbox/labs"
	}
	if cfg.Egress == "" {
		cfg.Egress = "auto"
	}
	if cfg.ToolPacksDir == "" {
		cfg.ToolPacksDir = "/opt/iolbox/tools/packs"
	}
	if cfg.StateDir == "" {
		cfg.StateDir = "/var/lib/iolbox"
	}
	s := &Server{
		cfg:          cfg,
		disp:         protocol.NewDispatcher(),
		consolePorts: node.NewPortAllocator(9000, 1000),
		capturePorts: node.NewPortAllocator(5500, 1000),
		udpPorts:     node.NewPortAllocator(10000, 20000),
		natSubnets:   extnet.NewSubnetAllocator(),
		images:       make(map[string]image.Info),
		toolPacks:    []tool.Pack{},
		bc:           newBroadcaster(),
	}
	// Detect nat support once: nat needs /dev/net/tun + passwordless sudo. Off
	// Linux this is always false, so the dev box never advertises the feature.
	// See extnet.Detect / handleHello.
	s.caps = extnet.Detect(extnet.SudoOK())
	// Resolve the NAT egress capability once (auto-detects the QEMU slirp
	// signature; -egress slirp|routed forces it). Best-effort, never fails.
	s.egress = egress.Resolve(cfg.Egress)
	s.register()
	return s
}

// InitRuntime performs the kernel-affecting tool startup sequence only for
// the real supervisor process. Keeping it out of New lets control-plane tests
// construct servers without migrating their own PID or creating probe cages.
func (s *Server) InitRuntime() error {
	if err := tool.SetSubreaper(); err != nil {
		return fmt.Errorf("tool: set subreaper: %w", err)
	}

	root, err := tool.InitCgroupRoot()
	if err != nil {
		return fmt.Errorf("tool: initialize cgroup root: %w", err)
	}
	s.toolRoot = root

	instanceID, err := tool.InstanceID(s.cfg.StateDir)
	if err != nil {
		return fmt.Errorf("tool: establish instance identity: %w", err)
	}
	if err := tool.ReapStale(tool.ReapConfig{
		Root:       s.toolRoot,
		StateDir:   s.cfg.StateDir,
		RunDir:     s.cfg.RunDir,
		InstanceID: instanceID,
	}); err != nil {
		return fmt.Errorf("tool: reap stale objects: %w", err)
	}

	s.toolStop = tool.StartReaper(tool.Registry)
	s.toolCaps = tool.Detect(s.toolRoot)
	s.toolpacksLoad(s.cfg.ToolPacksDir)
	return nil
}

// StopRuntime closes the supervisor-scope reaper after listeners have
// stopped accepting work. Clearing the function first makes repeated cleanup
// calls harmless, including when InitRuntime failed before the reaper started.
func (s *Server) StopRuntime() {
	if s.toolStop == nil {
		return
	}
	stop := s.toolStop
	s.toolStop = nil
	stop()
}

// register wires every verb to its handler.
func (s *Server) register() {
	// The dispatcher is intentionally shared by all control connections. Wrap
	// every handler, including reads, because several read handlers return
	// pointers into loaded-lab runtime state and must not race a topology or
	// lifecycle mutation halfway through a response. Non-lab operations are
	// cheap and using the same wrapper keeps the registration audit-proof.
	handle := func(op string, h protocol.Handler) { s.disp.Handle(op, s.serializedHandler(h)) }
	handle("hello", s.handleHello)
	handle("tool.listPacks", s.handleToolListPacks)
	handle("pc.syncState", s.handlePCSyncState)
	handle("image.list", s.handleImageList)
	handle("image.register", s.handleImageRegister)
	handle("lab.load", s.handleLabLoad)
	handle("lab.saveDoc", s.handleLabSaveDoc)
	handle("lab.listDocs", s.handleLabListDocs)
	handle("lab.getDoc", s.handleLabGetDoc)
	handle("lab.deleteDoc", s.handleLabDeleteDoc)
	handle("lab.start", s.handleLabStart)
	handle("lab.stop", s.handleLabStop)
	handle("lab.wipe", s.handleLabWipe)
	handle("lab.reap", s.handleLabReap)
	handle("node.add", s.handleNodeAdd)
	handle("node.remove", s.handleNodeRemove)
	handle("node.start", s.handleNodeStart)
	handle("node.stop", s.handleNodeStop)
	handle("node.restart", s.handleNodeRestart)
	handle("node.setImage", s.handleNodeSetImage)
	handle("node.macs", s.handleNodeMACs)
	handle("link.add", s.handleLinkAdd)
	handle("link.remove", s.handleLinkRemove)
	handle("link.setFault", s.handleLinkSetFault)
	handle("capture.start", s.handleCaptureStart)
	handle("capture.stop", s.handleCaptureStop)
	handle("config.save", s.handleConfigExtract)
	handle("config.extract", s.handleConfigExtract)
	handle("painter.collect", s.handlePainterCollect)
	handle("painter.stpVlans", s.handlePainterSTPVlans)
	handle("status", s.handleStatus)
}

func (s *Server) serializedHandler(h protocol.Handler) protocol.Handler {
	return func(raw json.RawMessage) (any, error) {
		s.labMu.Lock()
		defer s.labMu.Unlock()
		return h(raw)
	}
}

func (s *Server) isCurrentLab(ll *loadedLab) bool {
	s.mu.Lock()
	current := s.lab
	s.mu.Unlock()
	return current == ll
}

// Dispatcher exposes the verb dispatcher (used by tests).
func (s *Server) Dispatcher() *protocol.Dispatcher { return s.disp }

// ConsolePort returns the allocated telnet console port for nodeID in the
// currently loaded lab. Used by the WebSocket console bridge
// (internal/wsbridge) to dial the right local port for GET /console/{nodeId}.
// ok is false if no lab is loaded or the node id is unknown.
func (s *Server) ConsolePort(nodeID int) (port int, ok bool) {
	s.labMu.Lock()
	defer s.labMu.Unlock()
	s.mu.Lock()
	ll := s.lab
	s.mu.Unlock()
	if ll == nil {
		return 0, false
	}
	nr := ll.get(nodeID)
	if nr == nil {
		return 0, false
	}
	return nr.consolePort, true
}

// ConsoleSubscribe attaches an in-process subscriber to nodeID's console hub
// (v0.3.0 Phase 2), letting internal/wsbridge consume decoded console output
// and write keystrokes without dialing ConsolePort over loopback TCP — see
// node.Process.Subscribe / node.consoleHub.Subscribe. Returns nil if no lab is
// loaded, the node id is unknown, the node hasn't started, or the node has no
// console hub at all (VPCS, which is its own telnet server — see the
// node.Process doc comment). Mirrors ConsolePort's existing precedent for
// crossing the internal/node boundary into the server/wsbridge layer.
func (s *Server) ConsoleSubscribe(nodeID int) *node.Subscription {
	s.labMu.Lock()
	defer s.labMu.Unlock()
	s.mu.Lock()
	ll := s.lab
	s.mu.Unlock()
	if ll == nil {
		return nil
	}
	nr := ll.get(nodeID)
	if nr == nil || nr.proc == nil {
		return nil
	}
	return nr.proc.Subscribe()
}

// CapturePort returns the local TCP port serving the live pcapng stream for
// linkID in the currently loaded lab, if that link has an active capture. Used
// by the WebSocket capture bridge (internal/wsbridge) to dial the right local
// port for GET /capture/{linkId}. ok is false if no lab is loaded or the link
// has no active capture. The port is the one capture.start recorded in the
// lab's capture bookkeeping (ll.captures), which mirrors the relay's own
// CapturePort().
func (s *Server) CapturePort(linkID int) (port int, ok bool) {
	s.labMu.Lock()
	defer s.labMu.Unlock()
	s.mu.Lock()
	ll := s.lab
	s.mu.Unlock()
	if ll == nil {
		return 0, false
	}
	ll.mu.Lock()
	port, ok = ll.captures[linkID]
	ll.mu.Unlock()
	if !ok || port == 0 {
		return 0, false
	}
	return port, true
}

// ListenAndServe binds the control address and serves connections until ctx is
// cancelled. It refuses non-loopback bind hosts.
func (s *Server) ListenAndServe(ctx context.Context) error {
	// Materialize the embedded starter labs on first run (no-op once the
	// store holds any lab).
	s.seedLabs()

	// Rebuild the image registry from ImageDir: the registry is in-memory
	// only, so without this a supervisor restart forgets every registered
	// image even though the files persist on disk.
	s.rescanImages()

	host, _, err := net.SplitHostPort(s.cfg.ControlAddr)
	if err != nil {
		return fmt.Errorf("control addr %q: %w", s.cfg.ControlAddr, err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("control addr host %q must be loopback (127.0.0.1)", host)
	}

	ln, err := net.Listen("tcp", s.cfg.ControlAddr)
	if err != nil {
		return err
	}
	defer ln.Close()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	// Per-link throughput sampler: polls relay counters every statsInterval and
	// emits link.stats events. Tied to ctx so it exits with the server.
	go s.statsLoop(ctx)

	var wg sync.WaitGroup
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				wg.Wait()
				s.shutdown()
				return nil
			default:
				return err
			}
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.serveConn(ctx, conn)
		}()
	}
}

// serveConn handles one TCP client connection by delegating to ServeConn, the
// transport-agnostic NDJSON connection core also used by the WebSocket
// bridge (internal/wsbridge) so both transports share identical dispatch,
// event-subscription, and error-handling behaviour.
func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	s.ServeConn(ctx, conn)
}

// ServeConn runs the NDJSON control-protocol loop over rwc until the stream
// ends, ctx is cancelled, or a write fails: it reads one NDJSON request per
// line, dispatches it through the shared verb dispatcher, writes the
// response, and subscribes rwc to server-pushed events for the connection's
// lifetime. It does not close rwc; the caller owns that.
//
// This is the seam that lets the TCP control listener (server.go) and the
// WebSocket /control endpoint (internal/wsbridge) behave identically: both
// hand ServeConn an io.ReadWriteCloser (a net.Conn for TCP, a small adapter
// over WebSocket text frames for WS) and get the same request/response/event
// semantics with no duplicated verb-handling logic.
func (s *Server) ServeConn(ctx context.Context, rwc io.ReadWriteCloser) {
	dec := protocol.NewDecoder(rwc)
	enc := protocol.NewEncoder(rwc)

	unsub := s.bc.subscribe(enc)
	defer unsub()

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = rwc.Close()
		case <-done:
		}
	}()

	for {
		req, err := dec.ReadRequest()
		if err != nil {
			return // EOF or malformed; drop the connection
		}
		resp := s.disp.Dispatch(req)
		if err := enc.WriteResponse(resp); err != nil {
			return
		}
	}
}

// shutdown stops all nodes and tears down the fabric on server exit.
func (s *Server) shutdown() {
	s.labMu.Lock()
	defer s.labMu.Unlock()
	s.mu.Lock()
	ll := s.lab
	s.mu.Unlock()
	if ll != nil {
		ll.stopAll()
		s.teardownFabric(ll)
	}
}

// emit pushes an event to all subscribers.
func (s *Server) emit(name string, data any) {
	s.bc.publish(protocol.NewEvent(name, data))
}
