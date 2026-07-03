package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rohanpunj/iolab/supervisor/internal/extnet"
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
	// Base features are always present; nat/mgmt are advertised ("natgw"/"mgmt")
	// only when the runtime detected support at startup (see extnet.Detect).
	features := []string{"nvram", "capture", "i386"}
	features = append(features, s.caps.GateFeatures()...)
	return protocol.HelloResult{
		Supervisor: s.cfg.Version,
		Runtime:    s.cfg.Runtime,
		Arch:       s.cfg.Arch,
		Features:   features,
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
	old := s.lab
	s.lab = ll
	s.mu.Unlock()

	// Stop the outgoing lab's nodes BEFORE dropping the reference. Without this,
	// loading a new lab over a running one orphans the old nodes: they keep
	// running as untracked supervisor children (observed: stranded IOL holding
	// gigabytes of RAM and spinning VPCS), because nothing else ever holds a
	// handle to them again. Then release its console ports, stop its link
	// relays, and release its capture + bridge-plan UDP ports — the old lab is
	// dropped without a plan rebuild, so nothing else would ever return those
	// ports to the allocators (leaked across every lab switch before this).
	if old != nil {
		for id := range old.nodes {
			s.stopNode(old, id)
		}
		s.stopBridges(old)
		for _, nr := range old.nodes {
			s.consolePorts.Release(nr.consolePort)
		}
		s.releaseCaptures(old, false)
		if old.bridge != nil {
			for _, bl := range old.bridge.links {
				_ = s.relays.Stop(bl.linkID)
			}
			for _, port := range old.bridge.udpPorts() {
				s.udpPorts.Release(port)
			}
			old.bridge = nil
		}
	}

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
	all := args.Nodes == nil
	ids := args.Nodes
	if ids == nil {
		for _, n := range ll.doc.Nodes {
			ids = append(ids, n.ID)
		}
	}
	for _, id := range ids {
		s.stopNode(ll, id)
	}
	// A full lab stop tears down the iouyap bridges too (per-node stop leaves them
	// up; they restart idempotently on the next spawn), and releases every armed
	// capture: the tee'd relays are stopped so the TCP capture ports actually
	// free, capture.stopped is emitted per link, and the ports return to the
	// allocator. Links whose doc still says capture.enabled re-arm automatically
	// on the next lab start (see armDocCaptures), with a fresh capture.started.
	if all {
		s.stopBridges(ll)
		s.releaseCaptures(ll, true)
	}
	return protocol.StartResult{Started: []protocol.StartedNode{}}, nil
}

