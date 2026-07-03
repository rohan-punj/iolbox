package server

import (
	"testing"
	"time"
)

func TestProtoRates(t *testing.T) {
	const interval = 2 * time.Second

	// nil when there's no current traffic at all.
	if got := protoRates(nil, nil, interval); got != nil {
		t.Errorf("empty: got %v, want nil", got)
	}

	// Deltas -> per-second rates, one-decimal rounding, unseen-last-tick label
	// baselines from 0. 100 TCP over 2s = 50 fps; +3 ARP = 1.5 fps.
	prev := map[string]uint64{"TCP": 200}
	cur := map[string]uint64{"TCP": 300, "ARP": 3}
	got := protoRates(prev, cur, interval)
	if got["TCP"] != 50 || got["ARP"] != 1.5 {
		t.Errorf("rates: got %v, want TCP=50 ARP=1.5", got)
	}

	// A protocol with no delta is dropped; a counter that went backwards is
	// skipped (re-baselined by the caller). Only OSPF advanced here.
	prev = map[string]uint64{"TCP": 10, "STP": 50, "OSPF": 0}
	cur = map[string]uint64{"TCP": 10 /*idle*/, "STP": 5 /*reset*/, "OSPF": 4}
	got = protoRates(prev, cur, interval)
	if len(got) != 1 || got["OSPF"] != 2 {
		t.Errorf("filter: got %v, want only OSPF=2", got)
	}

	// Cap to the top maxProtos by fps: build 8 distinct protocols with
	// descending deltas; the two smallest must be dropped.
	prev = map[string]uint64{}
	cur = map[string]uint64{
		"A": 80, "B": 70, "C": 60, "D": 50, "E": 40, "F": 30, "G": 20, "H": 10,
	}
	got = protoRates(prev, cur, interval)
	if len(got) != maxProtos {
		t.Fatalf("cap: got %d entries, want %d", len(got), maxProtos)
	}
	if _, ok := got["G"]; ok {
		t.Errorf("cap: G (below top-6) should be dropped, got %v", got)
	}
	if _, ok := got["H"]; ok {
		t.Errorf("cap: H (below top-6) should be dropped, got %v", got)
	}
	if got["A"] != 40 { // 80 frames / 2s
		t.Errorf("cap: A fps = %v, want 40", got["A"])
	}
}

func TestProtoDirRates(t *testing.T) {
	const interval = 2 * time.Second

	// nil when neither direction has any current traffic.
	if got := protoDirRates([2]map[string]uint64{}, [2]map[string]uint64{}, interval); got != nil {
		t.Errorf("empty: got %v, want nil", got)
	}

	// Traffic sourced only from endpoint 0 appears as [x, 0]; a label present
	// in both directions carries both rates. DOT1Q overlaps and is present here
	// (it lives in the directional map, not the aggregate protos).
	prev := [2]map[string]uint64{
		{"BGP": 100, "DOT1Q": 10},
		{"BGP": 200},
	}
	cur := [2]map[string]uint64{
		{"BGP": 200, "DOT1Q": 30}, // +100 BGP, +20 DOT1Q from ep0 over 2s
		{"BGP": 260},              // +60 BGP from ep1 over 2s
	}
	got := protoDirRates(prev, cur, interval)
	if got["BGP"] != [2]float64{50, 30} {
		t.Errorf("bgp: got %v, want [50 30]", got["BGP"])
	}
	if got["DOT1Q"] != [2]float64{10, 0} { // 20/2s from ep0 only
		t.Errorf("dot1q: got %v, want [10 0]", got["DOT1Q"])
	}

	// A label that didn't advance in either direction is dropped; a backward
	// counter contributes 0 for that side. Here RIP is idle (dropped) and STP
	// advanced only on ep1.
	prev = [2]map[string]uint64{
		{"RIP": 10, "STP": 5},
		{"STP": 50},
	}
	cur = [2]map[string]uint64{
		{"RIP": 10, "STP": 5}, // idle
		{"STP": 70},           // +20 over 2s = 10 fps on ep1
	}
	got = protoDirRates(prev, cur, interval)
	if _, ok := got["RIP"]; ok {
		t.Errorf("idle RIP should be dropped, got %v", got)
	}
	if got["STP"] != [2]float64{0, 10} {
		t.Errorf("stp: got %v, want [0 10]", got["STP"])
	}
}

// TestDot1qExcludedFromProtos documents the overlap invariant: DOT1Q is counted
// per-direction (protoDirRates) but never in the aggregate protos, because the
// relay never adds DOT1Q to the aggregate map — so protoRates never sees it and
// protos still sums to fps. This test asserts protoRates passes DOT1Q through
// only if it were present (it won't be), while protoDirRates does surface it.
func TestDot1qOverlap(t *testing.T) {
	const interval = 2 * time.Second
	// Aggregate map as the relay builds it: no DOT1Q key. protoRates surfaces
	// exactly the labels it's given.
	protos := protoRates(map[string]uint64{}, map[string]uint64{"TCP": 20}, interval)
	if _, ok := protos["DOT1Q"]; ok {
		t.Errorf("protos must not contain DOT1Q, got %v", protos)
	}
	// Directional map does carry DOT1Q.
	dir := protoDirRates(
		[2]map[string]uint64{{}, {}},
		[2]map[string]uint64{{"TCP": 20, "DOT1Q": 20}, {}},
		interval)
	if _, ok := dir["DOT1Q"]; !ok {
		t.Errorf("protosDir must contain DOT1Q, got %v", dir)
	}
}

func TestLinkRate(t *testing.T) {
	const interval = 2 * time.Second
	cases := []struct {
		name           string
		pf, pb, cf, cb uint64
		wantFPS        float64
		wantBPS        uint64
		wantEmit       bool
	}{
		// 100 frames / 6400 bytes over 2s -> 50 fps, 3200 bps.
		{"steady", 0, 0, 100, 6400, 50, 3200, true},
		// Delta of 3 frames over 2s -> 1.5 fps (one-decimal rounding).
		{"rounds-to-one-decimal", 10, 200, 13, 500, 1.5, 150, true},
		// No change since last tick -> no event.
		{"idle", 100, 6400, 100, 6400, 0, 0, false},
		// Counter reset (relay restarted) -> suppressed, re-baseline.
		{"reset", 100, 6400, 5, 320, 0, 0, false},
		// First observation with traffic already present.
		{"first-nonzero", 0, 0, 20, 1280, 10, 640, true},
	}
	for _, c := range cases {
		fps, bps, emit := linkRate(c.pf, c.pb, c.cf, c.cb, interval)
		if emit != c.wantEmit {
			t.Errorf("%s: emit = %v, want %v", c.name, emit, c.wantEmit)
			continue
		}
		if emit && (fps != c.wantFPS || bps != c.wantBPS) {
			t.Errorf("%s: fps=%v bps=%v, want fps=%v bps=%v", c.name, fps, bps, c.wantFPS, c.wantBPS)
		}
	}
}
