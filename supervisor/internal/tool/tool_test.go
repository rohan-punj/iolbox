package tool

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNaming(t *testing.T) {
	cases := []struct {
		nodeID int
		name   string
		want   string
		got    func(int) string
	}{
		{0, "netns", "iolt0", NetnsName},
		{42, "netns", "iolt42", NetnsName},
		{123456789, "netns", "iolt123456789", NetnsName},
		{0, "host veth", "vtool0", HostVethName},
		{42, "host veth", "vtool42", HostVethName},
		{123456789, "host veth", "vtool123456789", HostVethName},
		{0, "peer temp", "vtoolp0", PeerTempName},
		{42, "peer temp", "vtoolp42", PeerTempName},
		{123456789, "peer temp", "vtoolp123456789", PeerTempName},
		{0, "mgmt veth", "mtool0", MgmtVethName},
		{42, "mgmt veth", "mtool42", MgmtVethName},
		{123456789, "mgmt veth", "mtool123456789", MgmtVethName},
		{0, "cage", "tool-0", CageName},
		{42, "cage", "tool-42", CageName},
		{123456789, "cage", "tool-123456789", CageName},
	}
	for _, tc := range cases {
		if got := tc.got(tc.nodeID); got != tc.want {
			t.Errorf("%s(%d) = %q, want %q", tc.name, tc.nodeID, got, tc.want)
		}
	}

	for _, tc := range []struct {
		name string
		got  string
	}{
		{"netns", NetnsName(999999999)},
		{"host veth", HostVethName(999999999)},
		{"peer temp", PeerTempName(999999999)},
		{"mgmt veth", MgmtVethName(999999999)},
	} {
		if len(tc.got) > 15 {
			t.Errorf("%s name %q is %d characters; want <= 15", tc.name, tc.got, len(tc.got))
		}
	}
	if GuestIface != "eth1" {
		t.Fatalf("GuestIface = %q, want eth1", GuestIface)
	}
}

func TestNetnsExecArgs(t *testing.T) {
	argv := []string{"python", "attack.py", "--target", "10.0.0.1"}
	got := NetnsExecArgs(7, argv)
	want := []string{"ip", "netns", "exec", "iolt7", "python", "attack.py", "--target", "10.0.0.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NetnsExecArgs = %#v, want %#v", got, want)
	}
	argv[0] = "changed"
	if got[4] != "python" {
		t.Fatal("NetnsExecArgs aliased the caller's argv")
	}
	got[4] = "also changed"
	if argv[0] != "changed" {
		t.Fatal("NetnsExecArgs returned storage aliased with the caller's argv")
	}
}

func TestCapabilities(t *testing.T) {
	all := Capabilities{
		NetnsCreate:          true,
		VethCreate:           true,
		VethMoveRename:       true,
		CgroupDelegated:      true,
		AmbientCapTransition: true,
		UnixProxy:            true,
	}
	if !all.OK() || !reflect.DeepEqual(all.GateFeatures(), []string{"tools"}) || !all.Supports(KindTool) {
		t.Fatalf("all capabilities do not pass: %+v, features=%v", all, all.GateFeatures())
	}
	if all.Supports(Kind("other")) {
		t.Fatal("unknown kind is supported")
	}
	for _, clear := range []func(*Capabilities){
		func(c *Capabilities) { c.NetnsCreate = false },
		func(c *Capabilities) { c.VethCreate = false },
		func(c *Capabilities) { c.VethMoveRename = false },
		func(c *Capabilities) { c.CgroupDelegated = false },
		func(c *Capabilities) { c.AmbientCapTransition = false },
		func(c *Capabilities) { c.UnixProxy = false },
	} {
		caps := all
		clear(&caps)
		if caps.OK() || len(caps.GateFeatures()) != 0 || caps.Supports(KindTool) {
			t.Fatalf("missing capability still passed: %+v", caps)
		}
	}
}

