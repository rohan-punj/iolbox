package dirstat

import (
	"math"
	"reflect"
	"testing"
)

// round1 mirrors the stats loop's one-decimal fps rounding so the Direction
// tests exercise the same rounding the server uses.
func round1(v float64) float64 { return math.Round(v*10) / 10 }

// TestDirectionDiff checks that Direction diffs two cumulative snapshots into
// per-(label,subtype) directional rates, attributing each bucket to its endpoint
// index and folding subtype counts under their label.
func TestDirectionDiff(t *testing.T) {
	prev := Counters{
		{ep: 0, label: "BGP", subtype: "keepalive"}: 10,
		{ep: 1, label: "OSPF", subtype: "hello"}:    2,
	}
	cur := Counters{
		{ep: 0, label: "BGP", subtype: "keepalive"}: 14, // +4 over 2s = 2.0 fps ep0
		{ep: 1, label: "BGP", subtype: "update"}:    2,  // +2 = 1.0 fps ep1
		{ep: 1, label: "OSPF", subtype: "hello"}:    2,  // no change -> dropped
		{ep: 0, label: "ARP", subtype: ""}:          6,  // +6 = 3.0 fps ep0, no subtype
	}
	byLabel, bySub := Direction(prev, cur, 2.0, round1)

	wantLabel := map[string][2]float64{
		"BGP": {2.0, 1.0},
		"ARP": {3.0, 0.0},
	}
	if !reflect.DeepEqual(byLabel, wantLabel) {
		t.Errorf("byLabel = %v, want %v", byLabel, wantLabel)
	}

	wantSub := map[string]map[string][2]float64{
		"BGP": {
			"keepalive": {2.0, 0.0},
			"update":    {0.0, 1.0},
		},
	}
	if !reflect.DeepEqual(bySub, wantSub) {
		t.Errorf("byLabelSubtype = %v, want %v", bySub, wantSub)
	}
}

// TestDirectionCounterReset ensures a bucket whose counter went backwards (a
// re-opened socket after link re-add) is skipped rather than producing a
// negative rate.
func TestDirectionCounterReset(t *testing.T) {
	prev := Counters{{ep: 0, label: "TCP", subtype: ""}: 100}
	cur := Counters{{ep: 0, label: "TCP", subtype: ""}: 5} // reset
	byLabel, bySub := Direction(prev, cur, 2.0, round1)
	if byLabel != nil || bySub != nil {
		t.Errorf("reset: got (%v, %v), want (nil, nil)", byLabel, bySub)
	}
}

// TestDirectionEmpty and nil-receiver Snapshot/Close cover the degraded
// (non-fabric / stub) path.
func TestDirectionEmpty(t *testing.T) {
	if bl, bs := Direction(nil, nil, 2.0, round1); bl != nil || bs != nil {
		t.Errorf("empty: got (%v, %v), want (nil, nil)", bl, bs)
	}
	var c *Classifier
	if got := c.Snapshot(); got != nil {
		t.Errorf("nil Snapshot = %v, want nil", got)
	}
	c.Close() // must not panic
}
