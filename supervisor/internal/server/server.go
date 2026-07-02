// Package server binds the control protocol to a TCP listener and orchestrates
// the lab: it holds loaded lab state, allocates console/capture ports, wires
// links via the relay manager, and spawns nodes. It runs on any OS (node/relay
// have Linux-only cores with stubs), so the control plane logic is testable on
// the dev box; the data plane only functions inside the Linux runtime.
package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/rohanpunj/iolab/supervisor/internal/image"
	"github.com/rohanpunj/iolab/supervisor/internal/node"
	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
	"github.com/rohanpunj/iolab/supervisor/internal/relay"
)

// Config configures a Server.
type Config struct {
	// ControlAddr is the bind address (must be a loopback host).
	ControlAddr string
	// ImageDir is where images are registered from.
	ImageDir string
	// RunDir is the base for per-lab working directories.
	RunDir string
	// IourcPath is the runtime's generated IOU license file, copied into each
	// lab's shared dir so co-located IOL instances find it. Empty defaults to
	// <ImageDir>/../iourc then /opt/iolab/iourc (see prepareLabDir).
	IourcPath string
	// Runtime/Arch are advertised in the hello handshake.
	Runtime string
	Arch    string
	Version string
}

// Server is the supervisor control server.
type Server struct {
	cfg    Config
	disp   *protocol.Dispatcher
	relays *relay.Manager

	consolePorts *node.PortAllocator
	capturePorts *node.PortAllocator
	udpPorts     *node.PortAllocator

	mu     sync.Mutex
	images map[string]image.Info // by id
	lab    *loadedLab

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
	s := &Server{
		cfg:          cfg,
		disp:         protocol.NewDispatcher(),
		relays:       relay.NewManager(),
		consolePorts: node.NewPortAllocator(9000, 1000),
		capturePorts: node.NewPortAllocator(5500, 1000),
		udpPorts:     node.NewPortAllocator(10000, 20000),
		images:       make(map[string]image.Info),
		bc:           newBroadcaster(),
	}
	s.register()
	return s
}

// register wires every verb to its handler.
func (s *Server) register() {
	s.disp.Handle("hello", s.handleHello)
	s.disp.Handle("image.list", s.handleImageList)
	s.disp.Handle("image.register", s.handleImageRegister)
	s.disp.Handle("lab.load", s.handleLabLoad)
	s.disp.Handle("lab.start", s.handleLabStart)
	s.disp.Handle("lab.stop", s.handleLabStop)
	s.disp.Handle("node.start", s.handleNodeStart)
	s.disp.Handle("node.stop", s.handleNodeStop)
	s.disp.Handle("node.restart", s.handleNodeRestart)
	s.disp.Handle("node.setImage", s.handleNodeSetImage)
	s.disp.Handle("link.add", s.handleLinkAdd)
	s.disp.Handle("link.remove", s.handleLinkRemove)
	s.disp.Handle("capture.start", s.handleCaptureStart)
	s.disp.Handle("capture.stop", s.handleCaptureStop)
	s.disp.Handle("config.save", s.handleConfigExtract)
	s.disp.Handle("config.extract", s.handleConfigExtract)
	s.disp.Handle("status", s.handleStatus)
}

// Dispatcher exposes the verb dispatcher (used by tests).
func (s *Server) Dispatcher() *protocol.Dispatcher { return s.disp }

// ConsolePort returns the allocated telnet console port for nodeID in the
// currently loaded lab. Used by the WebSocket console bridge
// (internal/wsbridge) to dial the right local port for GET /console/{nodeId}.
// ok is false if no lab is loaded or the node id is unknown.
func (s *Server) ConsolePort(nodeID int) (port int, ok bool) {
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

// ListenAndServe binds the control address and serves connections until ctx is
// cancelled. It refuses non-loopback bind hosts.
func (s *Server) ListenAndServe(ctx context.Context) error {
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

// shutdown stops all relays and nodes on server exit.
func (s *Server) shutdown() {
	s.relays.StopAll()
	s.mu.Lock()
	ll := s.lab
	s.mu.Unlock()
	if ll != nil {
		ll.stopAll()
	}
}

// emit pushes an event to all subscribers.
func (s *Server) emit(name string, data any) {
	s.bc.publish(protocol.NewEvent(name, data))
}
