package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeRequest(t *testing.T) {
	in := `{"id":"abc","op":"hello","args":{"client":"iolab-gui/0.1.0"}}` + "\n"
	dec := NewDecoder(strings.NewReader(in))
	req, err := dec.ReadRequest()
	if err != nil {
		t.Fatalf("ReadRequest: %v", err)
	}
	if req.ID != "abc" || req.Op != "hello" {
		t.Fatalf("bad request: %+v", req)
	}
	var a HelloArgs
	if err := json.Unmarshal(req.Args, &a); err != nil {
		t.Fatalf("args: %v", err)
	}
	if a.Client != "iolab-gui/0.1.0" {
		t.Fatalf("client=%q", a.Client)
	}
}

func TestDecodeSkipsBlankAndCRLF(t *testing.T) {
	in := "\r\n" + `{"id":"1","op":"status"}` + "\r\n"
	dec := NewDecoder(strings.NewReader(in))
	req, err := dec.ReadRequest()
	if err != nil {
		t.Fatalf("ReadRequest: %v", err)
	}
	if req.ID != "1" || req.Op != "status" {
		t.Fatalf("bad request: %+v", req)
	}
}

func TestDecodeLongLine(t *testing.T) {
	big := strings.Repeat("x", 1<<18)
	in := `{"id":"1","op":"lab.load","args":{"blob":"` + big + `"}}` + "\n"
	dec := NewDecoder(strings.NewReader(in))
	req, err := dec.ReadRequest()
	if err != nil {
		t.Fatalf("ReadRequest long: %v", err)
	}
	if req.Op != "lab.load" {
		t.Fatalf("op=%q", req.Op)
	}
}

func TestEncodeResponseAndEvent(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.WriteResponse(&Response{ID: "1", OK: true, Result: json.RawMessage(`{"x":1}`)}); err != nil {
		t.Fatal(err)
	}
	if err := enc.WriteEvent(NewEvent(EventNodeState, NodeStateData{Node: 3, State: "running"})); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}
	var resp Response
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil || !resp.OK {
		t.Fatalf("resp line: %v %q", err, lines[0])
	}
	var ev Event
	if err := json.Unmarshal([]byte(lines[1]), &ev); err != nil || ev.Event != "node.state" {
		t.Fatalf("event line: %v %q", err, lines[1])
	}
	var nsd NodeStateData
	if err := json.Unmarshal(ev.Data, &nsd); err != nil || nsd.Node != 3 || nsd.State != "running" {
		t.Fatalf("event data: %v %+v", err, nsd)
	}
}

func TestDispatch(t *testing.T) {
	d := NewDispatcher()
	d.Handle("echo", func(args json.RawMessage) (any, error) {
		return map[string]string{"got": string(args)}, nil
	})
	d.Handle("boom", func(args json.RawMessage) (any, error) {
		return nil, NewError(CodeSchemaInvalid, "nope")
	})

	resp := d.Dispatch(&Request{ID: "1", Op: "echo", Args: json.RawMessage(`"hi"`)})
	if !resp.OK || resp.ID != "1" {
		t.Fatalf("echo: %+v", resp)
	}
	resp = d.Dispatch(&Request{ID: "2", Op: "boom"})
	if resp.OK || resp.Error == nil || resp.Error.Code != CodeSchemaInvalid {
		t.Fatalf("boom: %+v", resp)
	}
	resp = d.Dispatch(&Request{ID: "3", Op: "missing"})
	if resp.OK || resp.Error.Code != CodeUnsupported {
		t.Fatalf("missing: %+v", resp)
	}
}

func TestVerbResultShapes(t *testing.T) {
	// hello result must serialize exactly per protocol.md field names.
	b, _ := json.Marshal(HelloResult{Supervisor: "0.1.0", Runtime: "debian-slim-12", Arch: "x86_64", Features: []string{"nvram"}})
	for _, k := range []string{`"supervisor"`, `"runtime"`, `"arch"`, `"features"`} {
		if !strings.Contains(string(b), k) {
			t.Fatalf("hello result missing %s: %s", k, b)
		}
	}
	// lab.load result
	b, _ = json.Marshal(LabLoadResult{LabID: "L", Nodes: []NodeConsole{{ID: 1, ConsolePort: 9000}}, Warnings: []string{}})
	for _, k := range []string{`"labId"`, `"consolePort"`, `"warnings"`} {
		if !strings.Contains(string(b), k) {
			t.Fatalf("lab.load result missing %s: %s", k, b)
		}
	}
	// capture result omits file when empty
	b, _ = json.Marshal(CaptureResult{Link: 2, CapturePort: 5500})
	if strings.Contains(string(b), `"file"`) {
		t.Fatalf("capture result should omit empty file: %s", b)
	}
}
