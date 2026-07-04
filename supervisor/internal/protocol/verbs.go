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
	// Nodes optionally restricts the scrape to these node ids; empty = all
	// running IOL nodes in the lab.
	Nodes []int `json:"nodes,omitempty"`
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
// plus the echoed proto/dest so the frontend knows what snapshot it holds.
type PainterResult struct {
	Proto string        `json:"proto"`
	Dest  string        `json:"dest,omitempty"`
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

// PainterSTP is a node's spanning-tree decision.
type PainterSTP struct {
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

// LinkData is the link.up/link.down event payload.
type LinkData struct {
	Link int `json:"link"`
}

// CaptureData is the capture.started/stopped event payload.
type CaptureData struct {
	Link        int `json:"link"`
	CapturePort int `json:"capturePort"`
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
