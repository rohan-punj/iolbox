package server

import (
	"encoding/json"
	"testing"

	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
)

// captureLabJSON is a 2-node lab (IOL + VPCS) whose single link has
// capture.enabled persisted in the doc — the documented "enable capture and
// restart the lab" flow.
const captureLabJSON = `{"lab":{"version":1,"id":"cap-lab","name":"n","nodes":[
  {"id":0,"kind":"iol","name":"R1","x":0,"y":0,"image":{"id":"img-l3","filename":"L3.bin","class":"l3"}},
  {"id":1,"kind":"vpcs","name":"PC1","x":0,"y":0}],
 "links":[{"id":4,"type":"p2p","capture":{"enabled":true,"mode":"live"},
  "endpoints":[{"node":0,"interface":"e0/0"},{"node":1,"interface":"eth0"}]}]}}`

// TestDocCaptureAutoArmsOnStart pins the item-1 supervisor fix: a plain
// lab.load + lab.start of a doc whose link carries capture.enabled must yield
// (a) a runtime capture port in ll.captures, (b) a bridge-plan relay config
// carrying that port as its pcapng tee, and (c) a capturePort in status — all
// WITHOUT any capture.start call. Before the fix ll.captures stayed empty, the
// relay got no tee, and /capture/{id} 404'd forever after a lab restart.
//
// lab.start is issued with an explicit empty node list so the control-plane
// flow (arm -> plan -> prepare) runs without spawning processes — spawning is
// Linux-only, but the capture arming under test is pure control plane.
func TestDocCaptureAutoArmsOnStart(t *testing.T) {
	s := New(Config{ControlAddr: "127.0.0.1:0", ImageDir: t.TempDir(), RunDir: t.TempDir(), Version: "test"})

	resp := s.Dispatcher().Dispatch(&protocol.Request{ID: "1", Op: "lab.load", Args: json.RawMessage(captureLabJSON)})
	if !resp.OK {
		t.Fatalf("lab.load failed: %+v", resp.Error)
	}
	resp = s.Dispatcher().Dispatch(&protocol.Request{ID: "2", Op: "lab.start",
		Args: json.RawMessage(`{"labId":"cap-lab","nodes":[]}`)})
	if !resp.OK {
		t.Fatalf("lab.start failed: %+v", resp.Error)
	}

	ll, err := s.currentLab("cap-lab")
	if err != nil {
		t.Fatal(err)
	}
	ll.mu.Lock()
	port := ll.captures[4]
	ll.mu.Unlock()
	if port < 5500 {
		t.Fatalf("link 4 not auto-armed: captures port = %d", port)
	}
	cfg, ok := ll.bridge.relayConfigFor(4)
	if !ok {
		t.Fatal("link 4 missing from bridge plan")
	}
	if cfg.CapturePort != port {
		t.Fatalf("relay config tee port %d != armed port %d", cfg.CapturePort, port)
	}

	// status must expose the port so a freshly connected GUI can find it.
	resp = s.Dispatcher().Dispatch(&protocol.Request{ID: "3", Op: "status", Args: json.RawMessage(`{}`)})
	if !resp.OK {
		t.Fatalf("status failed: %+v", resp.Error)
	}
	var st protocol.StatusResult
	if err := json.Unmarshal(resp.Result, &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Links) != 1 || st.Links[0].CapturePort == nil || *st.Links[0].CapturePort != port {
		t.Fatalf("status missing capturePort: %+v", st.Links)
	}

	// Idempotent re-start: the SAME port survives (no leak, no churn).
	resp = s.Dispatcher().Dispatch(&protocol.Request{ID: "4", Op: "lab.start",
		Args: json.RawMessage(`{"labId":"cap-lab","nodes":[]}`)})
	if !resp.OK {
		t.Fatalf("second lab.start failed: %+v", resp.Error)
	}
	ll.mu.Lock()
	port2 := ll.captures[4]
	ll.mu.Unlock()
	if port2 != port {
		t.Fatalf("re-start changed the capture port: %d -> %d", port, port2)
	}
}

// TestCaptureStartReusesAutoArmedPort: an explicit capture.start on a link the
// doc already auto-armed must REUSE the armed port, not allocate a second one
// (which orphaned the first without release and left clients pointing at a
// dead tee).
func TestCaptureStartReusesAutoArmedPort(t *testing.T) {
	s := New(Config{ControlAddr: "127.0.0.1:0", ImageDir: t.TempDir(), RunDir: t.TempDir(), Version: "test"})
	if resp := s.Dispatcher().Dispatch(&protocol.Request{ID: "1", Op: "lab.load", Args: json.RawMessage(captureLabJSON)}); !resp.OK {
		t.Fatalf("lab.load failed: %+v", resp.Error)
	}
	if resp := s.Dispatcher().Dispatch(&protocol.Request{ID: "2", Op: "lab.start",
		Args: json.RawMessage(`{"labId":"cap-lab","nodes":[]}`)}); !resp.OK {
		t.Fatalf("lab.start failed: %+v", resp.Error)
	}
	ll, err := s.currentLab("cap-lab")
	if err != nil {
		t.Fatal(err)
	}
	ll.mu.Lock()
	armed := ll.captures[4]
	ll.mu.Unlock()
	if armed == 0 {
		t.Fatal("expected auto-armed port")
	}

	resp := s.Dispatcher().Dispatch(&protocol.Request{ID: "3", Op: "capture.start",
		Args: json.RawMessage(`{"labId":"cap-lab","link":4,"mode":"live"}`)})
	if !resp.OK {
		t.Fatalf("capture.start failed: %+v", resp.Error)
	}
	var res protocol.CaptureResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		t.Fatal(err)
	}
	if res.CapturePort != armed {
		t.Fatalf("capture.start allocated a second port: armed=%d got=%d", armed, res.CapturePort)
	}
}

