// Package server binds the control protocol to a TCP listener and orchestrates
// the lab: it holds loaded lab state, allocates console/capture ports, wires
// links via the relay manager, and spawns nodes. It runs on any OS (node/relay
// have Linux-only cores with stubs), so the control plane logic is testable on
// the dev box; the data plane only functions inside the Linux runtime.
package server

import (
	"context"
	"fmt"
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
	// RunDir is the base for per-node working directories.
	RunDir string
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

// serveConn handles one client connection: reads requests, dispatches, writes
// responses, and subscribes the connection to event pushes.
func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	dec := protocol.NewDecoder(conn)
	enc := protocol.NewEncoder(conn)

	unsub := s.bc.subscribe(enc)
	defer unsub()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
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
