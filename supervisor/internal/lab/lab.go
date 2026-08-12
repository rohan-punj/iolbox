// Package lab defines the Go representation of the iolbox lab document and its
// validation rules. The struct tags mirror contracts/lab.schema.json exactly so
// the same JSON round-trips between the GUI and the supervisor.
package lab

import "encoding/json"

// Version is the only lab file format version this supervisor understands.
const Version = 1

// Lab is one JSON document describing a full lab: nodes, links, metadata.
type Lab struct {
	Version     int     `json:"version"`
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Created     string  `json:"created,omitempty"`
	Modified    string  `json:"modified,omitempty"`
	Canvas      *Canvas `json:"canvas,omitempty"`
	Nodes       []Node  `json:"nodes"`
	Links       []Link  `json:"links"`
}

// Canvas holds purely presentational view state.
type Canvas struct {
	Zoom       float64 `json:"zoom,omitempty"`
	Pan        *Pan    `json:"pan,omitempty"`
	Background string  `json:"background,omitempty"`
}

// Pan is the canvas offset.
type Pan struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Kind enumerates the node kinds the supervisor can run.
type Kind string

const (
	// KindIOL is a Cisco IOL image (L2 or L3).
	KindIOL Kind = "iol"
	// KindVPCS is a virtual PC (Simulator).
	KindVPCS Kind = "vpcs"
	// KindNAT is a NAT gateway: a supervisor-internal tap with DHCP + MASQUERADE
	// that connects the lab to the outside world (see internal/extnet). Its one
	// connectable interface is "eth0".
	KindNAT Kind = "nat"
	// KindTool is a bundled learning-tool node (e.g. secbench) run as a
	// supervised process tree in a netns + cgroup cage. It has exactly one
	// connectable interface, "eth1"; tool-specific data rides the existing
	// Config map[string]json.RawMessage rather than new top-level Node fields.
	KindTool Kind = "tool"
	// KindPC is the first-class netprobe virtual PC. It uses the same single
	// eth1 data-plane endpoint as a tool, but has a supervisor-owned built-in
	// pack and a console CLI.
	KindPC Kind = "pc"
)

// Node is a single lab device.
type Node struct {
	ID   int     `json:"id"`
	Kind Kind    `json:"kind"`
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Icon string  `json:"icon,omitempty"`
	// Image is required when Kind==iol and ignored for vpcs.
	Image *ImageRef `json:"image,omitempty"`
	// RAM in megabytes. Zero means "use the class default".
	RAM int `json:"ram,omitempty"`
	// Ethernet is the number of Ethernet adapter groups (each = 4 ports in IOL).
	Ethernet *int `json:"ethernet,omitempty"`
	// Serial is the number of Serial adapter groups (each = 4 ports).
	Serial *int `json:"serial,omitempty"`
	// StartupConfig is embedded day-0 IOS CLI, injected into NVRAM at boot.
	StartupConfig string `json:"startupConfig,omitempty"`
	// Config is reserved for kind-specific extras (e.g. vpcs canned commands or
	// a tool pack identity).
	Config map[string]json.RawMessage `json:"config,omitempty"`
}

// Class enumerates the IOL image class hint.
type Class string

const (
	// ClassL2 is an L2 (switching) IOL image.
	ClassL2 Class = "l2"
	// ClassL3 is an L3 (routing) IOL image.
	ClassL3 Class = "l3"
	// ClassUnknown means the class could not be determined.
	ClassUnknown Class = "unknown"
)

// ImageRef references a library image by content id, with a filename fallback
// for portability across machines.
type ImageRef struct {
	// ID is the library image id (sha256 prefix); the node binds to THIS.
	ID string `json:"id"`
	// Filename is the fallback used if ID is not present in the target library.
	Filename string `json:"filename,omitempty"`
	// Class is a cached hint; the supervisor re-detects authoritatively.
	Class Class `json:"class,omitempty"`
}

// LinkType enumerates the link topology.
type LinkType string

const (
	// LinkP2P is a point-to-point link (exactly 2 endpoints, no relay needed).
	LinkP2P LinkType = "p2p"
	// LinkSegment is a shared medium served by a userspace hub.
	LinkSegment LinkType = "segment"
)

// Link is a point-to-point or multi-access segment between node interfaces.
type Link struct {
	ID        int        `json:"id"`
	Type      LinkType   `json:"type,omitempty"`
	Endpoints []Endpoint `json:"endpoints"`
	Capture   *Capture   `json:"capture,omitempty"`
	Fault     *LinkFault `json:"fault,omitempty"`
}

// EffectiveType returns the link type, defaulting to p2p when unset (per schema).
func (l Link) EffectiveType() LinkType {
	if l.Type == "" {
		return LinkP2P
	}
	return l.Type
}

// Capture is a persistent capture intent flag on a link.
type Capture struct {
	Enabled bool   `json:"enabled,omitempty"`
	Mode    string `json:"mode,omitempty"`
}

// LinkFault is the persisted definition of a per-link administrative or
// egress impairment. A nil TargetEndpoint applies an impairment to every
// currently-present endpoint device; an explicit zero targets endpoints[0].
// Runtime activation and timers are supervisor state, not lab JSON.
type LinkFault struct {
	Down           bool    `json:"down,omitempty"`
	DelayMs        float64 `json:"delayMs,omitempty"`
	JitterMs       float64 `json:"jitterMs,omitempty"`
	LossPct        float64 `json:"lossPct,omitempty"`
	RateKbit       int     `json:"rateKbit,omitempty"`
	DuplicatePct   float64 `json:"duplicatePct,omitempty"`
	ReorderPct     float64 `json:"reorderPct,omitempty"`
	TargetEndpoint *int    `json:"targetEndpoint,omitempty"`
	Initial        bool    `json:"initial,omitempty"`
}

// Endpoint is one side of a link: a node id plus an interface string.
type Endpoint struct {
	// Node is a node.id.
	Node int `json:"node"`
	// Interface is 'e0/0'/'s1/1' for IOL, 'eth0' for VPCS/NAT, or 'eth1' for a
	// tool/PC node.
	Interface string `json:"interface"`
}

// PCState is the small durable state shared by the netprobe CLI, its GUI, and
// the lab document. Runtime-only addresses and leases are intentionally not
// persisted here.
type PCState struct {
	DHCP          bool     `json:"dhcp"`
	SavedCommands []string `json:"savedCommands"`
}

// Unmarshal parses a lab document from JSON.
func Unmarshal(data []byte) (*Lab, error) {
	var l Lab
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, err
	}
	return &l, nil
}
