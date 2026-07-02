// Package protocol implements the NDJSON control protocol between the iolab GUI
// and the supervisor (see docs/protocol.md). One request, response, or event
// per line, UTF-8, correlated by id.
package protocol

import (
	"encoding/json"
	"fmt"
)

// Request is a client-to-server message: {"id","op","args"}.
type Request struct {
	ID   string          `json:"id"`
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args,omitempty"`
}

// Response is a server-to-client reply correlated to a Request by ID.
// Exactly one of Result / Error is set depending on OK.
type Response struct {
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// Error is a structured failure with a stable code (see docs/protocol.md).
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface so Error values can flow as errors.
func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Event is an unsolicited server-to-client push: {"event","data"}.
type Event struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// Error codes defined by the protocol.
const (
	CodeSchemaInvalid     = "schema_invalid"
	CodeImageNotFound     = "image_not_found"
	CodeImageArchMismatch = "image_arch_mismatch"
	CodeIourcFailed       = "iourc_failed"
	CodeNodeSpawnFailed   = "node_spawn_failed"
	CodePortUnavailable   = "port_unavailable"
	CodeNvramCodecFailed  = "nvram_codec_failed"
	CodeNotLoaded         = "not_loaded"
	CodeUnsupported       = "unsupported"
	// CodeBadRequest is used for malformed frames / unknown verbs; it is a
	// superset error not enumerated in the protocol but needed in practice.
	CodeBadRequest = "bad_request"
)

// Event names defined by the protocol.
const (
	EventNodeState      = "node.state"
	EventNodeConsole    = "node.console"
	EventLinkUp         = "link.up"
	EventLinkDown       = "link.down"
	EventCaptureStarted = "capture.started"
	EventCaptureStopped = "capture.stopped"
	EventLog            = "log"
)

// Errorf builds an *Error with a formatted message.
func Errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// NewError builds an *Error with a literal message.
func NewError(code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// mustMarshal marshals v to JSON, panicking on failure. Used only for values we
// control (protocol results/events), which never fail to marshal.
func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic("protocol: marshal: " + err.Error())
	}
	return b
}
