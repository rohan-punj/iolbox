package server

import (
	"testing"
	"time"
)

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
