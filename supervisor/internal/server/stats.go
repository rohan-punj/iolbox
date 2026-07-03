package server

import (
	"context"
	"math"
	"runtime"
	"sort"
	"time"

	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
)

// maxProtos caps how many per-protocol entries a link.stats event carries: the
// top few by fps are enough to drive the GUI's traffic breakdown without the
// event ballooning when a link briefly touches many protocols.
const maxProtos = 6

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

	// last holds the previous tick's cumulative frame/byte counts per link,
	// plus the per-proto cumulative counts so the next tick can diff them into
	// per-proto fps. protos is nil until a link reports any protocol traffic.
	type sample struct {
		frames, bytes uint64
		protos        map[string]uint64
	}
	last := make(map[int]sample)

	// Host resource monitor: sample the runtime VM's CPU/RAM/disk each tick and
	// push a host.stats event so the GUI can show a live monitor of the host
	// actually executing IOL. Disk is measured on the run-dir filesystem.
	cores := runtime.NumCPU()
	host := newHostReader(s.cfg.RunDir)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cpuPct, memUsed, memTotal, diskUsed, diskTotal := host.read(cores)
			if memTotal > 0 {
				s.emit(protocol.EventHostStats, protocol.HostStatsData{
					CPUPct:   math.Round(cpuPct*10) / 10,
					MemUsed:  memUsed,
					MemTotal: memTotal,
					DiskUsed: diskUsed,
					DiskTot:  diskTotal,
					Cores:    cores,
				})
			}

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
				last[id] = sample{frames: st.Frames, bytes: st.Bytes, protos: st.Protos}
				fps, bps, emit := linkRate(prev.frames, prev.bytes, st.Frames, st.Bytes, statsInterval)
				if !emit {
					continue
				}
				// Derive the per-proto breakdown over the same interval and
				// baseline (a link-wide counter reset re-baselines protos too).
				protos := protoRates(prev.protos, st.Protos, statsInterval)
				s.emit(protocol.EventLinkStats, protocol.LinkStatsData{Link: id, FPS: fps, BPS: bps, Protos: protos})
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

// protoRates diffs two cumulative per-protocol snapshots into per-second rates
// over the interval, returning only the non-zero entries capped to the top
// maxProtos by fps (ties broken by label for a stable order). Each rate is
// rounded to one decimal to match linkRate's fps. A protocol whose counter went
// backwards (relay restarted) is skipped for this tick and re-baselined by the
// caller storing cur. Returns nil when there's nothing to report so the field
// is omitted from the event entirely.
func protoRates(prev, cur map[string]uint64, interval time.Duration) map[string]float64 {
	if len(cur) == 0 {
		return nil
	}
	secs := interval.Seconds()
	type entry struct {
		label string
		fps   float64
	}
	entries := make([]entry, 0, len(cur))
	for label, c := range cur {
		p := prev[label] // 0 if unseen last tick
		if c < p {
			continue // counter reset; re-baseline silently
		}
		d := c - p
		if d == 0 {
			continue
		}
		entries = append(entries, entry{label, math.Round(float64(d)/secs*10) / 10})
	}
	if len(entries) == 0 {
		return nil
	}
	// Sort by fps desc, then label asc, so the top-N cut and map contents are
	// deterministic across ticks with identical traffic.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].fps != entries[j].fps {
			return entries[i].fps > entries[j].fps
		}
		return entries[i].label < entries[j].label
	})
	if len(entries) > maxProtos {
		entries = entries[:maxProtos]
	}
	out := make(map[string]float64, len(entries))
	for _, e := range entries {
		out[e.label] = e.fps
	}
	return out
}
