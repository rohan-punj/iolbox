package server

import (
	"encoding/json"
	"fmt"

	"github.com/rohanpunj/iolab/supervisor/internal/image"
	"github.com/rohanpunj/iolab/supervisor/internal/lab"
	"github.com/rohanpunj/iolab/supervisor/internal/netmap"
	"github.com/rohanpunj/iolab/supervisor/internal/node"
	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
	"github.com/rohanpunj/iolab/supervisor/internal/relay"
)

// decode unmarshals raw args into v, returning a schema_invalid protocol error
// on failure. nil args decode to the zero value.
func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return protocol.Errorf(protocol.CodeBadRequest, "bad args: %v", err)
	}
	return nil
}

func (s *Server) handleHello(raw json.RawMessage) (any, error) {
	var args protocol.HelloArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	return protocol.HelloResult{
		Supervisor: s.cfg.Version,
		Runtime:    s.cfg.Runtime,
		Arch:       s.cfg.Arch,
		Features:   []string{"nvram", "capture", "i386"},
	}, nil
}

func (s *Server) handleImageList(_ json.RawMessage) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := protocol.ImageListResult{Images: []protocol.ImageInfo{}}
	for _, info := range s.images {
		out.Images = append(out.Images, toImageInfo(info))
	}
	return out, nil
}

func (s *Server) handleImageRegister(raw json.RawMessage) (any, error) {
	var args protocol.ImageRegisterArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	if args.Path == "" {
		return nil, protocol.NewError(protocol.CodeBadRequest, "path is required")
	}
	info, err := image.Inspect(args.Path)
	if err != nil {
		return nil, protocol.Errorf(protocol.CodeImageNotFound, "inspect %s: %v", args.Path, err)
	}
	s.mu.Lock()
	s.images[info.ID] = *info
	s.mu.Unlock()
	return protocol.ImageRegisterResult{
		ID:     info.ID,
		Class:  string(info.Class),
		Arch:   string(info.Arch),
		SHA256: info.SHA256,
	}, nil
}

func toImageInfo(info image.Info) protocol.ImageInfo {
	return protocol.ImageInfo{
		ID:       info.ID,
		Filename: info.Filename,
		Class:    string(info.Class),
		Arch:     string(info.Arch),
		SHA256:   info.SHA256,
		Size:     info.Size,
	}
}

func (s *Server) handleLabLoad(raw json.RawMessage) (any, error) {
	var args protocol.LabLoadArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	doc := args.Lab
	if err := doc.Validate(); err != nil {
		return nil, protocol.Errorf(protocol.CodeSchemaInvalid, "%v", err)
	}

	ll := newLoadedLab(&doc, s.cfg.RunDir)
	var nodes []protocol.NodeConsole
	var warnings []string

	for i := range doc.Nodes {
		n := &doc.Nodes[i]
		port, err := s.consolePorts.Next()
		if err != nil {
			return nil, protocol.Errorf(protocol.CodePortUnavailable, "%v", err)
		}
		nr := &nodeRuntime{
			id:          n.ID,
			consolePort: port,
			machine:     node.NewMachine(s.nodeStateCallback(n.ID)),
			ram:         n.RAM,
		}
		if n.Kind == lab.KindIOL && n.Image != nil {
			nr.imageID = n.Image.ID
			if _, ok := s.lookupImage(n.Image.ID); !ok {
				warnings = append(warnings, fmt.Sprintf("node %d references unregistered image %s", n.ID, n.Image.ID))
			}
		}
		ll.nodes[n.ID] = nr
		nodes = append(nodes, protocol.NodeConsole{ID: n.ID, ConsolePort: port})
	}

	s.mu.Lock()
	// Release console ports of a previously loaded lab.
	if s.lab != nil {
		for _, nr := range s.lab.nodes {
			s.consolePorts.Release(nr.consolePort)
		}
	}
	s.lab = ll
	s.mu.Unlock()

	return protocol.LabLoadResult{LabID: doc.ID, Nodes: nodes, Warnings: warnings}, nil
}

