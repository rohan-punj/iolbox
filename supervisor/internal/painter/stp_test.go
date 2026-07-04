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