func TestContained(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "pack")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "gui")
	if err := os.WriteFile(inside, []byte("gui"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, ok := contained(root, inside)
	if !ok || resolved != inside {
		t.Fatalf("contained(%q, %q) = %q, %v", root, inside, resolved, ok)
	}
	traversal := filepath.Join(root, "..", "outside")
	if _, ok := contained(root, traversal); ok {
		t.Fatal(".. traversal escaped the pack root")
	}
	absoluteEscape := filepath.Join(base, "outside-absolute")
	if err := os.WriteFile(absoluteEscape, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := contained(root, absoluteEscape); ok {
		t.Fatal("absolute path escape was accepted")
	}
}

func TestDefaultLimits(t *testing.T) {
	got := DefaultLimits()
	if got.MemoryMax != 2048*1024*1024 || got.PidsMax != 512 || got.CPUMax != "200000 100000" || got.SwapMax != 0 {
		t.Fatalf("DefaultLimits = %+v", got)
	}
}

func TestOptionsFile(t *testing.T) {
	if got, want := OptionsFile("C:\\run", 12), filepath.Join("C:\\run", "tool", "12", "options.json"); got != want {
		t.Fatalf("custom OptionsFile = %q, want %q", got, want)
	}
	if got, want := OptionsFile("", 12), filepath.Join("/run/iolbox", "tool", "12", "options.json"); got != want {
		t.Fatalf("default OptionsFile = %q, want %q", got, want)
	}
}

func TestScrubbedEnvAllowlist(t *testing.T) {
	hasOptions := false
	hasOldName := false
	for _, name := range ScrubbedEnvAllowlist {
		if name == "IOLBOX_TOOL_OPTIONS" {
			hasOptions = true
		}
		if name == "IOLBOX_TOOL_OPTS" {
			hasOldName = true
		}
	}
	if !hasOptions || hasOldName {
		t.Fatalf("scrubbed environment list = %#v", ScrubbedEnvAllowlist)
	}
}

func TestManifestJSONRoundTrip(t *testing.T) {
	literal := []byte(`{
  "manifestVersion": 1,
  "id": "secbench",
  "name": "Security Bench",
  "icon": "Firewall-2D-Generic-S.svg",
  "interpreter": "venv",
  "gui": {"bin": "secbench-gui", "transport": "unix", "console": "http", "health": "/healthz"},
  "caps": ["NET_RAW"],
  "options": [],
  "groups": ["recon", "spoof", "dhcp", "stp", "vlan", "fhrp"],
  "modules": [{
    "key": "arp_spoof",
    "label": "ARP Spoof / MITM",
    "group": "spoof",
    "script": "attacks/arp_spoof.py",
    "fields": [{"name": "target", "type": "ipv4"}],
    "mitigation": {"text": "ip arp inspection vlan ..."}
  }]
}`)
	var first Manifest
	if err := json.Unmarshal(literal, &first); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	var second Manifest
	if err := json.Unmarshal(encoded, &second); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("manifest changed across JSON round-trip: %#v != %#v", first, second)
	}
	if first.ManifestVersion != ManifestVersion || first.GUI.Health != "/healthz" || first.Modules[0].Script != "attacks/arp_spoof.py" {
		t.Fatalf("manifest did not decode the pinned fields: %+v", first)
	}
}

func TestObjectStateJSONRoundTrip(t *testing.T) {
	want := ObjectState{
		InstanceID: "install-1",
		Objects: map[string]ObjectRecord{
			"7": {
				NodeID:     7,
				CgroupPath: "/sys/fs/cgroup/tool-7",
				Netns:      "iolt7",
				HostVeth:   "vtool7",
				MgmtVeth:   "",
				SocketDir:  "/run/iolbox/tool/7",
			},
		},
	}
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got ObjectState
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("object state changed across JSON round-trip: %#v != %#v", got, want)
	}
}

func TestPIDRegistry(t *testing.T) {
	r := NewPIDRegistry()
	if r.Len() != 0 || r.Contains(42) {
		t.Fatal("new registry is not empty")
	}
	r.Add(42)
	r.Add(42)
	if !r.Contains(42) || r.Len() != 1 {
		t.Fatal("registry did not retain one direct child")
	}
	r.Remove(42)
	if r.Contains(42) || r.Len() != 0 {
		t.Fatal("registry did not remove direct child")
	}
}

func TestPIDRegistryStartAndAdd(t *testing.T) {
	r := NewPIDRegistry()

	started := false
	if err := r.StartAndAdd(func() error { started = true; return nil }, func() int { return 77 }); err != nil {
		t.Fatalf("StartAndAdd: %v", err)
	}
	if !started {
		t.Fatal("StartAndAdd did not call start")
	}
	if !r.Contains(77) || r.Len() != 1 {
		t.Fatal("StartAndAdd did not register the started child")
	}

	// A failed start must register nothing and must surface its own error.
	wantErr := errors.New("start failed")
	pidCalled := false
	if err := r.StartAndAdd(func() error { return wantErr }, func() int { pidCalled = true; return 88 }); !errors.Is(err, wantErr) {
		t.Fatalf("StartAndAdd error = %v, want %v", err, wantErr)
	}
	if pidCalled {
		t.Fatal("StartAndAdd asked for a PID after start failed")
	}
	if r.Contains(88) || r.Len() != 1 {
		t.Fatal("StartAndAdd registered a child whose start failed")
	}
}

// TestPIDRegistryStartAndAddExcludesReap is the regression guard for the
// spawn/reap race: the reap side's check-and-reap and the spawn side's
// start-and-register must be mutually exclusive, so a child that exits
// instantly can never be seen as an unregistered orphan while its spawner has
// already forked it.
func TestPIDRegistryStartAndAddExcludesReap(t *testing.T) {
	r := NewPIDRegistry()

	inReap := make(chan struct{})
	releaseReap := make(chan struct{})
	reapDone := make(chan struct{})
	reaped := false

	go func() {
		defer close(reapDone)
		r.ReapUnregistered(99, func(int) {
			reaped = true
			close(inReap)
			<-releaseReap
		})
	}()
	<-inReap

	registered := make(chan struct{})
	go func() {
		defer close(registered)
		_ = r.StartAndAdd(func() error { return nil }, func() int { return 99 })
	}()

	// The spawner must be blocked for as long as the reap side holds the lock.
	select {
	case <-registered:
		t.Fatal("StartAndAdd registered while ReapUnregistered held the lock")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseReap)
	<-reapDone
	<-registered

	if !reaped {
		t.Fatal("ReapUnregistered skipped an unregistered pid")
	}
	if !r.Contains(99) {
		t.Fatal("StartAndAdd did not register after the lock was released")
	}

	// The mirror case: an already-registered pid is never handed to reap.
	r.ReapUnregistered(99, func(int) { t.Fatal("reaped a registered direct child") })
}

func TestAllowedCaps(t *testing.T) {
	if !reflect.DeepEqual(AllowedCaps, []string{"NET_RAW"}) {
		t.Fatalf("AllowedCaps = %#v", AllowedCaps)
	}
	if strings.Contains(strings.Join(AllowedCaps, ","), "NET_ADMIN") {
		t.Fatal("NET_ADMIN is grantable")
	}
}
