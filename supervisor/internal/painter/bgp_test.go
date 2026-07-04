package painter

import "testing"

// Two eBGP paths; best selected on shorter AS-path (path #2, 1 hop vs 2).
const bgpASPath = `
BGP routing table entry for 10.0.99.0/24, version 5
Paths: (2 available, best #2, table default)
  Advertised to update-groups:
     3
  100 200
    10.0.12.2 from 10.0.12.2 (2.2.2.2)
      Origin IGP, metric 0, localpref 100, valid, external
      rx pathid: 0, tx pathid: 0
  300
    10.0.13.3 from 10.0.13.3 (3.3.3.3)
      Origin IGP, metric 0, localpref 100, valid, external, best
      rx pathid: 0, tx pathid: 0x0
`

// Best selected on higher weight.
const bgpWeight = `
BGP routing table entry for 10.0.88.0/24, version 7
Paths: (2 available, best #1, table default)
  200
    10.0.12.2 from 10.0.12.2 (2.2.2.2)
      Origin IGP, metric 0, localpref 100, weight 32768, valid, external, best
  200
    10.0.13.3 from 10.0.13.3 (3.3.3.3)
      Origin IGP, metric 0, localpref 100, valid, external
`

const bgpErr = `% Network not in table`

func TestParseBGPASPath(t *testing.T) {
	r := ParseBGP(bgpASPath)
	if r.Prefix != "10.0.99.0/24" {
		t.Errorf("prefix = %q", r.Prefix)
	}
	if len(r.Paths) != 2 {
		t.Fatalf("got %d paths, want 2", len(r.Paths))
	}
	if r.BestNextHop != "10.0.13.3" {
		t.Errorf("BestNextHop = %q, want 10.0.13.3", r.BestNextHop)
	}
	// Path 0 = "100 200" (len 2); path 1 = "300" (len 1), marked best.
	if r.Paths[0].ASPath != "100 200" {
		t.Errorf("path0 ASPath = %q", r.Paths[0].ASPath)
	}
	if !r.Paths[1].Best {
		t.Errorf("path1 should be best")
	}
	if !contains(r.Reason, "AS-path") {
		t.Errorf("reason = %q, want AS-path tiebreak", r.Reason)
	}
}

func TestParseBGPWeight(t *testing.T) {
	r := ParseBGP(bgpWeight)
	if r.BestNextHop != "10.0.12.2" {
		t.Errorf("BestNextHop = %q, want 10.0.12.2", r.BestNextHop)
	}
	if r.Paths[0].Weight != 32768 {
		t.Errorf("weight = %d, want 32768", r.Paths[0].Weight)
	}
	if !contains(r.Reason, "Weight") {
		t.Errorf("reason = %q, want Weight tiebreak", r.Reason)
	}
}

func TestParseBGPError(t *testing.T) {
	if r := ParseBGP(bgpErr); !r.Empty() {
		t.Errorf("error output not empty: %+v", r)
	}
	if r := ParseBGP(""); !r.Empty() {
		t.Errorf("empty output not empty: %+v", r)
	}
}
