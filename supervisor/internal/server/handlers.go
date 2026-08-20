package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/egress"
	"github.com/rohanpunj/iolbox/supervisor/internal/extnet"
	"github.com/rohanpunj/iolbox/supervisor/internal/image"
	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
	"github.com/rohanpunj/iolbox/supervisor/internal/netmap"
	"github.com/rohanpunj/iolbox/supervisor/internal/node"
	"github.com/rohanpunj/iolbox/supervisor/internal/painter"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
	"github.com/rohanpunj/iolbox/supervisor/internal/tool"
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
	// Base features are always present; nat and tools are advertised only when
	// their runtime probes detected support at startup.
	features := []string{"nvram", "capture"}
	if !s.cfg.DisableI386 {
		features = append(features, "i386")
	}
	features = append(features, s.caps.GateFeatures()...)
	features = append(features, s.toolCaps.GateFeatures()...)
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

	// T1.1 requires every tool node to name a known installed pack at load
	// time. internal/lab deliberately performs only the structural half of
	// that rule because importing internal/tool there would invert the package
	// layering; checking only in startToolNode would silently defer an invalid
	// document's failure until a later lifecycle operation.
	for _, n := range doc.Nodes {
		if n.Kind != lab.KindTool {
			continue
		}
		var packID string
		if err := json.Unmarshal(n.Config["pack"], &packID); err != nil {
			return nil, protocol.Errorf(protocol.CodeBadRequest,
				"tool: node %d has invalid pack id: %v", n.ID, err)
		}
		if _, ok := s.toolPack(packID); !ok {
			return nil, protocol.Errorf(protocol.CodeBadRequest,
				"tool: node %d references unknown pack %q", n.ID, packID)
		}
	}

	// ADOPT PATH (WS2): a lab.load for the SAME id, SAME topology, while at
	// least one node of the currently-loaded lab is actually up, must NOT go
	// through the teardown-and-reload path below. This is what makes a
	// second browser tab (or a GUI refresh that still ends up here, e.g. the
	// labGetDoc-failed fallback) safe to open on top of a lab someone is
	// already running: without this, opening the SAME lab a second time
	// looked identical to loading a different one and evicted every running
	// node out from under the first tab. Cosmetic-only differences (name,
	// position, annotations, startupConfig text) do not block adoption but
	// DO get persisted into the stored doc, so edits made in the second tab
	// before the reload aren't silently dropped.
	if res, ok := s.tryAdoptLoad(&doc); ok {
		return res, nil
	}

	ll := newLoadedLab(&doc, s.cfg.RunDir)
	nodes := []protocol.NodeConsole{}
	warnings := []string{}

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
			class := ""
			if info, ok := s.lookupImage(n.Image.ID); ok {
				class = string(info.Class)
			} else {
				warnings = append(warnings, fmt.Sprintf("node %d references unregistered image %s", n.ID, n.Image.ID))
			}
			// Report the RAM floor the node will actually launch with. buildSpec
			// raises it either way; warning here is what makes the correction
			// visible, because the failure it prevents (IOS wedging mid-init on
			// a live process) produces no error the supervisor can ever see.
			if eff := node.IOLRAMFor(n.RAM, class); eff != n.RAM {
				nr.ram = eff
				if n.RAM == 0 {
					warnings = append(warnings, fmt.Sprintf(
						"node %d has no ram set; using the %d MB class default", n.ID, eff))
				} else {
					warnings = append(warnings, fmt.Sprintf(
						"node %d ram %d MB is below the %d MB minimum for modern IOL images and would wedge during boot; raising it to %d MB",
						n.ID, n.RAM, eff, eff))
				}
			}
		}
		ll.nodes[n.ID] = nr
		nodes = append(nodes, protocol.NodeConsole{ID: n.ID, ConsolePort: port})
	}

	s.mu.Lock()
	old := s.lab
	s.mu.Unlock()

	// Stop the outgoing lab's nodes BEFORE dropping the reference. Without this,
	// loading a new lab over a running one orphans the old nodes: they keep
	// running as untracked supervisor children (observed: stranded IOL holding
	// gigabytes of RAM and spinning VPCS), because nothing else ever holds a
	// handle to them again. Then tear down its fabric (taps/bridges/shims),
	// release its console ports, and release its capture ports — the old lab is
	// dropped without a rebuild, so nothing else would ever return those ports.
	if old != nil {
		for _, id := range old.nodeIDs() {
			s.stopNode(old, id)
		}
		s.teardownFabric(old)
		for _, id := range old.nodeIDs() {
			if nr := old.get(id); nr != nil {
				s.consolePorts.Release(nr.consolePort)
			}
		}
		s.releaseCaptures(old, false)
	}

	// Publish only after the outgoing lab has released its children and
	// unscoped kernel objects. Tap/bridge names are not lab-scoped, so publishing
	// earlier allowed a concurrent start of the new lab to have this teardown
	// delete the new lab's devices by name.
	s.mu.Lock()
	s.lab = ll
	s.mu.Unlock()

	return protocol.LabLoadResult{LabID: doc.ID, Nodes: nodes, Warnings: warnings}, nil
}

