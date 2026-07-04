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