// nodeStateCallback returns a state-machine callback that emits node.state.
func (s *Server) nodeStateCallback(nodeID int) func(node.State) {
	return func(st node.State) {
		s.emit(protocol.EventNodeState, protocol.NodeStateData{Node: nodeID, State: string(st)})
	}
}

func (s *Server) lookupImage(id string) (image.Info, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, ok := s.images[id]
	return info, ok
}

// currentLab returns the loaded lab, verifying labId matches.
func (s *Server) currentLab(labID string) (*loadedLab, error) {
	s.mu.Lock()
	ll := s.lab
	s.mu.Unlock()
	if ll == nil {
		return nil, protocol.NewError(protocol.CodeNotLoaded, "no lab loaded")
	}
	if labID != "" && labID != ll.doc.ID {
		return nil, protocol.Errorf(protocol.CodeNotLoaded, "lab %q is not loaded (current: %q)", labID, ll.doc.ID)
	}
	return ll, nil
}

func (s *Server) handleLabStart(raw json.RawMessage) (any, error) {
	var args protocol.LabSelectArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	ll, err := s.currentLab(args.LabID)
	if err != nil {
		return nil, err
	}
	ids := args.Nodes
	if ids == nil {
		for _, n := range ll.doc.Nodes {
			ids = append(ids, n.ID)
		}
	}
	return s.startNodes(ll, ids)
}

func (s *Server) handleLabStop(raw json.RawMessage) (any, error) {
	var args protocol.LabSelectArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	ll, err := s.currentLab(args.LabID)
	if err != nil {
		return nil, err
	}
	ids := args.Nodes
	if ids == nil {
		for _, n := range ll.doc.Nodes {
			ids = append(ids, n.ID)
		}
	}
	for _, id := range ids {
		s.stopNode(ll, id)
	}
	return protocol.StartResult{Started: []protocol.StartedNode{}}, nil
}

func (s *Server) handleNodeStart(raw json.RawMessage) (any, error) {
	var args protocol.NodeArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	ll, err := s.currentLab(args.LabID)
	if err != nil {
		return nil, err
	}
	return s.startNodes(ll, []int{args.Node})
}

func (s *Server) handleNodeStop(raw json.RawMessage) (any, error) {
	var args protocol.NodeArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	ll, err := s.currentLab(args.LabID)
	if err != nil {
		return nil, err
	}
	s.stopNode(ll, args.Node)
	return protocol.StartResult{Started: []protocol.StartedNode{}}, nil
}

func (s *Server) handleNodeRestart(raw json.RawMessage) (any, error) {
	var args protocol.NodeArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	ll, err := s.currentLab(args.LabID)
	if err != nil {
		return nil, err
	}
	s.stopNode(ll, args.Node)
	return s.startNodes(ll, []int{args.Node})
}

// startNodes spawns the given nodes (Linux) or records the attempt (other OS).
//
// Before spawning, it (re)prepares the shared lab dir: the whole-lab NETMAP
// (native IOL<->IOL links), the shared iourc, and each IOL node's NVRAM with its
// startupConfig injected. IOL reads all of these from its cwd at boot, so they
// must exist first. prepareLabDir is a no-op off Linux.
func (s *Server) startNodes(ll *loadedLab, ids []int) (any, error) {
	if err := s.prepareLabDir(ll); err != nil {
		return nil, err
	}
	out := protocol.StartResult{Started: []protocol.StartedNode{}}
	for _, id := range ids {
		nr := ll.get(id)
		if nr == nil {
			return nil, protocol.Errorf(protocol.CodeBadRequest, "unknown node %d", id)
		}
		docNode := ll.findNode(id)
		spec, err := s.buildSpec(ll, docNode, nr)
		if err != nil {
			return nil, err
		}
		proc, err := node.Spawn(spec, nr.machine)
		if err != nil {
			return nil, protocol.Errorf(protocol.CodeNodeSpawnFailed, "%v", err)
		}
		nr.proc = proc
		// The console listener is bound synchronously inside Spawn (before it
		// returns), so ConsolePort is reachable the moment we get here: the
		// pty->telnet bridge accepts clients immediately, buffering the live
		// pty stream. Flip to running and announce the console.
		nr.machine.To(node.StateRunning)
		s.emit(protocol.EventNodeConsole, protocol.NodeConsoleData{Node: id, ConsolePort: nr.consolePort})
		out.Started = append(out.Started, protocol.StartedNode{
			Node:        id,
			ConsolePort: nr.consolePort,
			PID:         proc.PID(),
			State:       string(nr.machine.State()),
		})
	}
	return out, nil
}

