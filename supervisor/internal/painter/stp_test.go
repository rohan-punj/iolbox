package painter

import "testing"

const stpNonRoot = `
VLAN0001
  Spanning tree enabled protocol rstp
  Root ID    Priority    32768
             Address     aabb.cc00.0100
             Cost        100
             Port        1 (Ethernet0/0)
             Hello Time   2 sec  Max Age 20 sec  Forward Delay 15 sec

  Bridge ID  Priority    32769  (priority 32768 sys-id-ext 1)
             Address     aabb.cc00.0200
             Hello Time   2 sec  Max Age 20 sec  Forward Delay 15 sec
             Aging Time  300 sec

Interface           Role Sts Cost      Prio.Nbr Type
------------------- ---- --- --------- -------- --------------------------------
Et0/0               Root FWD 100       128.1    Shr
Et0/1               Altn BLK 100       128.2    Shr
Et0/2               Desg FWD 100       128.3    Shr
`

const stpRoot = `
VLAN0001
  Spanning tree enabled protocol rstp
  Root ID    Priority    32768
             Address     aabb.cc00.0100
             This bridge is the root
             Hello Time   2 sec  Max Age 20 sec  Forward Delay 15 sec

  Bridge ID  Priority    32768  (priority 32768 sys-id-ext 1)
             Address     aabb.cc00.0100
             Aging Time  300 sec

Interface           Role Sts Cost      Prio.Nbr Type
------------------- ---- --- --------- -------- --------------------------------
Et0/0               Desg FWD 100       128.1    Shr
Et0/1               Desg FWD 100       128.2    Shr
`

const stpErr = `% Spanning tree instance(s) for vlan 1 does not exist.`

func TestParseSTP(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantRootID string
		wantIsRoot bool
		wantPorts  int
		wantBlkIf  string // interface expected blocked, "" if none
	}{
		{"non-root", stpNonRoot, "32768.aabb.cc00.0100", false, 3, "Et0/1"},
		{"root", stpRoot, "32768.aabb.cc00.0100", true, 2, ""},
		{"error", stpErr, "", false, 0, ""},
		{"empty", "", "", false, 0, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseSTP(tc.in)
			if got.RootID != tc.wantRootID {
				t.Errorf("RootID = %q, want %q", got.RootID, tc.wantRootID)
			}
			if got.IsRoot != tc.wantIsRoot {
				t.Errorf("IsRoot = %v, want %v", got.IsRoot, tc.wantIsRoot)
			}
			if len(got.Ports) != tc.wantPorts {
				t.Fatalf("len(Ports) = %d, want %d", len(got.Ports), tc.wantPorts)
			}
			if tc.wantBlkIf != "" {
				var blk *STPPort
				for i := range got.Ports {
					if got.Ports[i].Interface == tc.wantBlkIf {
						blk = &got.Ports[i]
					}
				}
				if blk == nil {
					t.Fatalf("expected port %s not found", tc.wantBlkIf)
				}
				if !blk.Blocked {
					t.Errorf("port %s Blocked = false, want true", tc.wantBlkIf)
				}
				if blk.Reason == "" {
					t.Errorf("port %s Reason is empty, want student-readable text", tc.wantBlkIf)
				}
			}
		})
	}
}

func TestParseSTPBlockedReasonEnriched(t *testing.T) {
	res := ParseSTP(stpNonRoot)
	var blk *STPPort
	for i := range res.Ports {
		if res.Ports[i].Role == RoleAltn {
			blk = &res.Ports[i]
		}
	}
	if blk == nil {
		t.Fatal("no alternate port parsed")
	}
	// Reason should mention the root port (Et0/0) and the root cost (100).
	if !contains(blk.Reason, "Et0/0") {
		t.Errorf("reason %q does not mention root port Et0/0", blk.Reason)
	}
	if !contains(blk.Reason, "100") {
		t.Errorf("reason %q does not mention root path cost 100", blk.Reason)
	}
	if blk.State != StateBLK {
		t.Errorf("state = %q, want BLK", blk.State)
	}
	if blk.InterfaceNorm != "et0/1" {
		t.Errorf("InterfaceNorm = %q, want et0/1", blk.InterfaceNorm)
	}
}

