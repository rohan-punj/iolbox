package protocol

import (
	"encoding/json"

	"github.com/rohanpunj/iolab/supervisor/internal/lab"
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
type ImageRegisterArgs struct {
	Path string `json:"path"`
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

// LabSaveDocArgs is the lab.saveDoc request payload: the full lab document.
type LabSaveDocArgs struct {
	Lab json.RawMessage `json:"lab"`
}

// LabSaveDocResult is the lab.saveDoc response payload.
type LabSaveDocResult struct {
	ID string `json:"id"`
}

// LabListDocsResult is the lab.listDocs response payload: every stored doc,
// parsed back from disk as raw JSON.
type LabListDocsResult struct {
	Labs []json.RawMessage `json:"labs"`
}

// LabGetDocArgs is the lab.getDoc / lab.deleteDoc request payload.
type LabGetDocArgs struct {
	LabID string `json:"labId"`
}

// LabGetDocResult is the lab.getDoc response payload: the stored doc.
type LabGetDocResult struct {
	Lab json.RawMessage `json:"lab"`
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
	// lab link's doc endpoints order. Only labels with a nonzero rate in either
	// direction; one-decimal rounding. "DOT1Q" appears here (counting 802.1Q-
	// tagged frames) and overlaps the primary labels, so this map does NOT sum
	// to FPS. Omitted (nil) when there's nothing to report.
	ProtosDir map[string][2]float64 `json:"protosDir,omitempty"`
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
