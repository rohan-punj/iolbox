package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// portRanges is the set of TCP ports the launcher exposes from the guest to
// the Windows host. iolbox's allocators hand out lowest-first with sticky
// assignments, so forwarding the first ~50 of each range covers realistic
// labs (see the launcher kickoff / supervisor allocator notes):
//
//   - 4001         GUI + WebSocket bridge (the one the user's browser opens).
//     NOT forwarded to 0.0.0.0 anywhere — localhost-only is the
//     safe default for the no-auth GUI.
//   - 9000..9049   native telnet consoles (pool of 1000 in the guest, allocated
//     from 9000 lowest-first).
//   - 5500..5529   Wireshark capture tees (pool of 1000 in the guest, from 5500).
//
// The control API (127.0.0.1:4000 in the guest) is guest-loopback only and is
// deliberately NOT forwarded. Internal UDP relays (10000+) are never forwarded.
type portRanges struct {
	guiPort      int
	consoleStart int
	consoleCount int
	captureStart int
	captureCount int
}

func defaultPortRanges() portRanges {
	return portRanges{
		guiPort:      4001,
		consoleStart: 9000,
		consoleCount: 50,
		captureStart: 5500,
		captureCount: 30,
	}
}

// forwardedPorts flattens the ranges into the explicit list of host<->guest
// TCP ports to forward. host port == guest port throughout (qemu user-mode net
// exposes each as 127.0.0.1:<port> on the Windows side, matching what the
// browser/telnet/wireshark clients expect).
func (p portRanges) forwardedPorts() []int {
	out := make([]int, 0, 1+p.consoleCount+p.captureCount)
	out = append(out, p.guiPort)
	for i := 0; i < p.consoleCount; i++ {
		out = append(out, p.consoleStart+i)
	}
	for i := 0; i < p.captureCount; i++ {
		out = append(out, p.captureStart+i)
	}
	return out
}

// qemuNetdevArgFor builds the qemu `-netdev user,...` argument for an explicit
// port list. Every port is a hostfwd bound to 127.0.0.1 (never 0.0.0.0 — the
// no-auth GUI stays localhost-only). Consoles/capture bind 0.0.0.0 inside the
// guest already, so guest side is bare :<port>.
//
// Returned as the single netdev string, e.g.
//
//	user,id=net0,hostfwd=tcp:127.0.0.1:4001-:4001,hostfwd=tcp:127.0.0.1:9000-:9000,...
func qemuNetdevArgFor(ports []int) string {
	var b strings.Builder
	b.WriteString("user,id=net0")
	for _, port := range ports {
		fmt.Fprintf(&b, ",hostfwd=tcp:127.0.0.1:%d-:%d", port, port)
	}
	return b.String()
}

// qemuNetdevArg is qemuNetdevArgFor over the full (unfiltered) range set.
func (p portRanges) qemuNetdevArg() string {
	return qemuNetdevArgFor(p.forwardedPorts())
}

// probeFreePorts splits ports into (free, busy) by attempting a short-lived
// 127.0.0.1 listen on each. qemu hard-fails the ENTIRE launch if even one
// hostfwd port can't bind (real-world offender seen on the dev box: ASUS
// ArmouryHtmlDebugServer.exe squatting 127.0.0.1:9014), so the launcher probes
// first: busy console/capture ports are skipped with a warning; a busy GUI
// port is a hard error (the whole product is behind it).
func probeFreePorts(ports []int) (free, busy []int) {
	for _, p := range ports {
		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
		if err != nil {
			busy = append(busy, p)
			continue
		}
		_ = l.Close()
		free = append(free, p)
	}
	return free, busy
}

// parsePortsOverride parses a --ports value of the form
// "gui:console_start:console_count:capture_start:capture_count" (colon-
// separated). Empty fields keep the default for that slot. Fewer fields are
// allowed (trailing defaults kept). Used by the --ports CLI override.
func parsePortsOverride(s string, base portRanges) (portRanges, error) {
	if strings.TrimSpace(s) == "" {
		return base, nil
	}
	fields := strings.Split(s, ":")
	if len(fields) > 5 {
		return base, fmt.Errorf("--ports: too many fields (want up to 5: gui:cstart:ccount:capstart:capcount), got %d", len(fields))
	}
	set := func(dst *int, raw string, name string) error {
		if strings.TrimSpace(raw) == "" {
			return nil
		}
		v, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return fmt.Errorf("--ports: %s %q is not a number", name, raw)
		}
		if v < 0 || v > 65535 {
			return fmt.Errorf("--ports: %s %d out of range 0..65535", name, v)
		}
		*dst = v
		return nil
	}
	out := base
	names := []string{"gui", "console-start", "console-count", "capture-start", "capture-count"}
	dsts := []*int{&out.guiPort, &out.consoleStart, &out.consoleCount, &out.captureStart, &out.captureCount}
	for i, f := range fields {
		if err := set(dsts[i], f, names[i]); err != nil {
			return base, err
		}
	}
	return out, nil
}