// buildSpec assembles a node.Spec from the lab node + runtime state.
func (s *Server) buildSpec(ll *loadedLab, n *lab.Node, nr *nodeRuntime) (node.Spec, error) {
	spec := node.Spec{
		NodeID:      n.ID,
		Kind:        string(n.Kind),
		WorkDir:     ll.workDir(n.ID),
		ConsolePort: nr.consolePort,
		RAM:         n.RAM,
	}
	switch n.Kind {
	case lab.KindIOL:
		info, ok := s.lookupImage(nr.imageID)
		if !ok {
			return spec, protocol.Errorf(protocol.CodeImageNotFound, "node %d: image %s not registered", n.ID, nr.imageID)
		}
		spec.ImagePath = s.cfg.ImageDir + "/" + info.Filename
		spec.Ethernet = intOr(n.Ethernet, 1)
		spec.Serial = intOr(n.Serial, 1)
		// Size NVRAM to hold the injected startup-config (P0 correction #3:
		// boot pre-configured so IOS-XE PnP never engages). -n must be >= the
		// nvram_<id> file prepareLabDir writes.
		spec.NVRAMKiB = node.NVRAMKiBFor(len(n.StartupConfig))
	case lab.KindVPCS:
		spec.VPCSCount = 1
	}
	return spec, nil
}

func (s *Server) stopNode(ll *loadedLab, id int) {
	nr := ll.get(id)
	if nr == nil {
		return
	}
	if nr.proc != nil {
		_ = nr.proc.Stop()
		nr.proc = nil
	} else {
		nr.machine.To(node.StateStopped)
	}
}

func (s *Server) handleNodeSetImage(raw json.RawMessage) (any, error) {
	var args protocol.NodeSetImageArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	ll, err := s.currentLab(args.LabID)
	if err != nil {
		return nil, err
	}
	nr := ll.get(args.Node)
	if nr == nil {
		return nil, protocol.Errorf(protocol.CodeBadRequest, "unknown node %d", args.Node)
	}
	info, ok := s.lookupImage(args.ImageID)
	if !ok {
		return nil, protocol.Errorf(protocol.CodeImageNotFound, "image %s not registered", args.ImageID)
	}
	nr.imageID = args.ImageID
	// Reflect into the lab doc so status/config carry it.
	if dn := ll.findNode(args.Node); dn != nil && dn.Image != nil {
		dn.Image.ID = args.ImageID
	}
	return protocol.NodeSetImageResult{
		Node:    args.Node,
		ImageID: args.ImageID,
		Class:   string(info.Class),
	}, nil
}

func (s *Server) handleLinkAdd(raw json.RawMessage) (any, error) {
	var args protocol.LinkArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	ll, err := s.currentLab(args.LabID)
	if err != nil {
		return nil, err
	}

	// Native same-host IOL<->IOL links are realized through the whole-lab
	// NETMAP, which IOL reads once at boot from the shared lab dir. There is no
	// runtime relay to start for them: they come up when the NETMAP (written by
	// prepareLabDir before spawn) already contains the line and both endpoints
	// are running. A link.add for a native link that wasn't in the NETMAP at
	// boot needs a restart to take effect; we still report link.up so the GUI
	// reflects intent.
	//
	// TODO(iouyap): bridged links (capture/VPCS/cross-host) start a UDP relay
	// today; once internal/iouyap lands, insert its netio<->UDP bridge here so
	// the IOL side speaks netio into the bridge and the relay tees/floods UDP.
	if wiringFor(&args.Link, isIOLMap(ll.doc)) == wiringNative {
		s.emit(protocol.EventLinkUp, protocol.LinkData{Link: args.Link.ID})
		return protocol.LinkData{Link: args.Link.ID}, nil
	}

	cfg, err := s.buildRelayConfig(ll, &args.Link, 0)
	if err != nil {
		return nil, err
	}
	if _, err := s.relays.Start(cfg); err != nil {
		return nil, protocol.Errorf(protocol.CodeBadRequest, "%v", err)
	}
	s.emit(protocol.EventLinkUp, protocol.LinkData{Link: args.Link.ID})
	return protocol.LinkData{Link: args.Link.ID}, nil
}

