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
