package protocol

import (
	"encoding/json"

	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
)

// This file defines the arg and result payloads for every protocol verb, so
// they marshal to exactly the shapes documented in docs/protocol.md.

// --- hello ---

// HelloArgs is the hello request payload.
type HelloArgs struct {
	Client string `json:"client"`
}

// HelloResult is the hello response payload.
type HelloResult struct {
	Supervisor string   `json:"supervisor"`
	Runtime    string   `json:"runtime"`
	Arch       string   `json:"arch"`
	Features   []string `json:"features"`
	// Egress reports the runtime's internet-egress capability for the NAT node:
	// "slirp" means QEMU user-mode slirp (DHCP + outbound TCP work through NAT,
	// but ping/traceroute to the internet do NOT); "routed" means a full path
	// (ICMP/traceroute work). "routed" is the permissive default on any runtime
	// that isn't detected as slirp, so the NAT node is only badged when it truly
	// can't pass ICMP.
	Egress string `json:"egress"`
	// EgressNote is a short human explanation of the egress limitation, present
	// only when Egress == "slirp".
	EgressNote string `json:"egressNote,omitempty"`
}

// --- image.list / image.register ---

// ImageInfo describes a registered image.
type ImageInfo struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Class    string `json:"class"`
	Arch     string `json:"arch"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

// ImageListResult is the image.list response payload.
type ImageListResult struct {
	Images []ImageInfo `json:"images"`
}

// ImageRegisterArgs is the image.register request payload.
//
// The Hint* fields are an OPTIONAL client-asserted fingerprint (filled in by
// the Windows launcher, which persists fingerprints across the ephemeral
// guest disk — see tools/iolab-launcher/imagecache.go). The server only
// trusts them when a cheap os.Stat of Path shows the file's current
// (size, mtime) still matches HintSize/HintMTimeNs; any mismatch (or a
// missing hint) falls back to a full re-hash via image.Inspect. This lets a
// re-uploaded-but-unchanged image skip re-hashing a multi-hundred-MB file
// inside the guest without ever trusting an unverified claim.
type ImageRegisterArgs struct {
	Path string `json:"path"`

	HintSize    int64  `json:"hintSize,omitempty"`
	HintMTimeNs int64  `json:"hintMtimeNs,omitempty"`
	HintSHA256  string `json:"hintSha256,omitempty"`
	HintArch    string `json:"hintArch,omitempty"`
	HintClass   string `json:"hintClass,omitempty"`
}

// ImageRegisterResult is the image.register response payload.
type ImageRegisterResult struct {
	ID     string `json:"id"`
	Class  string `json:"class"`
	Arch   string `json:"arch"`
	SHA256 string `json:"sha256"`
}

// --- lab.load ---

// LabLoadArgs is the lab.load request payload.
type LabLoadArgs struct {
	Lab lab.Lab `json:"lab"`
}

// NodeConsole pairs a node id with its allocated console port.
type NodeConsole struct {
	ID          int `json:"id"`
	ConsolePort int `json:"consolePort"`
}

// LabLoadResult is the lab.load response payload.
type LabLoadResult struct {
	LabID    string        `json:"labId"`
	Nodes    []NodeConsole `json:"nodes"`
	Warnings []string      `json:"warnings"`
	// Adopted is true when this lab.load matched the already-running lab
	// (same id, same topology) and was serviced WITHOUT tearing anything
	// down — see handleLabLoad's adopt path in the server package. The
	// returned Nodes carry the EXISTING runtime's console ports, not freshly
	// allocated ones. Omitted (false) on every ordinary load/reload, so old
	// clients that don't look at this field see no behavioural difference.
	Adopted bool `json:"adopted,omitempty"`
}

// --- lab.saveDoc / lab.listDocs / lab.getDoc / lab.deleteDoc ---
//
// These verbs are the durable lab-document store (distinct from the runtime
// lab.load/lab.start lifecycle). The document is carried as a raw JSON message
// so it round-trips byte-exact and preserves any fields the supervisor's lab
// struct does not model.

// LabSaveDocArgs is the lab.saveDoc request payload: the full lab document as
// text (YAML — iolbox's native lab format; JSON is also accepted on read for
// back-compat). The supervisor stores it verbatim and does not parse it beyond
// extracting the id for the filename.
type LabSaveDocArgs struct {
	Lab string `json:"lab"`
}

// LabSaveDocResult is the lab.saveDoc response payload.
type LabSaveDocResult struct {
	ID string `json:"id"`
}

// LabListDocsResult is the lab.listDocs response payload: every stored doc as
// its raw on-disk text (YAML or legacy JSON), parsed by the client.
type LabListDocsResult struct {
	Labs []string `json:"labs"`
}

// LabGetDocArgs is the lab.getDoc / lab.deleteDoc request payload.
type LabGetDocArgs struct {
	LabID string `json:"labId"`
}

// LabGetDocResult is the lab.getDoc response payload: the stored doc text.
type LabGetDocResult struct {
	Lab string `json:"lab"`
}

// --- lab.start / lab.stop / node.* ---

// LabSelectArgs targets all nodes (Nodes nil) or a subset of a lab.
type LabSelectArgs struct {
	LabID string `json:"labId"`
	Nodes []int  `json:"nodes"`
}

// StartedNode describes a node that transitioned to running.
type StartedNode struct {
	Node        int    `json:"node"`
	ConsolePort int    `json:"consolePort"`
	PID         int    `json:"pid"`
	State       string `json:"state"`
}

// StartResult is the lab.start/node.start response payload.
type StartResult struct {
	Started []StartedNode `json:"started"`
}

// NodeArgs targets a single node in a lab.
type NodeArgs struct {
	LabID string `json:"labId"`
	Node  int    `json:"node"`
}

// NodeMACsArgs requests the current per-interface MAC facts for one node.
// Learned is reserved for the opt-in learned-IOL display and defaults false.
type NodeMACsArgs struct {
	Node    int  `json:"node"`
	Learned bool `json:"learned"`
}

// NodeMAC is one interface's link-layer address for a node, with the PROVENANCE
// that licenses reporting it. There is no reading of this struct that yields a
// MAC the supervisor did not either compute from a flag it passed, read from the
// kernel, or positively learn from observed traffic.
//
// Source:
//
//	"derived" - computed from an argument this supervisor passed to the node
//	            (VPCS -m; see node.VPCSMAC). Valid even while the node is stopped.
//	"read"    - read from the kernel for a device this supervisor created
//	            (a netns node's GuestIface). Requires the node to be running.
//	"learned" - observed as the single source MAC on this endpoint's tap
//	            (P6 Batch 7's dirstat attribution). Requires traffic AND the
//	            learned-MAC DISPLAY opt-in. The supervisor learns either way;
//	            the opt-in gates only whether this handler reports it.
//	""        - nothing is known; State says why.
//
// State:
//
//	"known"     - MAC is set and is the interface's address.
//	"unknown"   - not knowable right now; Reason carries a short human phrase.
//	"ambiguous" - the endpoint relays for other devices, so no single address can
//	              be attributed to it (11b only; see P6 plan §7.3.2).
//	"disabled"  - knowable in principle, but the learned-MAC display opt-in is
//	              off (11b only). NOT a statement that learning is off.
type NodeMAC struct {
	Interface string `json:"interface"`        // the lab document's spelling: e0/0, eth0, eth1
	MAC       string `json:"mac,omitempty"`    // lowercase colon-separated; set iff State=="known"
	Source    string `json:"source,omitempty"` // "derived" | "read" | "learned"
	State     string `json:"state"`            // "known" | "unknown" | "ambiguous" | "disabled"
	Reason    string `json:"reason,omitempty"` // short phrase for the UI, e.g. "node not running"
}

type NodeMACsResult struct {
	Node int       `json:"node"`
	MACs []NodeMAC `json:"macs"`
}

// PCStateSyncArgs asks the supervisor to pull durable state from one running
// PC, or every running PC in the selected lab when Node is omitted.
type PCStateSyncArgs struct {
	LabID string `json:"labId"`
	Node  *int   `json:"node,omitempty"`
}

// PCStateData is the authoritative PC state mirrored into the GUI lab model.
type PCStateData struct {
	Node  int          `json:"node"`
	State *lab.PCState `json:"state"`
	Stale bool         `json:"stale"`
}

// PCStateSyncResult is the response to pc.syncState.
type PCStateSyncResult struct {
	States []PCStateData `json:"states"`
}

// --- node.add / node.remove ---
//
// Incremental topology sync for nodes, the counterpart of link.add/link.remove:
// the GUI edits its local doc live and mirrors each change here so the loaded
// lab always knows every node — without these, a node dropped onto an
// already-loaded lab was UNKNOWN to the supervisor until the next lab.load
// (page refresh) and could never start.

// NodeAddArgs carries the full doc node to register with the loaded lab.
type NodeAddArgs struct {
	LabID string   `json:"labId"`
	Node  lab.Node `json:"node"`
}

// NodeAddResult echoes the node id with its allocated console port (same shape
// as one lab.load NodeConsole entry).
type NodeAddResult struct {
	Node        int `json:"node"`
	ConsolePort int `json:"consolePort"`
}

// --- lab.wipe ---

// LabWipeArgs targets all nodes (Nodes nil) or a subset of a lab for a wipe:
// stop each node and delete its persisted per-node NVRAM state.
type LabWipeArgs struct {
	LabID string `json:"labId"`
	Nodes []int  `json:"nodes"`
}

// LabWipeResult lists the node ids that were wiped.
type LabWipeResult struct {
	Wiped []int `json:"wiped"`
}

// --- lab.reap ---

// ReapResult reports how many tracked nodes a force-clean reap stopped.
type ReapResult struct {
	Reaped int `json:"reaped"`
}

// --- node.setImage ---

// NodeSetImageArgs is the node.setImage request payload.
type NodeSetImageArgs struct {
	LabID   string `json:"labId"`
	Node    int    `json:"node"`
	ImageID string `json:"imageId"`
}

// NodeSetImageResult is the node.setImage response payload.
type NodeSetImageResult struct {
	Node    int    `json:"node"`
	ImageID string `json:"imageId"`
	Class   string `json:"class"`
}

// --- link.add / link.remove ---

// LinkArgs carries a link document for add/remove.
type LinkArgs struct {
	LabID string   `json:"labId"`
	Link  lab.Link `json:"link"`
}

// LinkFaultArgs carries a complete fault replacement. Fault=nil clears the
// persisted definition and any currently-applied qdiscs/administrative state.
// afterSec and forSec are runtime-only scheduling controls and are never
// written into the lab document.
type LinkFaultArgs struct {
	LabID    string         `json:"labId"`
	Link     int            `json:"link"`
	Fault    *lab.LinkFault `json:"fault"`
	AfterSec float64        `json:"afterSec,omitempty"`
	ForSec   float64        `json:"forSec,omitempty"`
}

// --- capture.start / capture.stop ---

// CaptureArgs is the capture.start/stop request payload.
type CaptureArgs struct {
	LabID string `json:"labId"`
	Link  int    `json:"link"`
	Mode  string `json:"mode,omitempty"`
	File  string `json:"file,omitempty"`
}

// CaptureResult is the capture.start response payload.
type CaptureResult struct {
	Link        int    `json:"link"`
	CapturePort int    `json:"capturePort"`
	File        string `json:"file,omitempty"`
}

// --- config.save / config.extract ---

// ConfigArgs targets nodes for NVRAM config extraction.
type ConfigArgs struct {
	LabID string `json:"labId"`
	Nodes []int  `json:"nodes"`
}

// NodeConfig is one node's extracted startup-config.
type NodeConfig struct {
	Node          int    `json:"node"`
	StartupConfig string `json:"startupConfig"`
}

// ConfigResult is the config.save/extract response payload.
type ConfigResult struct {
	Configs []NodeConfig `json:"configs"`
}

// --- painter (topology-decision overlays) ---

// PainterArgs targets the painter.collect verb: which protocol to scrape and,
// for the routing protocols, the destination to trace toward.
type PainterArgs struct {
	LabID string `json:"labId"`
	// Proto is one of "stp", "ospf", "eigrp", "bgp".
	Proto string `json:"proto"`
	// Dest is the routing destination (a prefix "10.0.0.0/24", a host
	// "10.0.0.1", or a nodeId reference the caller has resolved to an address).
	// Ignored for STP. Optional for OSPF/EIGRP (path highlight only when set).
	Dest string `json:"dest,omitempty"`
	// VLAN scopes an STP collect to one VLAN's spanning tree (proto == "stp"
	// only). STP is per-VLAN with exactly one root per VLAN, so a paint always
	// targets a single VLAN chosen via the painter.stpVlans step; VLAN==0 is
	// invalid for a "stp" collect (the handler rejects it) rather than
	// silently falling back to "first VLAN found", which is what produced the
	// old two-roots-per-lab bug.
	VLAN int `json:"vlan,omitempty"`
	// Nodes optionally restricts the scrape to these node ids; empty = all
	// running IOL nodes in the lab (all L2 bridges, auto-queried for the
	// chosen VLAN's tree).
	Nodes []int `json:"nodes,omitempty"`
}

// PainterVlansArgs targets the painter.stpVlans verb: enumerate the
// STP-enabled VLAN instances on ONE node, the first step of the painter STP
// flow (pick a node -> pick a VLAN -> auto-query every bridge for that VLAN).
type PainterVlansArgs struct {
	LabID  string `json:"labId"`
	NodeID int    `json:"nodeId"`
}

// PainterVlan is one STP-enabled VLAN instance on the queried node.
type PainterVlan struct {
	ID   int    `json:"id"`
	Name string `json:"name,omitempty"`
}

// PainterVlansResult is the painter.stpVlans response. Running=false / an
// empty Vlans list (with Hint set) covers a stopped node, an L3-only node, or
// a node with no STP configured — never an error, so the frontend can render
// a clear "no VLANs with STP here" state.
type PainterVlansResult struct {
	Node    int           `json:"node"`
	Running bool          `json:"running"`
	Vlans   []PainterVlan `json:"vlans"`
	Hint    string        `json:"hint,omitempty"`
}

// PainterNode is one node's painter result. Exactly one of the protocol-shaped
// fields is populated, matching the requested proto. A node that is not running,
// has no data, or errored carries Running=false / an empty payload plus a
// human-readable Hint (never fabricated data).
type PainterNode struct {
	Node    int    `json:"node"`
	Running bool   `json:"running"`
	Hint    string `json:"hint,omitempty"`

	// STP result (proto == "stp").
	STP *PainterSTP `json:"stp,omitempty"`
	// OSPF result (proto == "ospf").
	OSPF *PainterOSPF `json:"ospf,omitempty"`
	// EIGRP result (proto == "eigrp").
	EIGRP *PainterEIGRP `json:"eigrp,omitempty"`
	// BGP result (proto == "bgp").
	BGP *PainterBGP `json:"bgp,omitempty"`
}

// PainterResult is the painter.collect response: one entry per targeted node,
// plus the echoed proto/dest/vlan so the frontend knows what snapshot it holds.
type PainterResult struct {
	Proto string `json:"proto"`
	Dest  string `json:"dest,omitempty"`
	// VLAN echoes the requested VLAN for a "stp" collect (omitted otherwise).
	VLAN  int           `json:"vlan,omitempty"`
	Nodes []PainterNode `json:"nodes"`
}

// PainterSTPPort is one STP port's decision at a link endpoint.
type PainterSTPPort struct {
	Interface     string `json:"interface"`
	InterfaceNorm string `json:"interfaceNorm"`
	Role          string `json:"role"`  // Root|Desg|Altn|Back
	State         string `json:"state"` // FWD|BLK|LRN|LIS|DIS
	Cost          int    `json:"cost"`
	Prio          int    `json:"prio,omitempty"`
	Blocked       bool   `json:"blocked"`
	Reason        string `json:"reason,omitempty"`
}

// PainterSTP is a node's spanning-tree decision for the ONE VLAN the paint
// request targeted (see PainterArgs.VLAN). IsRoot is true on at most one node
// across the whole painter.collect response — the node whose BridgeID equals
// RootID.
type PainterSTP struct {
	VLAN     int              `json:"vlan,omitempty"`
	RootID   string           `json:"rootId,omitempty"`
	BridgeID string           `json:"bridgeId,omitempty"`
	IsRoot   bool             `json:"isRoot"`
	RootCost int              `json:"rootCost,omitempty"`
	RootPort string           `json:"rootPort,omitempty"`
	Ports    []PainterSTPPort `json:"ports"`
}

// PainterOSPFNeighbor is one OSPF adjacency.
type PainterOSPFNeighbor struct {
	NeighborID    string `json:"neighborId"`
	State         string `json:"state"`
	Role          string `json:"role,omitempty"` // DR|BDR|DROTHER
	Address       string `json:"address,omitempty"`
	Interface     string `json:"interface"`
	InterfaceNorm string `json:"interfaceNorm"`
}

// PainterRoute is a winning route toward the requested destination (OSPF).
type PainterRoute struct {
	Prefix        string `json:"prefix,omitempty"`
	NextHop       string `json:"nextHop,omitempty"`
	Interface     string `json:"interface,omitempty"`
	InterfaceNorm string `json:"interfaceNorm,omitempty"`
	Cost          int    `json:"cost,omitempty"`
}

// PainterOSPF is a node's OSPF decision.
type PainterOSPF struct {
	Neighbors []PainterOSPFNeighbor `json:"neighbors"`
	Route     *PainterRoute         `json:"route,omitempty"`
}

// PainterEIGRPPath is a successor / feasible-successor path.
type PainterEIGRPPath struct {
	NextHop           string `json:"nextHop"`
	Interface         string `json:"interface,omitempty"`
	InterfaceNorm     string `json:"interfaceNorm,omitempty"`
	FD                int64  `json:"fd"`
	RD                int64  `json:"rd"`
	Successor         bool   `json:"successor"`
	FeasibleSuccessor bool   `json:"feasibleSuccessor"`
}

// PainterEIGRP is a node's EIGRP topology decision toward the destination.
type PainterEIGRP struct {
	Prefix  string             `json:"prefix,omitempty"`
	FD      int64              `json:"fd,omitempty"`
	Paths   []PainterEIGRPPath `json:"paths"`
	NextHop string             `json:"nextHop,omitempty"`
}

// PainterBGPPath is one BGP candidate path for the prefix.
type PainterBGPPath struct {
	NextHop   string `json:"nextHop"`
	ASPath    string `json:"asPath,omitempty"`
	Origin    string `json:"origin,omitempty"`
	Weight    int    `json:"weight,omitempty"`
	LocalPref int    `json:"localPref,omitempty"`
	MED       int    `json:"med,omitempty"`
	Best      bool   `json:"best"`
}

// PainterBGP is a node's BGP best-path decision for the prefix.
type PainterBGP struct {
	Prefix      string           `json:"prefix,omitempty"`
	Paths       []PainterBGPPath `json:"paths"`
	BestNextHop string           `json:"bestNextHop,omitempty"`
	Reason      string           `json:"reason,omitempty"`
}

// --- status ---

// StatusNode is a node entry in a status snapshot.
type StatusNode struct {
	ID          int    `json:"id"`
	State       string `json:"state"`
	ConsolePort int    `json:"consolePort"`
	PID         int    `json:"pid"`
	RAM         int    `json:"ram"`
	Image       string `json:"image"`
}

// StatusLink is a link entry in a status snapshot.
type StatusLink struct {
	ID          int  `json:"id"`
	CapturePort *int `json:"capturePort,omitempty"`
}

// StatusResult is the status response payload.
type StatusResult struct {
	LabID string       `json:"labId"`
	Nodes []StatusNode `json:"nodes"`
	Links []StatusLink `json:"links"`
}

// ToolListPacksArgs is the tool.listPacks request payload.
type ToolListPacksArgs struct{}

// ToolListPacksResult is the tool.listPacks response payload.
type ToolListPacksResult struct {
	Packs []ToolPackInfo `json:"packs"`
}

// ToolPackInfo is the wire-safe palette metadata for one installed tool pack.
type ToolPackInfo struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Icon      string           `json:"icon"`
	Transport string           `json:"transport"`
	Groups    []string         `json:"groups"`
	Modules   []ToolModuleInfo `json:"modules"`
}

// ToolModuleInfo is the wire-safe palette metadata for one tool-pack module.
type ToolModuleInfo struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Group string `json:"group"`
}

// --- event payloads ---

// NodeStateData is the node.state event payload.
type NodeStateData struct {
	Node  int    `json:"node"`
	State string `json:"state"`
}

// NodeConsoleData is the node.console event payload.
type NodeConsoleData struct {
	Node        int `json:"node"`
	ConsolePort int `json:"consolePort"`
}

// PCStateEventData is the node.pcState event payload.
type PCStateEventData = PCStateData

// LinkData is the link.up/link.down event payload.
type LinkData struct {
	Link int `json:"link"`
}

// LinkFaultData is the authoritative, separate link.fault event payload.
type LinkFaultData struct {
	Link   int            `json:"link"`
	Fault  *lab.LinkFault `json:"fault"`
	Active bool           `json:"active"`
	Reason string         `json:"reason,omitempty"`
}

// CaptureData is the capture.started/stopped event payload.
type CaptureData struct {
	Link        int `json:"link"`
	CapturePort int `json:"capturePort"`
}

// EndpointAttrib is the per-endpoint source-MAC attribution hint for one
// fabric link. EndpointIndex is the lab document endpoint index, not the
// position in EpAttrib: the slice is sparse when an endpoint has no tap.
// "single" is the only state that licenses naming a node. "ambiguous" means
// the endpoint forwarded more than one source MAC during the learning window,
// so its MAC is intentionally withheld; "none" means no usable observation
// remains. A MAC that appears on both endpoints is withheld from both sides.
type EndpointAttrib struct {
	EndpointIndex int    `json:"endpointIndex"`
	State         string `json:"state"`
	MAC           string `json:"mac,omitempty"`
}

// LinkStatsData is the link.stats event payload: per-link forwarded throughput
// over the last sampling interval. Only bridged links have a relay and thus
// stats; native (same-host IOL<->IOL) links produce none.
type LinkStatsData struct {
	Link int     `json:"link"`
	FPS  float64 `json:"fps"`
	BPS  uint64  `json:"bps"`
	// Protos is the per-protocol frames/sec breakdown over the same interval,
	// keyed by protocol label (ARP, TCP, OSPF, STP, CDP, ...). Only non-zero
	// entries, capped to the top 6 by fps; omitted entirely (nil) when there is
	// nothing to report. Each value is rounded to one decimal like FPS. The
	// overlapping "DOT1Q" label is excluded here so Protos still sums to FPS.
	Protos map[string]float64 `json:"protos,omitempty"`
	// ProtosDir is the per-direction per-protocol frames/sec breakdown over the
	// same interval, keyed by protocol label. Each value is [fps sourced from
	// endpoint 0, fps sourced from endpoint 1], where endpoint order matches the
	// lab link's doc endpoints order. Populated for fabric links from the
	// always-on per-endpoint-tap classifier: a frame is attributed to the
	// endpoint whose tap received it (the node behind that tap sent it). Only
	// labels with a nonzero rate in either direction; one-decimal rounding. A
	// frame counts once, under one label, in one direction, so this map does NOT
	// sum to FPS in general. Omitted (nil) when there's nothing to report.
	ProtosDir map[string][2]float64 `json:"protosDir,omitempty"`
	// ProtosSubtypeDir is the same directional breakdown one level deeper: for
	// each label that carries a decodable packet-type subtype (BGP open/update/
	// notification/keepalive/route-refresh; ICMP echo-request/echo-reply/
	// unreachable/time-exceeded/redirect/other; OSPF hello/db-desc/ls-request/
	// ls-update/ls-ack; EIGRP hello/update/query/reply/request; ARP request/
	// reply), label -> subtype -> [ep0 fps, ep1 fps]. Only subtypes with a
	// nonzero rate; frames whose subtype couldn't be decoded contribute to
	// ProtosDir under the label but appear under no subtype here. Omitted (nil)
	// when there's nothing to report.
	ProtosSubtypeDir map[string]map[string][2]float64 `json:"protosSubtypeDir,omitempty"`
	// EpAttrib is omitted when the per-endpoint classifier could not be opened.
	// When present it contains explicit document endpoint indexes and the
	// singular-MAC learning state: single, ambiguous, or none. It is a hint,
	// never a general CAM table; the browser names a node only for a unique
	// single MAC match at event-creation time.
	EpAttrib []EndpointAttrib `json:"epAttrib,omitempty"`
}

// HostStatsData is the host.stats event payload: the runtime VM's resource
// utilisation, pushed every sampling interval so the GUI can show a live
// CPU/RAM/disk monitor for the host actually executing the IOL processes.
// CPUPct is aggregate 0-100 across all cores; memory/disk are bytes.
type HostStatsData struct {
	CPUPct   float64 `json:"cpuPct"`
	MemUsed  uint64  `json:"memUsed"`
	MemTotal uint64  `json:"memTotal"`
	DiskUsed uint64  `json:"diskUsed"`
	DiskTot  uint64  `json:"diskTotal"`
	Cores    int     `json:"cores"`
}

// LogData is the log event payload.
type LogData struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	Node    *int   `json:"node,omitempty"`
}

// NewEvent builds an Event from a name and a payload value.
func NewEvent(name string, data any) *Event {
	var raw json.RawMessage
	if data != nil {
		raw = mustMarshal(data)
	}
	return &Event{Event: name, Data: raw}
}