// TestLabStopReleasesCaptures pins the release path: a full lab.stop clears the
// runtime captures and returns the port to the allocator, and the next start
// re-arms from the doc (getting the released port back — proof of the release).
func TestLabStopReleasesCaptures(t *testing.T) {
	s := New(Config{ControlAddr: "127.0.0.1:0", ImageDir: t.TempDir(), RunDir: t.TempDir(), Version: "test"})
	if resp := s.Dispatcher().Dispatch(&protocol.Request{ID: "1", Op: "lab.load", Args: json.RawMessage(captureLabJSON)}); !resp.OK {
		t.Fatalf("lab.load failed: %+v", resp.Error)
	}
	if resp := s.Dispatcher().Dispatch(&protocol.Request{ID: "2", Op: "lab.start",
		Args: json.RawMessage(`{"labId":"cap-lab","nodes":[]}`)}); !resp.OK {
		t.Fatalf("lab.start failed: %+v", resp.Error)
	}
	ll, err := s.currentLab("cap-lab")
	if err != nil {
		t.Fatal(err)
	}
	ll.mu.Lock()
	first := ll.captures[4]
	ll.mu.Unlock()
	if first == 0 {
		t.Fatal("expected link 4 armed after start")
	}

	if resp := s.Dispatcher().Dispatch(&protocol.Request{ID: "3", Op: "lab.stop",
		Args: json.RawMessage(`{"labId":"cap-lab"}`)}); !resp.OK {
		t.Fatalf("lab.stop failed: %+v", resp.Error)
	}
	ll.mu.Lock()
	nCaptures := len(ll.captures)
	ll.mu.Unlock()
	if nCaptures != 0 {
		t.Fatalf("lab.stop left %d armed captures", nCaptures)
	}

	// Restart: the allocator hands the released port back — same port proves no leak.
	if resp := s.Dispatcher().Dispatch(&protocol.Request{ID: "4", Op: "lab.start",
		Args: json.RawMessage(`{"labId":"cap-lab","nodes":[]}`)}); !resp.OK {
		t.Fatalf("restart failed: %+v", resp.Error)
	}
	ll.mu.Lock()
	second := ll.captures[4]
	ll.mu.Unlock()
	if second != first {
		t.Fatalf("capture port leaked across stop/start: first=%d second=%d", first, second)
	}
}

// TestLabLoadReleasesOldLabPorts guards the lab-switch cleanup added alongside
// auto-arm: replacing a loaded lab must release the old lab's capture port AND
// its bridge-plan UDP ports back to the allocators (they leaked before).
func TestLabLoadReleasesOldLabPorts(t *testing.T) {
	s := New(Config{ControlAddr: "127.0.0.1:0", ImageDir: t.TempDir(), RunDir: t.TempDir(), Version: "test"})
	if resp := s.Dispatcher().Dispatch(&protocol.Request{ID: "1", Op: "lab.load", Args: json.RawMessage(captureLabJSON)}); !resp.OK {
		t.Fatalf("lab.load failed: %+v", resp.Error)
	}
	if resp := s.Dispatcher().Dispatch(&protocol.Request{ID: "2", Op: "lab.start",
		Args: json.RawMessage(`{"labId":"cap-lab","nodes":[]}`)}); !resp.OK {
		t.Fatalf("lab.start failed: %+v", resp.Error)
	}
	oldLab, err := s.currentLab("cap-lab")
	if err != nil {
		t.Fatal(err)
	}
	oldLab.mu.Lock()
	oldPort := oldLab.captures[4]
	oldLab.mu.Unlock()
	oldUDP := oldLab.bridge.udpPorts()
	if oldPort == 0 || len(oldUDP) == 0 {
		t.Fatalf("old lab not armed/bridged: capture=%d udp=%v", oldPort, oldUDP)
	}

	// Load the same doc under a new id — the old lab must be fully released.
	replacement := `{"lab":{"version":1,"id":"cap-lab-2","name":"n","nodes":[
	  {"id":0,"kind":"iol","name":"R1","x":0,"y":0,"image":{"id":"img-l3","filename":"L3.bin","class":"l3"}},
	  {"id":1,"kind":"vpcs","name":"PC1","x":0,"y":0}],
	 "links":[{"id":4,"type":"p2p","capture":{"enabled":true,"mode":"live"},
	  "endpoints":[{"node":0,"interface":"e0/0"},{"node":1,"interface":"eth0"}]}]}}`
	if resp := s.Dispatcher().Dispatch(&protocol.Request{ID: "3", Op: "lab.load", Args: json.RawMessage(replacement)}); !resp.OK {
		t.Fatalf("replacement lab.load failed: %+v", resp.Error)
	}
	if resp := s.Dispatcher().Dispatch(&protocol.Request{ID: "4", Op: "lab.start",
		Args: json.RawMessage(`{"labId":"cap-lab-2","nodes":[]}`)}); !resp.OK {
		t.Fatalf("replacement lab.start failed: %+v", resp.Error)
	}
	newLab, err := s.currentLab("cap-lab-2")
	if err != nil {
		t.Fatal(err)
	}
	newLab.mu.Lock()
	newPort := newLab.captures[4]
	newLab.mu.Unlock()
	newUDP := newLab.bridge.udpPorts()
	if newPort != oldPort {
		t.Fatalf("old capture port leaked on lab switch: old=%d new=%d", oldPort, newPort)
	}
	for i := range oldUDP {
		if newUDP[i] != oldUDP[i] {
			t.Fatalf("old plan UDP ports leaked on lab switch: old=%v new=%v", oldUDP, newUDP)
		}
	}
}