func (s *Server) handleLinkRemove(raw json.RawMessage) (any, error) {
	var args protocol.LinkArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	if _, err := s.currentLab(args.LabID); err != nil {
		return nil, err
	}
	_ = s.relays.Stop(args.Link.ID)
	s.emit(protocol.EventLinkDown, protocol.LinkData{Link: args.Link.ID})
	return protocol.LinkData{Link: args.Link.ID}, nil
}

// buildRelayConfig derives the relay Config for a BRIDGED link (capture / VPCS /
// segment / cross-host). Native same-host IOL<->IOL links never reach here —
// they are wired via the whole-lab NETMAP (see wiringFor / netmapFor). UDP
// endpoint ports are allocated per node interface, modelling the VPCS UDP tunnel
// and the (future) iouyap netio<->UDP bridge pairing.
//
// TODO(iouyap): for IOL endpoints on a bridged link, the IOL side actually
// speaks unix-socket netio, so a netio<->UDP bridge (internal/iouyap, built in
// parallel) must sit between IOL and this UDP relay. This function allocates the
// UDP side; the iouyap bridge instance is created where startNodes handles a
// bridged link's IOL endpoints. Do NOT import iouyap here.
func (s *Server) buildRelayConfig(ll *loadedLab, link *lab.Link, capturePort int) (relay.Config, error) {
	kind := relay.KindP2P
	if link.EffectiveType() == lab.LinkSegment {
		kind = relay.KindHub
	}
	cfg := relay.Config{LinkID: link.ID, Kind: kind, CapturePort: capturePort}
	for _, ep := range link.Endpoints {
		if ll.findNode(ep.Node) == nil {
			return cfg, protocol.Errorf(protocol.CodeBadRequest, "link %d: unknown node %d", link.ID, ep.Node)
		}
		local, err := s.udpPorts.Next()
		if err != nil {
			return cfg, protocol.Errorf(protocol.CodePortUnavailable, "%v", err)
		}
		remote, err := s.udpPorts.Next()
		if err != nil {
			return cfg, protocol.Errorf(protocol.CodePortUnavailable, "%v", err)
		}
		cfg.Endpoints = append(cfg.Endpoints, relay.UDPEndpoint{
			Host: "127.0.0.1", LocalPort: local, RemotePort: remote,
		})
	}
	return cfg, nil
}

func (s *Server) handleCaptureStart(raw json.RawMessage) (any, error) {
	var args protocol.CaptureArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	ll, err := s.currentLab(args.LabID)
	if err != nil {
		return nil, err
	}
	link := ll.findLink(args.Link)
	if link == nil {
		return nil, protocol.Errorf(protocol.CodeBadRequest, "unknown link %d", args.Link)
	}
	port, err := s.capturePorts.Next()
	if err != nil {
		return nil, protocol.Errorf(protocol.CodePortUnavailable, "%v", err)
	}
	// Re-create the relay with a tee on the capture port.
	_ = s.relays.Stop(link.ID)
	cfg, err := s.buildRelayConfig(ll, link, port)
	if err != nil {
		return nil, err
	}
	r, err := s.relays.Start(cfg)
	if err != nil {
		s.capturePorts.Release(port)
		return nil, protocol.Errorf(protocol.CodeBadRequest, "%v", err)
	}
	actual := port
	if cp := r.CapturePort(); cp != 0 {
		actual = cp
	}
	ll.mu.Lock()
	ll.captures[link.ID] = actual
	ll.mu.Unlock()
	s.emit(protocol.EventCaptureStarted, protocol.CaptureData{Link: link.ID, CapturePort: actual})
	res := protocol.CaptureResult{Link: link.ID, CapturePort: actual}
	if args.Mode == "file" {
		res.File = args.File
	}
	return res, nil
}

