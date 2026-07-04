package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rohanpunj/iolbox/supervisor/internal/egress"
	"github.com/rohanpunj/iolbox/supervisor/internal/extnet"
	"github.com/rohanpunj/iolbox/supervisor/internal/image"
	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
	"github.com/rohanpunj/iolbox/supervisor/internal/netmap"
	"github.com/rohanpunj/iolbox/supervisor/internal/node"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
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
	// Base features are always present; nat is advertised ("natgw") only when the
	// runtime detected support at startup (see extnet.Detect).
	features := []string{"nvram", "capture", "i386"}
	features = append(features, s.caps.GateFeatures()...)
	return protocol.HelloResult{
		Supervisor: s.cfg.Version,
		Runtime:    s.cfg.Runtime,
		Arch:       s.cfg.Arch,
		Features:   features,
		Egress:     s.egress,
		EgressNote: egress.Note(s.egress),
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
	info, err := inspectOrTrustHint(args)
	if err != nil {
		return nil, protocol.Errorf(protocol.CodeImageNotFound, "inspect %s: %v", args.Path, err)
	}
	s.mu.Lock()
	s.images[info.ID] = *info
	s.mu.Unlock()
	// Seed the sidecar fingerprint cache so the startup rescan re-registers
	// this file after a restart without re-hashing it (see imagescan.go).
	s.cacheRegisteredImage(args.Path, info)
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
	// handle to them again. Then tear down its fabric (taps/bridges/shims),
	// release its console ports, and release its capture ports — the old lab is
	// dropped without a rebuild, so nothing else would ever return those ports.
	if old != nil {
		for id := range old.nodes {
			s.stopNode(old, id)
		}
		s.teardownFabric(old)
		for _, nr := range old.nodes {
			s.consolePorts.Release(nr.consolePort)
		}
		s.releaseCaptures(old, false)
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
	// A full lab stop tears down the fabric too (taps/bridges/netio<->tap iouyaps/
	// VPCS shims; per-node stop leaves them up and they restart idempotently on
	// the next spawn), and releases every armed capture: the bridge captures are
	// stopped so the TCP capture ports actually free, capture.stopped is emitted
	// per link, and the ports return to the allocator. Links whose doc still says
	// capture.enabled re-arm automatically on the next lab start (see
	// armDocCaptures), with a fresh capture.started.
	if all {
		s.teardownFabric(ll)
		s.releaseCaptures(ll, true)
	}
	return protocol.StartResult{Started: []protocol.StartedNode{}}, nil
}

// handleLabReap is the GUI "Force clean": it force-stops all runtime state
// regardless of tracking so orphans (nodes the GUI still thinks are running, a
// spinning console after an odd teardown) can be cleared from the GUI in one
// click. It stops every node of the loaded lab and tears down its fabric
// (taps/bridges/shims) and captures. Unlike lab.stop it takes no labId and
// never errors on a mismatch.
func (s *Server) handleLabReap(_ json.RawMessage) (any, error) {
	s.mu.Lock()
	ll := s.lab
	s.mu.Unlock()
	reaped := 0
	if ll != nil {
		for id := range ll.nodes {
			s.stopNode(ll, id)
			reaped++
		}
		s.teardownFabric(ll)
		s.releaseCaptures(ll, true)
	}
	return protocol.ReapResult{Reaped: reaped}, nil
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
	// Auto-arm capture for every doc link with capture.enabled BEFORE the fabric
	// is realised, so startFabric can start a bridge capture on its port the
	// moment the link's bridge is up. Without this, a lab (re)started with capture
	// enabled in its doc had an empty ll.captures, so /capture/{id} 404'd forever
	// — "enable capture and restart the lab" never worked. Ports persist across
	// per-node restarts (armDocCaptures only arms missing links) and are released
	// by lab.stop / capture.stop / lab.load.
	if err := s.armDocCaptures(ll); err != nil {
		return nil, err
	}
	// (Re)compute the whole-lab static-tap fabric first: prepareLabDir's NETMAP
	// and the netio<->tap iouyaps both derive from it, so it must exist before
	// either. It is deterministic and runs on every platform so control-plane
	// tests see the same NETMAP.
	s.refreshFabric(ll)
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

		// A nat node is a supervisor-internal tap endpoint, not a spawned
		// process: start an extnet.Endpoint that owns an fd instead of a
		// node.Process. The node state machine still flips to running so the GUI
		// treats it uniformly. It has no console.
		if docNode.Kind == lab.KindNAT {
			started, err := s.startExtnetNode(ll, docNode, nr)
			if err != nil {
				return nil, err
			}
			out.Started = append(out.Started, started)
			continue
		}

		// VPCS: bring up its udp<->tap shim BEFORE buildSpec, so VPCS is launched
		// with argv pointing at the shim's ports. Its tap is bridge-attached at
		// link time (hot-connect).
		if docNode.Kind == lab.KindVPCS {
			if err := s.setupVPCSFabric(ll, nr, docNode); err != nil {
				return nil, err
			}
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
		// Attach a fabric VPCS's tap to its link bridge now (its bridge was created
		// by startFabric before this node started). No-op for IOL / legacy VPCS /
		// an unlinked VPCS — link.add attaches it then.
		if nr.vtap != nil {
			if err := s.attachFabricForNode(ll, id); err != nil {
				return nil, err
			}
		}
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

// startExtnetNode brings up a nat node's tap endpoint and flips its state
// machine to running. It requires runtime capability support (gated at startup
// via extnet.Detect) and resolves the default-route interface plus a subnet
// index. The endpoint is idempotent per node: if one is already running it is
// left as-is. Returns the StartedNode summary.
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

	// A NAT node uses the static-tap bridge data plane: tap created unbridged,
	// gateway/NAT wired onto the link bridge by AttachBridge (so a NAT dropped
	// before its link is drawn still hot-connects).
	cfg := extnet.Config{Kind: extnet.Kind(n.Kind), NodeID: n.ID}

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
	// Attach this NAT to its link bridge now (created by startFabric before this
	// node started). No-op if its link isn't drawn yet — link.add attaches it then.
	if err := s.attachFabricForNode(ll, n.ID); err != nil {
		return protocol.StartedNode{}, err
	}
	return protocol.StartedNode{Node: n.ID, State: string(nr.machine.State())}, nil
}

// buildSpec assembles a node.Spec from the lab node + runtime state.
func (s *Server) buildSpec(ll *loadedLab, n *lab.Node, nr *nodeRuntime) (node.Spec, error) {
	spec := node.Spec{
		NodeID:      n.ID,
		Kind:        string(n.Kind),
		Name:        n.Name,
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
		if nr.vtap != nil {
			// The PC's UDP tunnel points at its udp<->tap shim. vtapPorts =
			// [vpcsBind, shimBind]: VPCS binds vpcsBind (-s) and sends to shimBind
			// (-c, the shim's bind port).
			spec.VPCSUDPLocal = nr.vtapPorts[0]
			spec.VPCSUDPRemote = nr.vtapPorts[1]
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
		// nat: Close deletes the tap and removes the iptables rules by exact -D
		// spec, then release the subnet index back to the pool.
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
	// Fabric VPCS: stop its shim, delete its tap, release its udp ports.
	if nr.vtap != nil || nr.vtapName != "" {
		s.teardownVPCS(nr)
	}
}

// handleNodeAdd registers a node the GUI just dropped onto an already-loaded
// lab: appends it to the loaded doc, allocates a console port, and creates its
// runtime — exactly what lab.load does per node — so it can start WITHOUT a
// page refresh. Without this a freshly dropped node was unknown to the
// supervisor (node.start -> "unknown node") until the next lab.load; NAT nodes
// were the visible victim since they're typically dropped and started
// mid-session. Validation runs on a candidate copy of the whole doc so a bad
// node (dup id, unknown kind, bad name) is rejected without side effects.
func (s *Server) handleNodeAdd(raw json.RawMessage) (any, error) {
	var args protocol.NodeAddArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	ll, err := s.currentLab(args.LabID)
	if err != nil {
		return nil, err
	}
	if ll.get(args.Node.ID) != nil {
		return nil, protocol.Errorf(protocol.CodeBadRequest, "node %d already exists", args.Node.ID)
	}
	candidate := *ll.doc
	candidate.Nodes = append(append([]lab.Node{}, ll.doc.Nodes...), args.Node)
	if err := candidate.Validate(); err != nil {
		return nil, protocol.Errorf(protocol.CodeSchemaInvalid, "%v", err)
	}

	port, err := s.consolePorts.Next()
	if err != nil {
		return nil, protocol.Errorf(protocol.CodePortUnavailable, "%v", err)
	}
	n := args.Node
	nr := &nodeRuntime{
		id:          n.ID,
		consolePort: port,
		machine:     node.NewMachine(s.nodeStateCallback(n.ID)),
		ram:         n.RAM,
	}
	if n.Kind == lab.KindIOL && n.Image != nil {
		nr.imageID = n.Image.ID
	}
	ll.doc.Nodes = append(ll.doc.Nodes, n)
	ll.nodes[n.ID] = nr
	s.emit(protocol.EventNodeConsole, protocol.NodeConsoleData{Node: n.ID, ConsolePort: port})
	return protocol.NodeAddResult{Node: n.ID, ConsolePort: port}, nil
}

// handleNodeRemove is node.add's inverse for GUI deletes: stop the node (full
// runtime cleanup), drop it and every link touching it from the loaded doc,
// detach those links' fabric bridges, release its console port, and refresh the
// fabric so the remaining wiring stays consistent.
func (s *Server) handleNodeRemove(raw json.RawMessage) (any, error) {
	var args protocol.NodeArgs
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
	s.stopNode(ll, args.Node)
	s.consolePorts.Release(nr.consolePort)
	delete(ll.nodes, args.Node)

	fabricOK := fabricNodes(ll.doc)
	kept := ll.doc.Links[:0]
	for i := range ll.doc.Links {
		l := &ll.doc.Links[i]
		touches := false
		for _, ep := range l.Endpoints {
			if ep.Node == args.Node {
				touches = true
				break
			}
		}
		if touches {
			if isFabricLink(l, fabricOK) {
				s.detachFabricLink(ll, l)
			}
		} else {
			kept = append(kept, *l)
		}
	}
	ll.doc.Links = kept
	var nodes []lab.Node
	for _, n := range ll.doc.Nodes {
		if n.ID != args.Node {
			nodes = append(nodes, n)
		}
	}
	ll.doc.Nodes = nodes
	s.refreshFabric(ll)
	s.emit(protocol.EventNodeState, protocol.NodeStateData{Node: args.Node, State: "stopped"})
	return protocol.NodeArgs{LabID: ll.doc.ID, Node: args.Node}, nil
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

	// Keep the LOADED doc in sync (upsert by link id): the fabric and future
	// refreshes all derive from ll.doc.Links, so a link that only lived in the
	// GUI's copy would never be wired until the next lab.load.
	replaced := false
	for i := range ll.doc.Links {
		if ll.doc.Links[i].ID == args.Link.ID {
			ll.doc.Links[i] = args.Link
			replaced = true
			break
		}
	}
	if !replaced {
		ll.doc.Links = append(ll.doc.Links, args.Link)
	}

	// The HOT-CONNECT path. Every interface already has its own static tap
	// (created at boot, topology-independent NETMAP), so wiring this link is a
	// pure runtime bridge-attach that never touches a running node — no relay, no
	// port churn, no restart. Refresh the fabric (so a newly reconnected interface
	// gets/keeps its static tap) then ensure the bridge + attach every member's
	// tap (both idempotent). An unlinked node whose tap isn't up yet is attached
	// by startFabric.
	s.refreshFabric(ll)
	if err := s.startFabric(ll); err != nil {
		return nil, err
	}
	s.emit(protocol.EventLinkUp, protocol.LinkData{Link: args.Link.ID})
	return protocol.LinkData{Link: args.Link.ID}, nil
}

func (s *Server) handleLinkRemove(raw json.RawMessage) (any, error) {
	var args protocol.LinkArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	ll, err := s.currentLab(args.LabID)
	if err != nil {
		return nil, err
	}
	// Detach the fabric link's taps + delete its bridge (the taps persist — the
	// interfaces are unconnected again, still tap-ready for a future hot-connect).
	// Classify from the CURRENT doc before we drop the link.
	if isFabricLink(&args.Link, fabricNodes(ll.doc)) {
		s.detachFabricLink(ll, &args.Link)
	}
	// Mirror the removal into the loaded doc (see handleLinkAdd's upsert) so the
	// wiring a later start/refresh derives never resurrects this link.
	kept := ll.doc.Links[:0]
	for _, l := range ll.doc.Links {
		if l.ID != args.Link.ID {
			kept = append(kept, l)
		}
	}
	ll.doc.Links = kept
	s.refreshFabric(ll)
	s.emit(protocol.EventLinkDown, protocol.LinkData{Link: args.Link.ID})
	return protocol.LinkData{Link: args.Link.ID}, nil
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
	// release AND leave GUI/native clients pointing at a dead capture. wasArmed
	// gates the rollback path so a pre-existing port is never released by a
	// failed re-request.
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

	// Capture the link's Linux bridge with tcpdump and serve it as pcapng on the
	// port model the GUI /capture/<id> path reads (ll.captures[link]). Every link
	// is capturable live with no node restart.
	actual, err := s.startBridgeCapture(ll, link.ID, port)
	if err != nil {
		if !wasArmed {
			s.capturePorts.Release(port)
		}
		return nil, protocol.Errorf(protocol.CodeBadRequest, "%v", err)
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
	// Stop the link's tcpdump/pcapng bridge capture; nothing else to do.
	s.stopBridgeCapture(ll, args.Link)
	s.emit(protocol.EventCaptureStopped, protocol.CaptureData{Link: args.Link, CapturePort: port})
	return protocol.CaptureResult{Link: args.Link, CapturePort: port}, nil
}

// armDocCaptures allocates a capture port for every doc link with
// capture.enabled that has no runtime capture yet, recording it in ll.captures
// so startFabric can auto-start a bridge capture on it once the link's bridge is
// up. Links already armed (capture.start, or a previous lab start) keep their
// port. Called by startNodes BEFORE startFabric. On allocator exhaustion the
// newly armed links are rolled back and the start fails.
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
// the lab's capture bookkeeping, and emits capture.stopped per link when
// emitStops is true (the full-lab-stop behaviour). With emitStops false it is
// the silent cleanup path for a lab being replaced by lab.load. The bridge
// captures themselves are stopped by teardownFabric, which every caller runs
// first.
func (s *Server) releaseCaptures(ll *loadedLab, emitStops bool) {
	ll.mu.Lock()
	captures := make(map[int]int, len(ll.captures))
	for link, port := range ll.captures {
		captures[link] = port
	}
	ll.captures = make(map[int]int)
	ll.mu.Unlock()
	for link, port := range captures {
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

// handlePainterCollect scrapes live protocol-decision state (STP/OSPF/EIGRP/BGP)
// from the running IOL nodes and returns a canvas-mappable result for the
// Topology Painter overlay. The heavy lifting (console scrape + parse) is in the
// platform-specific painterCollect (Linux does the real work; the Windows stub
// reports not-running).
func (s *Server) handlePainterCollect(raw json.RawMessage) (any, error) {
	var args protocol.PainterArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	switch args.Proto {
	case "stp", "ospf", "eigrp", "bgp":
	default:
		return nil, protocol.Errorf(protocol.CodeBadRequest, "painter.collect: unknown proto %q (want stp|ospf|eigrp|bgp)", args.Proto)
	}
	ll, err := s.currentLab(args.LabID)
	if err != nil {
		return nil, err
	}
	return s.painterCollect(context.Background(), ll, args)
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

// netmapFor renders the whole-lab NETMAP: a static-tap line for every IOL
// interface, pointing it at its own pseudo-instance/tap whether or not a link
// exists for it — the topology-independent NETMAP that makes hot-connect work
// (IOL reads NETMAP once at boot, so a link drawn later must not require a new
// line). It is written once, into the shared lab dir, before any IOL node spawns
// (Linux prepareLabDir); the fabric must be refreshed first (prepareLabDir's
// caller does that).
func (s *Server) netmapFor(ll *loadedLab) string {
	return netmap.BuildStatic(staticNetmapEntries(ll.staticTaps))
}