func TestParseSTPNormalization(t *testing.T) {
	res := ParseSTP(stpNonRoot)
	root := res.Ports[0]
	if root.Role != RoleRoot || root.State != StateFWD || root.Cost != 100 || root.Prio != 128 {
		t.Errorf("root port parsed wrong: %+v", root)
	}
	if res.BridgeID != "32769.aabb.cc00.0200" {
		t.Errorf("BridgeID = %q", res.BridgeID)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// --- multi-VLAN, single show spanning-tree (all-VLAN dump) ---

// A lab where two switches disagree on which VLAN's root they are: SW1 is
// root for VLAN 1 (lower bridge id in VLAN1's block) and SW2 is root for
// VLAN 10 (lower bridge id in VLAN10's block). Proves ParseSTP picks only the
// FIRST VLAN block, and ParseSTPVlanBlock picks the requested one — never
// both, so a caller never derives two roots out of one node's output.
const stpMultiVlanSW1 = `
VLAN0001
  Spanning tree enabled protocol rstp
  Root ID    Priority    32768
             Address     aabb.cc00.0100
             This bridge is the root
             Hello Time   2 sec  Max Age 20 sec  Forward Delay 15 sec

  Bridge ID  Priority    32768  (priority 32768 sys-id-ext 1)
             Address     aabb.cc00.0100
             Aging Time  300 sec

Interface           Role Sts Cost      Prio.Nbr Type
------------------- ---- --- --------- -------- --------------------------------
Et0/0               Desg FWD 100       128.1    Shr
Et0/1               Desg FWD 100       128.2    Shr

VLAN0010
  Spanning tree enabled protocol rstp
  Root ID    Priority    24586
             Address     aabb.cc00.0300
             Cost        100
             Port        2 (Ethernet0/1)
             Hello Time   2 sec  Max Age 20 sec  Forward Delay 15 sec

  Bridge ID  Priority    32778  (priority 32768 sys-id-ext 10)
             Address     aabb.cc00.0100
             Aging Time  300 sec

Interface           Role Sts Cost      Prio.Nbr Type
------------------- ---- --- --------- -------- --------------------------------
Et0/0               Altn BLK 100       128.1    Shr
Et0/1               Root FWD 100       128.2    Shr
`

const stpMultiVlanSW2 = `
VLAN0001
  Spanning tree enabled protocol rstp
  Root ID    Priority    32768
             Address     aabb.cc00.0100
             Cost        100
             Port        1 (Ethernet0/0)
             Hello Time   2 sec  Max Age 20 sec  Forward Delay 15 sec

  Bridge ID  Priority    32769  (priority 32768 sys-id-ext 1)
             Address     aabb.cc00.0200
             Aging Time  300 sec

Interface           Role Sts Cost      Prio.Nbr Type
------------------- ---- --- --------- -------- --------------------------------
Et0/0               Root FWD 100       128.1    Shr
Et0/1               Desg FWD 100       128.2    Shr

VLAN0010
  Spanning tree enabled protocol rstp
  Root ID    Priority    24586
             Address     aabb.cc00.0300
             This bridge is the root
             Hello Time   2 sec  Max Age 20 sec  Forward Delay 15 sec

  Bridge ID  Priority    24586  (priority 24576 sys-id-ext 10)
             Address     aabb.cc00.0300
             Aging Time  300 sec

Interface           Role Sts Cost      Prio.Nbr Type
------------------- ---- --- --------- -------- --------------------------------
Et0/0               Desg FWD 100       128.1    Shr
Et0/1               Desg FWD 100       128.2    Shr
`

func TestParseSTPVlanBlockOneRootPerVLAN(t *testing.T) {
	// VLAN 1: SW1 is root, SW2 is not.
	sw1v1 := ParseSTPVlanBlock(stpMultiVlanSW1, 1)
	sw2v1 := ParseSTPVlanBlock(stpMultiVlanSW2, 1)
	if !sw1v1.IsRoot {
		t.Errorf("SW1 VLAN1: IsRoot = false, want true")
	}
	if sw2v1.IsRoot {
		t.Errorf("SW2 VLAN1: IsRoot = true, want false")
	}
	if sw1v1.VLAN != 1 || sw2v1.VLAN != 1 {
		t.Errorf("VLAN field not set: sw1=%d sw2=%d, want 1", sw1v1.VLAN, sw2v1.VLAN)
	}
	if sw1v1.RootID != sw2v1.RootID {
		t.Errorf("VLAN1 root id mismatch: sw1=%q sw2=%q", sw1v1.RootID, sw2v1.RootID)
	}

	// VLAN 10: SW2 is root, SW1 is not — the OPPOSITE of VLAN 1. This is the
	// core "one root per VLAN, not one root for the whole lab" assertion.
	sw1v10 := ParseSTPVlanBlock(stpMultiVlanSW1, 10)
	sw2v10 := ParseSTPVlanBlock(stpMultiVlanSW2, 10)
	if sw1v10.IsRoot {
		t.Errorf("SW1 VLAN10: IsRoot = true, want false")
	}
	if !sw2v10.IsRoot {
		t.Errorf("SW2 VLAN10: IsRoot = false, want true")
	}
	if sw1v10.VLAN != 10 || sw2v10.VLAN != 10 {
		t.Errorf("VLAN field not set: sw1=%d sw2=%d, want 10", sw1v10.VLAN, sw2v10.VLAN)
	}

	// Requesting a VLAN block that isn't in the output yields empty, not a
	// crash and not another VLAN's data.
	missing := ParseSTPVlanBlock(stpMultiVlanSW1, 20)
	if !missing.Empty() {
		t.Errorf("VLAN20 (absent): got non-empty result %+v", missing)
	}
}

func TestParseSTPDefaultsToFirstBlock(t *testing.T) {
	// ParseSTP (no VLAN arg) is documented to take only the first block.
	got := ParseSTP(stpMultiVlanSW1)
	if got.VLAN != 1 {
		t.Errorf("ParseSTP VLAN = %d, want 1 (first block)", got.VLAN)
	}
	if !got.IsRoot {
		t.Errorf("ParseSTP IsRoot = false, want true (SW1 is VLAN1 root)")
	}
}

// --- LRN/LIS transitional-state fixture ---

const stpTransitional = `
VLAN0001
  Spanning tree enabled protocol rstp
  Root ID    Priority    32768
             Address     aabb.cc00.0100
             Cost        100
             Port        1 (Ethernet0/0)
             Hello Time   2 sec  Max Age 20 sec  Forward Delay 15 sec

  Bridge ID  Priority    32769  (priority 32768 sys-id-ext 1)
             Address     aabb.cc00.0200
             Aging Time  300 sec

Interface           Role Sts Cost      Prio.Nbr Type
------------------- ---- --- --------- -------- --------------------------------
Et0/0               Root LRN 100       128.1    Shr
Et0/1               Desg LIS 100       128.2    Shr
Et0/2               Desg FWD 100       128.3    Shr
`

func TestParseSTPTransitionalStatesPreservedVerbatim(t *testing.T) {
	res := ParseSTPVlanBlock(stpTransitional, 1)
	if len(res.Ports) != 3 {
		t.Fatalf("len(Ports) = %d, want 3", len(res.Ports))
	}
	states := map[string]PortState{}
	for _, p := range res.Ports {
		states[p.Interface] = p.State
	}
	if states["Et0/0"] != StateLRN {
		t.Errorf("Et0/0 state = %q, want LRN (not folded into FWD)", states["Et0/0"])
	}
	if states["Et0/1"] != StateLIS {
		t.Errorf("Et0/1 state = %q, want LIS (not folded into BLK)", states["Et0/1"])
	}
	if states["Et0/2"] != StateFWD {
		t.Errorf("Et0/2 state = %q, want FWD", states["Et0/2"])
	}
	// LRN/LIS ports are transitional, not blocked (they are Root/Desg role) —
	// the frontend distinguishes them by State, not by the Blocked flag.
	for _, p := range res.Ports {
		if p.Interface != "Et0/2" && p.Blocked {
			t.Errorf("port %s Blocked = true, want false (role is Root/Desg, just mid-transition)", p.Interface)
		}
	}
}

// --- blocked-port reason fixture (dedicated, beyond the existing enrichment test) ---

const stpBlockedBackupPort = `
VLAN0001
  Spanning tree enabled protocol rstp
  Root ID    Priority    32768
             Address     aabb.cc00.0100
             This bridge is the root
             Hello Time   2 sec  Max Age 20 sec  Forward Delay 15 sec

  Bridge ID  Priority    32768  (priority 32768 sys-id-ext 1)
             Address     aabb.cc00.0100
             Aging Time  300 sec

Interface           Role Sts Cost      Prio.Nbr Type
------------------- ---- --- --------- -------- --------------------------------
Et0/0               Desg FWD 100       128.1    Shr
Et0/1               Back BLK 100       128.2    Shr
`

func TestParseSTPBlockedReasonBackupPort(t *testing.T) {
	res := ParseSTPVlanBlock(stpBlockedBackupPort, 1)
	var blk *STPPort
	for i := range res.Ports {
		if res.Ports[i].Role == RoleBack {
			blk = &res.Ports[i]
		}
	}
	if blk == nil {
		t.Fatal("no backup port parsed")
	}
	if !blk.Blocked {
		t.Errorf("Backup port Blocked = false, want true")
	}
	if blk.Reason == "" {
		t.Errorf("Backup port Reason is empty, want student-readable text")
	}
	if !contains(blk.Reason, "redundant connection") {
		t.Errorf("reason %q does not describe the backup-port redundant-segment case", blk.Reason)
	}
}

// --- VLAN enumeration ---

func TestParseSTPVlansEnumeratesInstances(t *testing.T) {
	got := ParseSTPVlans(stpMultiVlanSW1, "")
	if len(got) != 2 {
		t.Fatalf("len(vlans) = %d, want 2: %+v", len(got), got)
	}
	if got[0].ID != 1 || got[1].ID != 10 {
		t.Errorf("vlan ids = %d,%d, want 1,10", got[0].ID, got[1].ID)
	}
}

func TestParseSTPVlansWithNames(t *testing.T) {
	const vlanBrief = `
VLAN Name                             Status    Ports
---- -------------------------------- --------- -------------------------------
1    default                          active    Et0/0, Et0/1
10   Engineering                      active    Et0/0, Et0/1
`
	got := ParseSTPVlans(stpMultiVlanSW1, vlanBrief)
	names := map[int]string{}
	for _, v := range got {
		names[v.ID] = v.Name
	}
	if names[1] != "default" {
		t.Errorf("vlan1 name = %q, want default", names[1])
	}
	if names[10] != "Engineering" {
		t.Errorf("vlan10 name = %q, want Engineering", names[10])
	}
}

func TestParseSTPVlansToleratesNoSTP(t *testing.T) {
	// L3 node / no STP at all: empty or error output -> empty list, not error.
	cases := []string{"", stpErr, "% Invalid input detected"}
	for _, c := range cases {
		got := ParseSTPVlans(c, "")
		if len(got) != 0 {
			t.Errorf("ParseSTPVlans(%q) = %+v, want empty", c, got)
		}
	}
}