func (s *Server) handleCaptureStop(raw json.RawMessage) (any, error) {
	var args protocol.CaptureArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	ll, err := s.currentLab(args.LabID)
	if err != nil {
		return nil, err
	}
	ll.mu.Lock()
	port, ok := ll.captures[args.Link]
	delete(ll.captures, args.Link)
	ll.mu.Unlock()
	if ok {
		s.capturePorts.Release(port)
	}
	// Rebuild the relay without a tee.
	_ = s.relays.Stop(args.Link)
	if link := ll.findLink(args.Link); link != nil {
		if cfg, cerr := s.buildRelayConfig(ll, link, 0); cerr == nil {
			_, _ = s.relays.Start(cfg)
		}
	}
	s.emit(protocol.EventCaptureStopped, protocol.CaptureData{Link: args.Link, CapturePort: port})
	return protocol.CaptureResult{Link: args.Link, CapturePort: port}, nil
}

func (s *Server) handleConfigExtract(raw json.RawMessage) (any, error) {
	var args protocol.ConfigArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	ll, err := s.currentLab(args.LabID)
	if err != nil {
		return nil, err
	}
	ids := args.Nodes
	if ids == nil {
		for _, n := range ll.doc.Nodes {
			ids = append(ids, n.ID)
		}
	}
	out := protocol.ConfigResult{Configs: []protocol.NodeConfig{}}
	for _, id := range ids {
		cfg, err := s.extractNVRAM(ll, id)
		if err != nil {
			return nil, err
		}
		out.Configs = append(out.Configs, protocol.NodeConfig{Node: id, StartupConfig: cfg})
	}
	return out, nil
}

func (s *Server) handleStatus(raw json.RawMessage) (any, error) {
	var args protocol.LabSelectArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	s.mu.Lock()
	ll := s.lab
	s.mu.Unlock()
	if ll == nil {
		return protocol.StatusResult{Nodes: []protocol.StatusNode{}, Links: []protocol.StatusLink{}}, nil
	}
	res := protocol.StatusResult{
		LabID: ll.doc.ID,
		Nodes: []protocol.StatusNode{},
		Links: []protocol.StatusLink{},
	}
	for _, dn := range ll.doc.Nodes {
		nr := ll.get(dn.ID)
		sn := protocol.StatusNode{ID: dn.ID, State: string(node.StateStopped), RAM: dn.RAM}
		if dn.Image != nil {
			sn.Image = dn.Image.ID
		}
		if nr != nil {
			sn.State = string(nr.machine.State())
			sn.ConsolePort = nr.consolePort
			if nr.proc != nil {
				sn.PID = nr.proc.PID()
			}
		}
		res.Nodes = append(res.Nodes, sn)
	}
	ll.mu.Lock()
	for _, dl := range ll.doc.Links {
		sl := protocol.StatusLink{ID: dl.ID}
		if port, ok := ll.captures[dl.ID]; ok {
			p := port
			sl.CapturePort = &p
		}
		res.Links = append(res.Links, sl)
	}
	ll.mu.Unlock()
	return res, nil
}

func intOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// netmapFor renders the whole-lab NETMAP for every natively-wired IOL<->IOL
// link (see nativeLinkSpecs / wiringFor). It is written once, into the shared
// lab dir, before any IOL node spawns (Linux prepareLabDir); bridged links
// (capture/VPCS/cross-host) are intentionally absent and are wired via
// iouyap+relay instead.
func (s *Server) netmapFor(ll *loadedLab) string {
	return netmap.Build(nativeLinkSpecs(ll.doc))
}