// handleLabWipe resets node state like PNetLab's wipe: for each targeted node
// it stops the node (via the shared stop path, so consoles/relays clean up and
// node.state events fire) then deletes the node's persisted nvram_<id> file from
// the lab dir so the next boot starts from the injected startup-config again. A
// missing nvram file is not an error. Nodes nil = all nodes.
func (s *Server) handleLabWipe(raw json.RawMessage) (any, error) {
	var args protocol.LabWipeArgs
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
	wiped := []int{}
	for _, id := range ids {
		if ll.get(id) == nil {
			return nil, protocol.Errorf(protocol.CodeBadRequest, "unknown node %d", id)
		}
		// Stop first so the process releases the nvram file before we delete it.
		s.stopNode(ll, id)
		path := filepath.Join(ll.labDir(), nvramFilename(id))
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return nil, protocol.Errorf(protocol.CodeNvramCodecFailed, "wipe nvram node %d: %v", id, err)
		}
		wiped = append(wiped, id)
	}
	return protocol.LabWipeResult{Wiped: wiped}, nil
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
	// Auto-arm capture for every doc link with capture.enabled BEFORE the bridge
	// plan is built, so the plan's relay configs carry a pcapng tee port and the
	// relays started below listen from the first packet. Without this, a lab
	// (re)started with capture enabled in its doc was bridged (wiringFor honours
	// the doc) but ll.captures was empty, so the relay got NO tee port and
	// /capture/{id} 404'd forever — "enable capture and restart the lab" never
	// worked. Ports persist across per-node restarts (armDocCaptures only arms
	// missing links) and are released by lab.stop / capture.stop / lab.load.
	if err := s.armDocCaptures(ll); err != nil {
		return nil, err
	}
	// (Re)compute the whole-lab bridge plan (pseudo-instances + relay/iouyap
	// pairing) first: prepareLabDir's NETMAP and the iouyap bridges both derive
	// from it, so it must exist before either. This is pure (no sockets) and runs
	// on every platform so control-plane tests see the same NETMAP.
	if err := s.rebuildBridgePlan(ll); err != nil {
		return nil, protocol.Errorf(protocol.CodeBadRequest, "%v", err)
	}
	if err := s.prepareLabDir(ll); err != nil {
		return nil, err
	}
	// Announce every armed capture (idempotent): the GUI (re)learns each
	// capturing link's port on every start — including after a supervisor
	// restart or page reload where its event-derived state began empty — and
	// uses it to (re)connect live capture tabs.
	ll.mu.Lock()
	armed := make(map[int]int, len(ll.captures))
	for link, port := range ll.captures {
		armed[link] = port
	}
	ll.mu.Unlock()
	for link, port := range armed {
		s.emit(protocol.EventCaptureStarted, protocol.CaptureData{Link: link, CapturePort: port})
	}
	out := protocol.StartResult{Started: []protocol.StartedNode{}}
	for _, id := range ids {
		nr := ll.get(id)
		if nr == nil {
			return nil, protocol.Errorf(protocol.CodeBadRequest, "unknown node %d", id)
		}
		docNode := ll.findNode(id)

		// nat/mgmt nodes are supervisor-internal tap/macvtap endpoints, not
		// spawned processes: start an extnet.Endpoint that owns an fd + pump
		// goroutines instead of a node.Process. The node state machine still
		// flips to running so the GUI treats them uniformly. They have no console.
		if docNode.Kind == lab.KindNAT || docNode.Kind == lab.KindMgmt {
			started, err := s.startExtnetNode(ll, docNode, nr)
			if err != nil {
				return nil, err
			}
			out.Started = append(out.Started, started)
			continue
		}

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

// startExtnetNode brings up a nat/mgmt node's tap/macvtap endpoint and flips its
// state machine to running. It requires runtime capability support (gated at
// startup via extnet.Detect) and resolves the default-route / management
// interface plus, for nat, a subnet index. The endpoint is idempotent per node:
// if one is already running it is left as-is. Returns the StartedNode summary.
func (s *Server) startExtnetNode(ll *loadedLab, n *lab.Node, nr *nodeRuntime) (protocol.StartedNode, error) {
	if !s.caps.Supports(extnet.Kind(n.Kind)) {
		return protocol.StartedNode{}, protocol.Errorf(protocol.CodeUnsupported,
			"runtime does not support %s nodes", n.Kind)
	}
	if nr.extnet != nil {
		// Already running; report current state without re-creating the device.
		return protocol.StartedNode{Node: n.ID, State: string(nr.machine.State())}, nil
	}
	if !nr.machine.To(node.StateStarting) {
		return protocol.StartedNode{}, protocol.Errorf(protocol.CodeNodeSpawnFailed,
			"node %d: not in a startable state", n.ID)
	}

	cfg := extnet.Config{Kind: extnet.Kind(n.Kind), NodeID: n.ID}
	// Relay UDP ports (same pattern as VPCS): the endpoint pumps its tap against
	// the plan's relay endpoint for this node's single link.
	if ll.bridge != nil {
		if send, listen, ok := ll.bridge.extnetUDPFor(n.ID); ok {
			cfg.SendPort = send
			cfg.ListenPort = listen
		}
	}

	switch n.Kind {
	case lab.KindNAT:
		idx, err := s.natSubnets.Next()
		if err != nil {
			nr.machine.To(node.StateCrashed)
			return protocol.StartedNode{}, protocol.Errorf(protocol.CodeNodeSpawnFailed, "node %d: %v", n.ID, err)
		}
		def, err := extnet.DefaultRouteIface()
		if err != nil {
			s.natSubnets.Release(idx)
			nr.machine.To(node.StateCrashed)
			return protocol.StartedNode{}, protocol.Errorf(protocol.CodeNodeSpawnFailed, "node %d: %v", n.ID, err)
		}
		cfg.SubnetIndex = idx
		cfg.DefaultIface = def
		nr.natSubnet = idx
	case lab.KindMgmt:
		iface, err := extnet.PickMgmtIface(s.cfg.MgmtIface)
		if err != nil {
			nr.machine.To(node.StateCrashed)
			return protocol.StartedNode{}, protocol.Errorf(protocol.CodeNodeSpawnFailed, "node %d: %v", n.ID, err)
		}
		cfg.MgmtIface = iface
	}

	ep, err := extnet.Start(cfg)
	if err != nil {
		if nr.natSubnet != 0 {
			s.natSubnets.Release(nr.natSubnet)
			nr.natSubnet = 0
		}
		nr.machine.To(node.StateCrashed)
		return protocol.StartedNode{}, protocol.Errorf(protocol.CodeNodeSpawnFailed, "node %d: %v", n.ID, err)
	}
	nr.extnet = ep
	nr.machine.To(node.StateRunning)
	return protocol.StartedNode{Node: n.ID, State: string(nr.machine.State())}, nil
}

// buildSpec assembles a node.Spec from the lab node + runtime state.
func (s *Server) buildSpec(ll *loadedLab, n *lab.Node, nr *nodeRuntime) (node.Spec, error) {
	spec := node.Spec{
		NodeID:      n.ID,
		Kind:        string(n.Kind),
		WorkDir:     ll.workDir(n.ID),
		ConsolePort: nr.consolePort,
		ConsoleBind: s.cfg.ConsoleBind,
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
		// nvram_<id> file prepareLabDir writes, which carries the *effective*
		// config (the generated minimal default when the node has none).
		spec.NVRAMKiB = node.NVRAMKiBFor(len(effectiveStartupConfig(n)))
	case lab.KindVPCS:
		spec.VPCSCount = 1
		// Wire the PC's UDP tunnel to the relay if this VPCS is a bridged-link
		// endpoint in the plan (the IOL side reaches the same relay via iouyap).
		if ll.bridge != nil {
			if send, listen, ok := ll.bridge.vpcsUDPFor(n.ID); ok {
				spec.VPCSUDPLocal = listen // VPCS binds the relay's delivery port (-s)
				spec.VPCSUDPRemote = send  // VPCS sends to the relay's receiving port (-c)
			}
		}
	}
	return spec, nil
}

func (s *Server) stopNode(ll *loadedLab, id int) {
	nr := ll.get(id)
	if nr == nil {
		return
	}
	if nr.extnet != nil {
		// nat/mgmt: Close deletes the tap/macvtap and removes the iptables rules
		// (nat) by exact -D spec, then release the subnet index back to the pool.
		_ = nr.extnet.Close()
		nr.extnet = nil
		if nr.natSubnet != 0 {
			s.natSubnets.Release(nr.natSubnet)
			nr.natSubnet = 0
		}
		nr.machine.To(node.StateStopped)
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
	// Native same-host IOL<->IOL links carry traffic purely via the whole-lab
	// NETMAP; there is no relay to start. (A native link added at runtime that
	// wasn't in the boot NETMAP needs a node restart to take effect; we still
	// report link.up so the GUI reflects intent.)
	if wiringFor(&args.Link, isIOLMap(ll.doc), ll.doc.CaptureReadyEnabled()) == wiringNative {
		s.emit(protocol.EventLinkUp, protocol.LinkData{Link: args.Link.ID})
		return protocol.LinkData{Link: args.Link.ID}, nil
	}

	// Bridged link: start (or restart) its UDP relay from the whole-lab bridge
	// plan so the relay's ports match the iouyap bridges started at node spawn.
	// The iouyap netio<->UDP bridges for this link's IOL endpoints are created in
	// prepareLabDir (startBridges) before the IOL nodes spawn; here we only bring
	// up the UDP relay half.
	cfg, err := s.relayConfigFor(ll, args.Link.ID)
	if err != nil {
		return nil, err
	}
	_ = s.relays.Stop(args.Link.ID)
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

// relayConfigFor returns the relay.Config for a BRIDGED link, sourced from the
// whole-lab bridge plan so the relay's UDP ports match the iouyap netio<->UDP
// bridges (IOL endpoints) and the VPCS UDP tunnel ports. Native same-host
// IOL<->IOL links never reach here — they are wired via the whole-lab NETMAP
// (see wiringFor / netmapFor). The plan carries the pcapng CapturePort for any
// link with an active capture intent (ll.captures), so the returned config tees
// automatically when capture is on.
//
// The plan is (re)built lazily if absent (e.g. link.add before lab.start) using
// the current capture intents, so a relay started here always agrees with the
// pseudo-instance NETMAP and the iouyap bridges.
func (s *Server) relayConfigFor(ll *loadedLab, linkID int) (relay.Config, error) {
	if ll.bridge == nil {
		if err := s.rebuildBridgePlan(ll); err != nil {
			return relay.Config{}, protocol.Errorf(protocol.CodeBadRequest, "%v", err)
		}
	}
	cfg, ok := ll.bridge.relayConfigFor(linkID)
	if !ok {
		return relay.Config{}, protocol.Errorf(protocol.CodeBadRequest, "link %d is not a bridged link", linkID)
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
	// Reuse an already-armed port (doc auto-arm at lab start, or a repeated
	// capture.start): allocating a fresh one would orphan the old port without
	// release AND leave GUI/native clients pointing at a dead tee. wasArmed
	// gates the rollback paths below so a pre-existing port is never released
	// by a failed re-request.
	ll.mu.Lock()
	port, wasArmed := ll.captures[link.ID]
	ll.mu.Unlock()
	if !wasArmed {
		var err error
		port, err = s.capturePorts.Next()
		if err != nil {
			return nil, protocol.Errorf(protocol.CodePortUnavailable, "%v", err)
		}
	}
	// Record the capture intent, then rebuild the plan so this link becomes
	// bridged with a pcapng tee on its relay. NOTE: an IOL<->IOL link that booted
	// NATIVE (no bridging) only routes through the relay/tee after the affected
	// IOL nodes RESTART to re-read the NETMAP (now pointing at iouyap
	// pseudo-instances) — NETMAP is read once at boot. A link that was already
	// bridged (VPCS, segment) picks up the tee immediately on relay restart.
	ll.mu.Lock()
	ll.captures[link.ID] = port
	ll.mu.Unlock()
	if err := s.rebuildBridgePlan(ll); err != nil {
		if !wasArmed {
			ll.mu.Lock()
			delete(ll.captures, link.ID)
			ll.mu.Unlock()
			s.capturePorts.Release(port)
		}
		return nil, protocol.Errorf(protocol.CodeBadRequest, "%v", err)
	}
	// Restart iouyap bridges so a newly-bridged IOL endpoint's netio socket
	// exists for when its node restarts (no-op off Linux / for already-bridged
	// links whose sockets are up).
	if err := s.startBridges(ll); err != nil {
		return nil, err
	}
	// Re-create the relay with a tee on the capture port.
	_ = s.relays.Stop(link.ID)
	cfg, err := s.relayConfigFor(ll, link.ID)
	if err != nil {
		return nil, err
	}
	r, err := s.relays.Start(cfg)
	if err != nil {
		if !wasArmed {
			s.capturePorts.Release(port)
		}
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
	// Rebuild the plan without this link's tee. In capture-ready mode (the
	// default) an IOL<->IOL link stays bridged, so it simply gets a fresh relay
	// without the tee below — no restart, and still live-capturable again later.
	// With capture-ready OFF the link reverts to native (wiringFor flips back
	// once capture is off); its relay is torn down and traffic returns to native
	// netio only after the affected nodes restart to re-read the NETMAP.
	// VPCS/segment links always stay bridged and get a fresh relay without the tee.
	_ = s.relays.Stop(args.Link)
	if err := s.rebuildBridgePlan(ll); err != nil {
		return nil, protocol.Errorf(protocol.CodeBadRequest, "%v", err)
	}
	if link := ll.findLink(args.Link); link != nil && wiringFor(link, isIOLMap(ll.doc), ll.doc.CaptureReadyEnabled()) == wiringBridged {
		if cfg, ok := ll.bridge.relayConfigFor(args.Link); ok {
			_, _ = s.relays.Start(cfg)
		}
	}
	s.emit(protocol.EventCaptureStopped, protocol.CaptureData{Link: args.Link, CapturePort: port})
	return protocol.CaptureResult{Link: args.Link, CapturePort: port}, nil
}

// armDocCaptures allocates a capture port for every doc link with
// capture.enabled that has no runtime capture yet, recording it in ll.captures
// so the next bridge-plan rebuild gives that link's relay a pcapng tee. Links
// already armed (capture.start, or a previous lab start) keep their port.
// Called by startNodes BEFORE rebuildBridgePlan — the whole point is that the
// plan sees the ports. On allocator exhaustion the newly armed links are rolled
// back and the start fails.
func (s *Server) armDocCaptures(ll *loadedLab) error {
	ll.mu.Lock()
	defer ll.mu.Unlock()
	var added []int
	for i := range ll.doc.Links {
		l := &ll.doc.Links[i]
		if l.Capture == nil || !l.Capture.Enabled {
			continue
		}
		if _, ok := ll.captures[l.ID]; ok {
			continue
		}
		port, err := s.capturePorts.Next()
		if err != nil {
			for _, id := range added {
				s.capturePorts.Release(ll.captures[id])
				delete(ll.captures, id)
			}
			return protocol.Errorf(protocol.CodePortUnavailable, "capture link %d: %v", l.ID, err)
		}
		ll.captures[l.ID] = port
		added = append(added, l.ID)
	}
	return nil
}

// releaseCaptures returns every armed capture port to the allocator and clears
// the lab's capture bookkeeping. When emitStops is true it also stops each
// tee'd relay (so the TCP capture port actually frees before the port is
// reused) and emits capture.stopped per link — the full-lab-stop behaviour.
// With emitStops false it is the silent cleanup path for a lab being replaced
// by lab.load (its relays are stopped wholesale by the caller).
func (s *Server) releaseCaptures(ll *loadedLab, emitStops bool) {
	ll.mu.Lock()
	captures := make(map[int]int, len(ll.captures))
	for link, port := range ll.captures {
		captures[link] = port
	}
	ll.captures = make(map[int]int)
	ll.mu.Unlock()
	for link, port := range captures {
		if emitStops {
			_ = s.relays.Stop(link)
		}
		s.capturePorts.Release(port)
		if emitStops {
			s.emit(protocol.EventCaptureStopped, protocol.CaptureData{Link: link, CapturePort: port})
		}
	}
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

// netmapFor renders the whole-lab NETMAP: a native IOL<->IOL line for every
// natively-wired link (see nativeLinkSpecs / wiringFor) PLUS a bridged line for
// every bridged IOL endpoint (capture/VPCS/segment/cross-host), pointing that
// interface at the iouyap-owned pseudo-instance from the lab's bridge plan. It
// is written once, into the shared lab dir, before any IOL node spawns (Linux
// prepareLabDir). The plan must be built first (prepareLabDir does that); if it
// is nil (no bridged links, or off-Linux tests), only native lines are emitted.
func (s *Server) netmapFor(ll *loadedLab) string {
	var bridged []netmap.BridgedEndpoint
	if ll.bridge != nil {
		bridged = ll.bridge.bridgedEndpointsForNetmap()
	}
	return netmap.Build(nativeLinkSpecs(ll.doc), bridged...)
}
