package server

import (
	"context"
	"math"
	"runtime"
	"sort"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/dirstat"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
)

// maxProtos caps how many per-protocol entries a link.stats event carries: the
// top few by fps are enough to drive the GUI's traffic breakdown without the
// event ballooning when a link briefly touches many protocols.
const maxProtos = 6

// statsInterval is how often the server samples per-link tap counters and emits
// link.stats events. 2s balances responsive traffic-driven link glow in the GUI
// against event volume.
const statsInterval = 2 * time.Second

// statsLoop samples every fabric link's endpoint-tap netdev counters (plus
// per-protocol counts from any active bridge capture) every statsInterval and
// emits a link.stats event for each link that forwarded traffic during the
// interval. It is the server-side owner of throughput derivation, turning two
// consecutive cumulative samples into per-second rates.
//
// A link.stats event is emitted for a link ONLY when its forwarded count
// changed since the previous tick (i.e. the per-interval delta is nonzero), so
// idle links stay quiet and the GUI can drive link glow purely off these
// events.
//
// The loop is tied to ctx (the server's ListenAndServe lifetime), so it exits
// with the server and leaks no goroutine. Links that disappear between ticks
// are pruned from the baseline so a restarted link (counters reset to 0) is not
// misread as negative throughput.
func (s *Server) statsLoop(ctx context.Context) {
	ticker := time.NewTicker(statsInterval)
	defer ticker.Stop()

	// flast holds the previous tick's cumulative frame/byte counts per fabric
	// link, plus the per-proto cumulative counts so the next tick can diff them
	// into per-proto fps. protos is nil until a link reports protocol traffic.
	type sample struct {
		frames, bytes uint64
		protos        map[string]uint64
		// dir is the previous tick's cumulative directional counters, diffed into
		// ProtosDir/ProtosSubtypeDir the same way protos becomes Protos.
		dir dirstat.Counters
	}
	flast := make(map[int]sample)

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

			// Fabric links: derive throughput from the endpoint taps' netdev
			// counters (always-on link glow) plus per-proto from an active bridge
			// capture. Directional breakdown isn't attributed (tcpdump-on-bridge
			// doesn't split by source), so ProtosDir is omitted.
			s.mu.Lock()
			ll := s.lab
			s.mu.Unlock()
			if ll == nil {
				continue
			}
			// Keep the background sampler from walking a static-tap plan while a
			// serialized topology/lifecycle RPC is replacing it.
			s.labMu.Lock()
			fcur := s.fabricStats(ll)
			s.labMu.Unlock()
			for id := range flast {
				if _, ok := fcur[id]; !ok {
					delete(flast, id)
				}
			}
			for id, st := range fcur {
				prev := flast[id]
				flast[id] = sample{frames: st.frames, bytes: st.bytes, protos: st.protos, dir: st.dir}
				fps, bps, emit := linkRate(prev.frames, prev.bytes, st.frames, st.bytes, statsInterval)
				if !emit {
					continue
				}
				protos := protoRates(prev.protos, st.protos, statsInterval)
				// Diff the always-on directional counters into per-direction rates.
				// Uses the same one-decimal rounding as linkRate's fps.
				protosDir, protosSubtypeDir := dirstat.Direction(
					prev.dir, st.dir, statsInterval.Seconds(), round1)
				s.emit(protocol.EventLinkStats, protocol.LinkStatsData{
					Link: id, FPS: fps, BPS: bps, Protos: protos,
					ProtosDir: protosDir, ProtosSubtypeDir: protosSubtypeDir,
					EpAttrib: toProtocolEndpointAttrib(st.attrib),
				})
			}
		}
	}
}

// round1 rounds a rate to one decimal, matching linkRate's fps rounding. Passed
// to dirstat.Direction so directional rates round the same way as FPS/Protos.
func round1(v float64) float64 { return math.Round(v*10) / 10 }

func toProtocolEndpointAttrib(in []dirstat.EndpointAttrib) []protocol.EndpointAttrib {
	if in == nil {
		return nil
	}
	out := make([]protocol.EndpointAttrib, len(in))
	for i, a := range in {
		out[i] = protocol.EndpointAttrib{
			EndpointIndex: a.EndpointIndex,
			State:         a.State,
			MAC:           a.MAC,
		}
	}
	return out
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