// tryAdoptLoad services a lab.load WITHOUT any teardown when it targets the
// SAME lab id as the one currently loaded, that lab has at least one node
// that is actually up (not merely loaded-but-stopped — an idle lab gets the
// normal reload path, which is cheap and correctness-neutral there), and the
// new doc's topology is identical to the loaded one (see sameTopology). The
// second return value is false when none of those hold, meaning the caller
// must fall through to the ordinary teardown-and-reload path.
//
// On adoption: the stored doc's cosmetic fields (name/position/annotations/
// startupConfig/description/canvas — everything sameTopology ignores) are
// copied onto the RUNNING lab's doc so edits made before the reload are not
// lost, but the runtime (nodes map, fabric, captures) is left completely
// untouched. Console ports in the result come from the live nodeRuntimes,
// never freshly allocated — a fresh allocation here would desync the GUI
// (which still has the old ports) from the actual listening sockets.
func (s *Server) tryAdoptLoad(doc *lab.Lab) (protocol.LabLoadResult, bool) {
	s.mu.Lock()
	ll := s.lab
	s.mu.Unlock()
	if ll == nil {
		return protocol.LabLoadResult{}, false
	}
	current := ll.docSnapshot()
	if current.ID != doc.ID {
		return protocol.LabLoadResult{}, false
	}
	ll.mu.Lock()
	anyUp := false
	for _, nr := range ll.nodes {
		if nr.machine.State() != node.StateStopped {
			anyUp = true
			break
		}
	}
	ll.mu.Unlock()
	if !anyUp {
		return protocol.LabLoadResult{}, false
	}
	if !sameTopology(current, doc) {
		return protocol.LabLoadResult{}, false
	}

	// Swap in the new doc's cosmetic content while keeping the loaded lab's
	// runtime (ll.nodes/fabric/captures) — topology is identical so every
	// node id used by the runtime maps still lines up against the new doc.
	// Guard the swap with ll.mu (the same lock handleStatus/fabricStats hold
	// when they walk ll.doc.Links) so a concurrent stats/status reader can't
	// tear the ll.doc pointer; no s.mu is held here, so there's no lock nesting.
	ll.mu.Lock()
	ll.doc = doc
	ll.mu.Unlock()

	var nodes []protocol.NodeConsole
	for i := range doc.Nodes {
		n := &doc.Nodes[i]
		nr := ll.get(n.ID)
		if nr == nil {
			// Should be unreachable (sameTopology guarantees identical node id
			// sets), but fall back to the teardown path rather than panic or
			// return a bogus port if it ever happens.
			return protocol.LabLoadResult{}, false
		}
		nodes = append(nodes, protocol.NodeConsole{ID: n.ID, ConsolePort: nr.consolePort})
	}
	return protocol.LabLoadResult{LabID: doc.ID, Nodes: nodes, Adopted: true}, true
}

// sameTopology reports whether a and b describe the same runtime shape: the
// same set of nodes (by id, kind, image id, ethernet/serial adapter counts)
// and the same set of links (by id and its endpoints as an unordered set).
// Names, positions, icons, annotations, canvas view state, and startupConfig
// text are COSMETIC and deliberately excluded — changing them must not block
// lab.load adoption (see tryAdoptLoad), since none of them affect the running
// fabric/process set. Node and link ORDER is irrelevant (compared as sets),
// only membership.
func sameTopology(a, b *lab.Lab) bool {
	if len(a.Nodes) != len(b.Nodes) || len(a.Links) != len(b.Links) {
		return false
	}
	an := make(map[int]lab.Node, len(a.Nodes))
	for _, n := range a.Nodes {
		an[n.ID] = n
	}
	for _, nb := range b.Nodes {
		na, ok := an[nb.ID]
		if !ok || !sameNodeShape(na, nb) {
			return false
		}
	}

	al := make(map[int]lab.Link, len(a.Links))
	for _, l := range a.Links {
		al[l.ID] = l
	}
	for _, lb := range b.Links {
		la, ok := al[lb.ID]
		if !ok || !sameEndpointSet(la.Endpoints, lb.Endpoints) {
			return false
		}
	}
	return true
}

// sameNodeShape compares the runtime-relevant fields of two nodes sharing an
// id: kind, image id (empty string when Image is nil, on both sides, for a
// non-iol node), and ethernet/serial adapter counts (nil treated as the same
// as an explicit 0 — buildSpec's intOr default — so a doc that never set the
// field matches one that set it to the default explicitly).
func sameNodeShape(a, b lab.Node) bool {
	if a.Kind != b.Kind {
		return false
	}
	if imageID(a) != imageID(b) {
		return false
	}
	return intOr(a.Ethernet, 1) == intOr(b.Ethernet, 1) && intOr(a.Serial, 1) == intOr(b.Serial, 1)
}

func imageID(n lab.Node) string {
	if n.Image == nil {
		return ""
	}
	return n.Image.ID
}

