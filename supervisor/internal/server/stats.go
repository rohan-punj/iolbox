package server

import (
	"context"
	"math"
	"time"

	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
)

// statsInterval is how often the server samples per-link relay counters and
// emits link.stats events. 2s balances responsive traffic-driven link glow in
// the GUI against event volume.
const statsInterval = 2 * time.Second

// statsLoop polls the relay manager every statsInterval and emits a link.stats
// event for each bridged link that forwarded traffic during the interval. It is
// the server-side owner of throughput derivation: the relay package only
// exposes monotonic Stats() counters (no server import), and this loop turns
// two consecutive samples into per-second rates.
//
// A link.stats event is emitted for a link ONLY when its forwarded count
// changed since the previous tick (i.e. the per-interval delta is nonzero), so
// idle links stay quiet and the GUI can drive link glow purely off these
// events. Native (non-bridged) links have no relay and never appear here.
//
// The loop is tied to ctx (the server's ListenAndServe lifetime), so it exits
// with the server and leaks no goroutine. Relays that disappear between ticks
// are pruned from the baseline so a restarted relay (counters reset to 0) is
// not misread as negative throughput.
func (s *Server) statsLoop(ctx context.Context) {
	ticker := time.NewTicker(statsInterval)
	defer ticker.Stop()

	// last holds the previous tick's cumulative frame/byte counts per link.
	type sample struct{ frames, bytes uint64 }
	last := make(map[int]sample)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cur := s.relays.Stats()
			// Prune links whose relay went away, so a same-id relay started
			// later begins from a fresh zero baseline.
			for id := range last {
				if _, ok := cur[id]; !ok {
					delete(last, id)
				}
			}
			for id, st := range cur {
				prev := last[id]
				last[id] = sample{frames: st.Frames, bytes: st.Bytes}
				fps, bps, emit := linkRate(prev.frames, prev.bytes, st.Frames, st.Bytes, statsInterval)
				if !emit {
					continue
				}
				s.emit(protocol.EventLinkStats, protocol.LinkStatsData{Link: id, FPS: fps, BPS: bps})
			}
		}
	}
}

// linkRate derives per-second forwarded throughput from two consecutive
// cumulative samples of a link's relay counters. It returns the frames/sec
// (rounded to one decimal) and bytes/sec over the interval, plus whether an
// event should be emitted: only when frames actually advanced this interval, so
// idle links stay quiet. A counter that went backwards (relay restarted, so
// counters reset to 0) is treated as a fresh baseline and suppressed until the
// next real delta.
func linkRate(prevFrames, prevBytes, curFrames, curBytes uint64, interval time.Duration) (fps float64, bps uint64, emit bool) {
	if curFrames < prevFrames || curBytes < prevBytes {
		return 0, 0, false // counters reset; re-baseline silently
	}
	dFrames := curFrames - prevFrames
	if dFrames == 0 {
		return 0, 0, false
	}
	dBytes := curBytes - prevBytes
	secs := interval.Seconds()
	fps = math.Round(float64(dFrames)/secs*10) / 10
	bps = uint64(float64(dBytes) / secs)
	return fps, bps, true
}