// sameEndpointSet compares two links' endpoints as an unordered set of
// {node,interface} pairs (a link's two endpoints can be listed in either
// order without changing its meaning).
func sameEndpointSet(a, b []lab.Endpoint) bool {
	if len(a) != len(b) {
		return false
	}
	count := make(map[lab.Endpoint]int, len(a))
	for _, e := range a {
		count[e]++
	}
	for _, e := range b {
		count[e]--
	}
	for _, c := range count {
		if c != 0 {
			return false
		}
	}
	return true
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
	current := ll.docSnapshot()
	if labID != "" && labID != current.ID {
		return nil, protocol.Errorf(protocol.CodeNotLoaded, "lab %q is not loaded (current: %q)", labID, current.ID)
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
		for _, n := range ll.docSnapshot().Nodes {
			ids = append(ids, n.ID)
		}
	}
	out, err := s.startNodes(ll, ids)
	if err != nil {
		return nil, err
	}
	// Fault state is separate from link.stats and must be replayed even for an
	// idle/admin-down link that will never generate a stats tick.
	for _, l := range ll.docSnapshot().Links {
		if l.Fault != nil {
			s.emitLinkFault(ll, l.ID, "lab start")
		}
	}
	return out, nil
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
		for _, n := range ll.docSnapshot().Nodes {
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
		// Full stop deactivates runtime faults during fabric teardown. Replay the
		// resulting inactive state so the canvas does not retain an admin-down or
		// impairment badge while the lab is stopped; the persisted definition is
		// still present and will be replayed/applied again only when appropriate at
		// the next lab.start.
		for _, l := range ll.docSnapshot().Links {
			if l.Fault != nil {
				s.emitLinkFault(ll, l.ID, "lab stop")
			}
		}
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
		for _, id := range ll.nodeIDs() {
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
		for _, n := range ll.docSnapshot().Nodes {
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
	// stopNode is synchronous all the way down: node.Process.Stop and
	// tool.Endpoint.Stop each block (bounded) until the killed process is
	// observed gone, and teardownVPCS/extnet.Close release the tap and UDP
	// ports before returning. So by the time startNodes runs, the old
	// instance's NETIO socket dir, console port and tap are genuinely free —
	// no old/new overlap for the same node id.
	s.stopNode(ll, args.Node)
	return s.startNodes(ll, []int{args.Node})
}

// preflightIOLImages applies deployment capability policy before any start
// side effects. In particular, a disabled i386 capability must not arm
// captures, rebuild fabric, prepare NVRAM, or otherwise make a rejected start
// look like a deeper launch failure. Unknown images remain allowed here: the
// normal image-not-found path owns that error, and only a positively identified
// i386 image is restricted.
func (s *Server) preflightIOLImages(ll *loadedLab, ids []int) ([]int, protocol.StartResult) {
	eligible := make([]int, 0, len(ids))
	out := protocol.StartResult{Started: []protocol.StartedNode{}}
	for _, id := range ids {
		nr := ll.get(id)
		n := ll.findNode(id)
		if nr == nil || n == nil || n.Kind != lab.KindIOL || !s.cfg.DisableI386 {
			eligible = append(eligible, id)
			continue
		}
		// Start is idempotent for a live process. Preserve that contract before
		// consulting the image registry; an already-running node never reaches
		// the spec path and must still be reported as running.
		state := nr.machine.State()
		if nr.proc != nil && (state == node.StateStarting || state == node.StateRunning) {
			eligible = append(eligible, id)
			continue
		}
		info, ok := s.lookupImage(nr.imageID)
		if !ok || info.Arch != image.ArchI386 {
			eligible = append(eligible, id)
			continue
		}
		err := protocol.Errorf(protocol.CodeImageArchMismatch,
			"node %d: i386 IOL images are disabled by this runtime", id)
		out.Failed = append(out.Failed, startFailure(id, nr, err))
	}
	return eligible, out
}

// startNodes spawns the given nodes (Linux) or records the attempt (other OS).
//
// Before spawning, it (re)prepares the shared lab dir: the whole-lab NETMAP
// (native IOL<->IOL links), the shared iourc, and each IOL node's NVRAM with its
// startupConfig injected. IOL reads all of these from its cwd at boot, so they
// must exist first. prepareLabDir is a no-op off Linux.
func (s *Server) startNodes(ll *loadedLab, ids []int) (any, error) {
	singleRequest := len(ids) == 1
	ids, preflight := s.preflightIOLImages(ll, ids)
	if len(ids) == 0 {
		return preflight, nil
	}
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
	if err := s.prepareLabDir(ll, ids); err != nil {
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
	out := preflight
	if out.Started == nil {
		out.Started = []protocol.StartedNode{}
	}
	for _, id := range ids {
		nr := ll.get(id)
		if nr == nil {
			err := protocol.Errorf(protocol.CodeBadRequest, "unknown node %d", id)
			if singleRequest {
				return nil, err
			}
			out.Failed = append(out.Failed, startFailure(id, nil, err))
			continue
		}
		docNode := ll.findNode(id)
		if docNode == nil {
			err := protocol.Errorf(protocol.CodeBadRequest, "unknown node %d", id)
			if singleRequest {
				return nil, err
			}
			out.Failed = append(out.Failed, startFailure(id, nil, err))
			continue
		}
		if docNode.Kind == lab.KindIOL || docNode.Kind == lab.KindVPCS {
			state := nr.machine.State()
			if nr.proc != nil && (state == node.StateStarting || state == node.StateRunning) {
				out.Started = append(out.Started, protocol.StartedNode{
					Node: id, ConsolePort: nr.consolePort, PID: nr.proc.PID(),
					State: string(state),
				})
				continue
			}
			// An unexpected exit leaves the Process pointer in the runtime record
			// until the next start. Do not mistake that stale handle for a live
			// node; Spawn is allowed to advance crashed/stopped state to starting.
			if nr.proc != nil {
				nr.proc = nil
			}
		}

		// A nat node is a supervisor-internal tap endpoint, not a spawned
		// process: start an extnet.Endpoint that owns an fd instead of a
		// node.Process. The node state machine still flips to running so the GUI
		// treats it uniformly. It has no console.
		if docNode.Kind == lab.KindNAT {
			started, err := s.startExtnetNode(ll, docNode, nr)
			if err != nil {
				if singleRequest {
					return nil, err
				}
				out.Failed = append(out.Failed, startFailure(id, nr, err))
				continue
			}
			out.Started = append(out.Started, started)
			continue
		}

		if docNode.Kind == lab.KindTool {
			started, err := s.startToolNode(ll, docNode, nr)
			if err != nil {
				if singleRequest {
					return nil, err
				}
				out.Failed = append(out.Failed, startFailure(id, nr, err))
				continue
			}
			out.Started = append(out.Started, started)
			continue
		}

		if docNode.Kind == lab.KindPC {
			started, err := s.startPCNode(ll, docNode, nr)
			if err != nil {
				if singleRequest {
					return nil, err
				}
				out.Failed = append(out.Failed, startFailure(id, nr, err))
				continue
			}
			out.Started = append(out.Started, started)
			continue
		}

		// VPCS: bring up its udp<->tap shim BEFORE buildSpec, so VPCS is launched
		// with argv pointing at the shim's ports. Its tap is bridge-attached at
		// link time (hot-connect).
		if docNode.Kind == lab.KindVPCS {
			if err := s.setupVPCSFabric(ll, nr, docNode); err != nil {
				if singleRequest {
					return nil, err
				}
				out.Failed = append(out.Failed, startFailure(id, nr, err))
				continue
			}
		}

		spec, err := s.buildSpec(ll, docNode, nr)
		if err != nil {
			if nr.vtap != nil || nr.vtapName != "" {
				s.teardownVPCS(nr)
			}
			if singleRequest {
				return nil, err
			}
			out.Failed = append(out.Failed, startFailure(id, nr, err))
			continue
		}
		proc, err := node.Spawn(spec, nr.machine)
		if err != nil {
			if nr.vtap != nil || nr.vtapName != "" {
				s.teardownVPCS(nr)
			}
			spawnErr := protocol.Errorf(protocol.CodeNodeSpawnFailed, "%v", err)
			if singleRequest {
				return nil, spawnErr
			}
			out.Failed = append(out.Failed, startFailure(id, nr, spawnErr))
			continue
		}
		nr.proc = proc
		// The console listener is bound synchronously inside Spawn (before it
		// returns), so ConsolePort is reachable the moment we get here: the
		// pty->telnet bridge accepts clients immediately, buffering the live
		// pty stream. Flip to running and announce the console.
		if !nr.machine.To(node.StateRunning) {
			_ = proc.Stop()
			nr.proc = nil
			if nr.vtap != nil || nr.vtapName != "" {
				s.teardownVPCS(nr)
			}
			out.Failed = append(out.Failed, startFailure(id, nr,
				protocol.Errorf(protocol.CodeNodeSpawnFailed, "node %d exited before becoming running", id)))
			continue
		}
		// Attach a fabric VPCS's tap to its link bridge now (its bridge was created
		// by startFabric before this node started). No-op for IOL / legacy VPCS /
		// an unlinked VPCS — link.add attaches it then.
		if nr.vtap != nil {
			if err := s.attachFabricForNode(ll, id); err != nil {
				_ = proc.Stop()
				nr.proc = nil
				s.teardownVPCS(nr)
				nr.machine.To(node.StateCrashed)
				if singleRequest {
					return nil, err
				}
				out.Failed = append(out.Failed, startFailure(id, nr, err))
				continue
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

func startFailure(id int, nr *nodeRuntime, err error) protocol.StartFailure {
	failure := protocol.StartFailure{Node: id, Error: err.Error()}
	if nr != nil && nr.machine != nil {
		failure.State = string(nr.machine.State())
	}
	return failure
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
		_ = nr.extnet.Close()
		nr.extnet = nil
		if nr.natSubnet != 0 {
			s.natSubnets.Release(nr.natSubnet)
			nr.natSubnet = 0
		}
		nr.machine.To(node.StateCrashed)
		return protocol.StartedNode{}, err
	}
	return protocol.StartedNode{Node: n.ID, State: string(nr.machine.State())}, nil
}

// startToolNode starts the pack GUI through internal/tool, which owns the
// ordered preclean, cage, netns/veth, socket/options-file, launch, readiness,
// and exit-watcher lifecycle. The endpoint turns Options into the 0600,
// ioltool-owned options.json read through IOLBOX_TOOL_OPTIONS; a tool node
// cannot start without that file existing, so the handler passes the document
// payload through and lets tool.Start create {} when it is absent.
func (s *Server) startToolNode(ll *loadedLab, n *lab.Node, nr *nodeRuntime) (protocol.StartedNode, error) {
	if !s.toolCaps.Supports(tool.KindTool) {
		return protocol.StartedNode{}, protocol.Errorf(protocol.CodeUnsupported,
			"tool: runtime does not support %s nodes: %v", tool.KindTool, s.toolCaps.Reasons)
	}
	if nr.tool != nil {
		// Already running; report current state without recreating the endpoint.
		return protocol.StartedNode{Node: n.ID, State: string(nr.machine.State())}, nil
	}
	if !nr.machine.To(node.StateStarting) {
		return protocol.StartedNode{}, protocol.Errorf(protocol.CodeNodeSpawnFailed,
			"node %d: not in a startable state", n.ID)
	}

	var packID string
	if err := json.Unmarshal(n.Config["pack"], &packID); err != nil {
		nr.machine.To(node.StateCrashed)
		return protocol.StartedNode{}, protocol.Errorf(protocol.CodeBadRequest,
			"tool: node %d has invalid pack id: %v", n.ID, err)
	}
	pack, ok := s.toolPack(packID)
	if !ok {
		nr.machine.To(node.StateCrashed)
		return protocol.StartedNode{}, protocol.Errorf(protocol.CodeBadRequest,
			"tool: node %d references unknown pack %q", n.ID, packID)
	}

	var options []byte
	if raw, exists := n.Config["options"]; exists {
		if !json.Valid(raw) {
			nr.machine.To(node.StateCrashed)
			return protocol.StartedNode{}, protocol.Errorf(protocol.CodeBadRequest,
				"tool: node %d has invalid options JSON", n.ID)
		}
		options = append([]byte(nil), raw...)
	}

	// Optional static IP for GuestIface (eth1) — most packs don't need one
	// (they operate at L2 or forge their own L3 headers), so an absent/empty
	// "net.ip" leaves the interface unaddressed exactly as before this field
	// existed.
	var netCfg *tool.NetAddrConfig
	if raw, exists := n.Config["net"]; exists {
		var addr struct {
			IP        string `json:"ip"`
			PrefixLen int    `json:"prefixLen"`
			Gateway   string `json:"gateway"`
		}
		if err := json.Unmarshal(raw, &addr); err != nil {
			nr.machine.To(node.StateCrashed)
			return protocol.StartedNode{}, protocol.Errorf(protocol.CodeBadRequest,
				"tool: node %d has invalid net config: %v", n.ID, err)
		}
		if addr.IP != "" {
			if addr.PrefixLen <= 0 || addr.PrefixLen > 32 {
				addr.PrefixLen = 24
			}
			netCfg = &tool.NetAddrConfig{IP: addr.IP, PrefixLen: addr.PrefixLen, Gateway: addr.Gateway}
		}
	}

	cfg := tool.Config{
		NodeID:   n.ID,
		Pack:     pack,
		Root:     s.toolRoot,
		StateDir: s.cfg.StateDir,
		RunDir:   s.cfg.RunDir,
		Options:  options,
		Net:      netCfg,
	}
	ep, err := tool.Start(cfg)
	if err != nil {
		nr.machine.To(node.StateCrashed)
		return protocol.StartedNode{}, protocol.Errorf(protocol.CodeNodeSpawnFailed,
			"tool: node %d: %v", n.ID, err)
	}
	nr.tool = ep
	nr.machine.To(node.StateRunning)
	// A tool started after its bridge exists must hot-connect immediately; this
	// is the same late-start seam used by the extnet endpoint.
	if err := s.attachFabricForNode(ll, n.ID); err != nil {
		_ = nr.tool.Stop()
		nr.tool = nil
		nr.machine.To(node.StateCrashed)
		return protocol.StartedNode{}, err
	}
	return protocol.StartedNode{Node: n.ID, State: string(nr.machine.State())}, nil
}

// startPCNode starts the built-in netprobe pack with the same data-plane cage
// as a tool, then bridges its private AF_UNIX CLI into the supervisor console
// port. PC nodes never select config.pack and never enter node.Spawn.
func (s *Server) startPCNode(ll *loadedLab, n *lab.Node, nr *nodeRuntime) (protocol.StartedNode, error) {
	if !s.toolCaps.Supports(tool.KindTool) {
		return protocol.StartedNode{}, protocol.Errorf(protocol.CodeUnsupported,
			"pc: runtime does not support PC nodes: %v", s.toolCaps.Reasons)
	}
	if !s.pcPackOK {
		return protocol.StartedNode{}, protocol.Errorf(protocol.CodeUnsupported,
			"pc: built-in netprobe pack is not installed")
	}
	if nr.tool != nil {
		return protocol.StartedNode{Node: n.ID, ConsolePort: nr.consolePort, State: string(nr.machine.State())}, nil
	}
	if !nr.machine.To(node.StateStarting) {
		return protocol.StartedNode{}, protocol.Errorf(protocol.CodeNodeSpawnFailed,
			"node %d: not in a startable state", n.ID)
	}

	state := lab.PCState{SavedCommands: []string{}}
	if raw := n.Config["pc"]; len(raw) != 0 {
		if err := json.Unmarshal(raw, &state); err != nil {
			nr.machine.To(node.StateCrashed)
			return protocol.StartedNode{}, protocol.Errorf(protocol.CodeBadRequest,
				"pc: node %d has invalid pc state: %v", n.ID, err)
		}
	}
	options, _ := json.Marshal(pcStateEnvelope{PC: clonePCState(state)})

	var netCfg *tool.NetAddrConfig
	if raw := n.Config["net"]; len(raw) != 0 {
		var addr struct {
			IP        string `json:"ip"`
			PrefixLen int    `json:"prefixLen"`
			Gateway   string `json:"gateway"`
		}
		if err := json.Unmarshal(raw, &addr); err != nil {
			nr.machine.To(node.StateCrashed)
			return protocol.StartedNode{}, protocol.Errorf(protocol.CodeBadRequest,
				"pc: node %d has invalid net config: %v", n.ID, err)
		}
		if addr.IP != "" {
			if addr.PrefixLen <= 0 || addr.PrefixLen > 32 {
				addr.PrefixLen = 24
			}
			netCfg = &tool.NetAddrConfig{IP: addr.IP, PrefixLen: addr.PrefixLen, Gateway: addr.Gateway}
		}
	}

	cfg := tool.Config{
		NodeID: n.ID, Pack: s.pcPack, Root: s.toolRoot,
		StateDir: s.cfg.StateDir, RunDir: s.cfg.RunDir,
		Options: options, Net: netCfg, CLISocket: true,
	}
	ep, err := tool.Start(cfg)
	if err != nil {
		nr.machine.To(node.StateCrashed)
		return protocol.StartedNode{}, protocol.Errorf(protocol.CodeNodeSpawnFailed,
			"pc: node %d: %v", n.ID, err)
	}
	conn, err := dialPCConsole(ep.CLISocketPath())
	if err != nil {
		_ = ep.Stop()
		nr.machine.To(node.StateCrashed)
		return protocol.StartedNode{}, protocol.Errorf(protocol.CodeNodeSpawnFailed,
			"pc: node %d CLI socket: %v", n.ID, err)
	}
	bind := s.cfg.ConsoleBind
	if bind == "" {
		bind = "127.0.0.1"
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(nr.consolePort)))
	if err != nil {
		_ = conn.Close()
		_ = ep.Stop()
		nr.machine.To(node.StateCrashed)
		return protocol.StartedNode{}, protocol.Errorf(protocol.CodePortUnavailable,
			"pc: node %d console :%d: %v", n.ID, nr.consolePort, err)
	}
	nr.tool = ep
	nr.proc = node.NewConsoleBridge(conn, n.Name, ln)
	nr.machine.To(node.StateRunning)
	if err := s.attachFabricForNode(ll, n.ID); err != nil {
		_ = nr.proc.Stop()
		_ = nr.tool.Stop()
		nr.proc, nr.tool = nil, nil
		nr.machine.To(node.StateCrashed)
		return protocol.StartedNode{}, err
	}
	s.emit(protocol.EventNodeConsole, protocol.NodeConsoleData{Node: n.ID, ConsolePort: nr.consolePort})
	return protocol.StartedNode{Node: n.ID, ConsolePort: nr.consolePort,
		PID: ep.PID(), State: string(nr.machine.State())}, nil
}

func dialPCConsole(socket string) (net.Conn, error) {
	deadline := time.Now().Add(2 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", socket, 200*time.Millisecond)
		if err == nil {
			return conn, nil
		}
		last = err
		time.Sleep(50 * time.Millisecond)
	}
	return nil, last
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
		// Apply the class RAM floor. node.ram of 0 means "class default" (see
		// lab.Node.RAM) and anything under the floor wedges IOS during init
		// while the process — and therefore the node's state — still looks
		// healthy, so both cases are raised here rather than passed through.
		spec.RAM = node.IOLRAMFor(n.RAM, string(info.Class))
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
	// Scheduled fault intents are runtime-only and must not fire after the
	// endpoint they target has been stopped.
	s.cancelFaultTimersForNode(ll, id)
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
	if nr.tool != nil {
		// handleLabReap (the GUI's "Force clean") already loops stopNode and
		// teardownFabric, so tool endpoints are covered by that path as well.
		if n := ll.findNode(id); n != nil && n.Kind == lab.KindPC {
			// Pull before closing the GUI socket. A failed pull leaves the previous
			// document state intact and is intentionally non-fatal to teardown.
			_ = s.syncPCNode(ll, id)
		}
		if nr.proc != nil {
			_ = nr.proc.Stop()
			nr.proc = nil
		}
		_ = nr.tool.Stop()
		nr.tool = nil
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
	current := ll.docSnapshot()
	candidate := *current
	candidate.Nodes = append(append([]lab.Node{}, current.Nodes...), args.Node)
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
		class := ""
		if info, ok := s.lookupImage(n.Image.ID); ok {
			class = string(info.Class)
		}
		// Same floor buildSpec will apply at spawn — recorded here so the
		// runtime's ram never disagrees with what the node actually launches
		// with (node.add has no warnings channel to report the bump on).
		nr.ram = node.IOLRAMFor(n.RAM, class)
	}
	ll.mu.Lock()
	ll.doc.Nodes = append(ll.doc.Nodes, n)
	ll.nodes[n.ID] = nr
	ll.mu.Unlock()
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
	var orphanTaps []string
	staticTaps := ll.staticTapsSnapshot()
	for _, tap := range staticTaps[args.Node] {
		orphanTaps = append(orphanTaps, tap.tapName)
	}
	s.stopNode(ll, args.Node)
	s.consolePorts.Release(nr.consolePort)

	doc := ll.docSnapshot()
	fabricOK := fabricNodes(doc)
	kept := doc.Links[:0]
	for i := range doc.Links {
		l := &doc.Links[i]
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
	doc.Links = kept
	var nodes []lab.Node
	for _, n := range doc.Nodes {
		if n.ID != args.Node {
			nodes = append(nodes, n)
		}
	}
	doc.Nodes = nodes
	ll.mu.Lock()
	delete(ll.nodes, args.Node)
	ll.doc = doc
	ll.mu.Unlock()
	s.evictStaticTaps(ll, orphanTaps)
	s.refreshFabric(ll)
	s.emit(protocol.EventNodeState, protocol.NodeStateData{Node: args.Node, State: "stopped"})
	return protocol.NodeArgs{LabID: doc.ID, Node: args.Node}, nil
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
	ll.mu.Lock()
	for i := range ll.doc.Nodes {
		if ll.doc.Nodes[i].ID == args.Node && ll.doc.Nodes[i].Image != nil {
			ll.doc.Nodes[i].Image.ID = args.ImageID
			break
		}
	}
	ll.mu.Unlock()
	return protocol.NodeSetImageResult{
		Node:    args.Node,
		ImageID: args.ImageID,
		Class:   string(info.Class),
	}, nil
}

func (s *Server) handleNodeMACs(raw json.RawMessage) (any, error) {
	var args protocol.NodeMACsArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	ll, err := s.currentLab("")
	if err != nil {
		return nil, err
	}
	n := ll.findNode(args.Node)
	nr := ll.get(args.Node)
	if n == nil || nr == nil {
		return nil, protocol.Errorf(protocol.CodeBadRequest, "unknown node %d", args.Node)
	}

	interfaces := lab.Interfaces(*n)
	macs := make([]protocol.NodeMAC, 0, len(interfaces))
	switch n.Kind {
	case lab.KindVPCS:
		macs = append(macs, protocol.NodeMAC{
			Interface: interfaces[0],
			MAC:       node.VPCSMAC(args.Node, 0),
			Source:    "derived",
			State:     "known",
		})
	case lab.KindPC, lab.KindTool:
		for _, iface := range interfaces {
			mac := protocol.NodeMAC{Interface: iface, State: "unknown"}
			if nr.machine.State() != node.StateRunning {
				mac.Reason = "node not running"
			} else if address, readErr := readNetnsMAC(args.Node); readErr != nil {
				mac.Reason = "MAC unavailable"
			} else {
				mac.MAC = address
				mac.Source = "read"
				mac.State = "known"
			}
			macs = append(macs, mac)
		}
	case lab.KindIOL:
		macs = make([]protocol.NodeMAC, len(interfaces))
		rowForInterface := make(map[string]int, len(interfaces))
		for i, iface := range interfaces {
			macs[i] = protocol.NodeMAC{Interface: iface, State: "unknown"}
			if parsed, parseErr := netmap.ParseIface(iface); parseErr == nil {
				rowForInterface[parsed.String()] = i
				if parsed.Type == netmap.Serial {
					macs[i].Reason = "interface has no IEEE MAC address"
				} else {
					macs[i].Reason = "hardware address not reported by IOS"
				}
			} else {
				macs[i].Reason = "hardware address not reported by IOS"
			}
		}

		if nr.proc == nil || nr.machine == nil || nr.machine.State() != node.StateRunning {
			for i := range macs {
				macs[i].Reason = "node not running"
			}
			break
		}
		out, showErr := s.runShow(context.Background(), ll, args.Node,
			"show interfaces | include ^[A-Za-z].* is |Hardware is .*address is")
		if showErr != nil {
			for i := range macs {
				macs[i].Reason = "MAC unavailable (console busy or unreachable)"
			}
			break
		}
		for _, parsedMAC := range painter.ParseInterfaceMACs(out) {
			parsedIface, parseErr := netmap.ParseIface(parsedMAC.Interface)
			if parseErr != nil {
				continue
			}
			row, ok := rowForInterface[parsedIface.String()]
			if !ok {
				continue
			}
			macs[row].MAC = parsedMAC.MAC
			macs[row].Source = "read"
			macs[row].State = "known"
			macs[row].Reason = ""
		}
		break

	case lab.KindNAT:
		for _, iface := range interfaces {
			macs = append(macs, protocol.NodeMAC{
				Interface: iface,
				State:     "unknown",
				Reason:    "not tracked for the NAT gateway",
			})
		}
	}
	return protocol.NodeMACsResult{Node: args.Node, MACs: macs}, nil
}

func readNetnsMAC(nodeID int) (string, error) {
	argv := tool.NetnsExecArgs(nodeID, []string{"cat", "/sys/class/net/" + tool.GuestIface + "/address"})
	cmd := exec.Command(argv[0], argv[1:]...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := tool.Registry.StartAndAdd(cmd.Start, func() int { return cmd.Process.Pid })
	if err == nil {
		err = cmd.Wait()
		tool.Registry.Remove(cmd.Process.Pid)
	}
	if err != nil {
		return "", fmt.Errorf("read guest MAC: %w: %s", err, strings.TrimSpace(output.String()))
	}
	address := strings.ToLower(strings.TrimSpace(output.String()))
	if address == "" {
		return "", fmt.Errorf("read guest MAC: empty address")
	}
	return address, nil
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
	doc := ll.docSnapshot()
	for i := range doc.Links {
		if doc.Links[i].ID == args.Link.ID {
			doc.Links[i] = args.Link
			replaced = true
			break
		}
	}
	if !replaced {
		doc.Links = append(doc.Links, args.Link)
	}
	ll.mu.Lock()
	ll.doc = doc
	ll.mu.Unlock()

	// The HOT-CONNECT path. Every interface already has its own static tap
	// (created at boot, topology-independent NETMAP), so wiring this link is a
	// pure runtime bridge-attach that never touches a running node — no relay, no
	// port churn, no restart. Refresh the fabric (so a newly reconnected interface
	// gets/keeps its static tap) then ensure the bridge + attach every member's
	// tap (both idempotent). An unlinked node whose tap isn't up yet is attached
	// by startFabric.
	s.refreshFabric(ll)
	if err := s.startFabric(ll, nil); err != nil {
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
	doc := ll.docSnapshot()
	if isFabricLink(&args.Link, fabricNodes(doc)) {
		s.detachFabricLink(ll, &args.Link)
	}
	ll.mu.Lock()
	delete(ll.linkFaults, args.Link.ID)
	ll.mu.Unlock()
	// Mirror the removal into the loaded doc (see handleLinkAdd's upsert) so the
	// wiring a later start/refresh derives never resurrects this link.
	kept := doc.Links[:0]
	for _, l := range doc.Links {
		if l.ID != args.Link.ID {
			kept = append(kept, l)
		}
	}
	doc.Links = kept
	ll.mu.Lock()
	ll.doc = doc
	ll.mu.Unlock()
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
		for _, n := range ll.docSnapshot().Nodes {
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
	if args.Proto == "stp" && args.VLAN <= 0 {
		// STP is per-VLAN with exactly one root per VLAN: a paint must target
		// one VLAN (chosen via painter.stpVlans) rather than falling back to
		// "whichever VLAN block came first", which is what produced the old
		// two-root-crowns bug.
		return nil, protocol.Errorf(protocol.CodeBadRequest, "painter.collect: proto \"stp\" requires a positive vlan (pick one via painter.stpVlans first)")
	}
	ll, err := s.currentLab(args.LabID)
	if err != nil {
		return nil, err
	}
	return s.painterCollect(context.Background(), ll, args)
}

// handlePainterSTPVlans enumerates the STP-enabled VLAN instances on ONE node
// — step 1 of the STP painter flow (pick a node -> pick a VLAN -> paint every
// bridge's tree for that VLAN). The heavy lifting is in the platform-specific
// painterSTPVlans (Linux scrapes the console; the Windows stub reports
// Linux-only).
func (s *Server) handlePainterSTPVlans(raw json.RawMessage) (any, error) {
	var args protocol.PainterVlansArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	ll, err := s.currentLab(args.LabID)
	if err != nil {
		return nil, err
	}
	return s.painterSTPVlans(context.Background(), ll, args.NodeID)
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
	doc := ll.docSnapshot()
	res := protocol.StatusResult{
		LabID: doc.ID,
		Nodes: []protocol.StatusNode{},
		Links: []protocol.StatusLink{},
	}
	for _, dn := range doc.Nodes {
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
			if sn.PID == 0 && nr.tool != nil {
				sn.PID = nr.tool.PID()
			}
		}
		res.Nodes = append(res.Nodes, sn)
	}
	ll.mu.Lock()
	for _, dl := range doc.Links {
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
	return netmap.BuildStatic(staticNetmapEntries(ll.staticTapsSnapshot()))
}
