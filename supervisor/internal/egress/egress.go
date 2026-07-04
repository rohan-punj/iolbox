// Package egress classifies the runtime's internet-egress path so the GUI can
// tell students when the NAT node cannot pass ICMP/traceroute.
//
// The QEMU launcher uses user-mode slirp, which terminates ICMP: ping and
// traceroute to the internet through the NAT node silently fail there (DHCP and
// outbound TCP still work). Every other runtime (WSL2 behind WinNAT, bridged
// VMware/OVA, LXC/native/Docker on a real host) has a real kernel + host
// NAT/bridge and passes ICMP/traceroute. This package detects the slirp
// signature at startup so hello can advertise it.
package egress

import "net"

// Egress values reported in the hello handshake.
const (
	// Slirp means the runtime is behind QEMU user-mode slirp: DHCP + outbound
	// TCP work through the NAT node, but ICMP/traceroute to the internet do NOT.
	Slirp = "slirp"
	// Routed means a full egress path (real kernel NAT/bridge): ICMP/traceroute
	// through the NAT node work. This is also the permissive default on any
	// uncertainty, so non-launcher runtimes are never mislabeled.
	Routed = "routed"
)

// slirpNote is the short human explanation attached alongside Slirp so the GUI
// can surface why NAT ping/traceroute is unavailable.
const slirpNote = "QEMU user-mode slirp: DHCP & outbound TCP work through NAT, " +
	"but ping/traceroute to the internet do not. Use the bridged VMware/OVA " +
	"appliance or WSL2 for real internet."

// QEMU user-mode slirp hands the guest a fixed 10.0.2.0/24: guest 10.0.2.15,
// default gateway 10.0.2.2, DNS 10.0.2.3. We match on the gateway and subnet.
var (
	slirpGateway = net.IPv4(10, 0, 2, 2)
	slirpNet     = &net.IPNet{IP: net.IPv4(10, 0, 2, 0), Mask: net.CIDRMask(24, 32)}
)

// Resolve turns the -egress flag value into the reported egress. "auto" runs
// the (platform-specific) detector; "slirp"/"routed" force the value; anything
// else falls back to Routed (permissive default). It never fails.
func Resolve(flag string) string {
	switch flag {
	case Slirp:
		return Slirp
	case "auto":
		return Detect()
	default:
		// Routed and any unrecognized value: permissive default.
		return Routed
	}
}

// Note returns the human-readable egress note for the resolved value, or "" for
// the permissive Routed default (nothing to warn about).
func Note(egress string) string {
	if egress == Slirp {
		return slirpNote
	}
	return ""
}

// classify decides Slirp vs Routed from a default-route gateway (may be nil) and
// the set of unicast addresses on the primary interface(s). It is the pure,
// testable core shared by every platform: the slirp default route via 10.0.2.2,
// or a primary address inside 10.0.2.0/24, both indicate slirp. Anything else,
// including no data at all, resolves to Routed.
func classify(gateway net.IP, addrs []net.IP) string {
	if gateway != nil && gateway.Equal(slirpGateway) {
		return Slirp
	}
	for _, ip := range addrs {
		if ip != nil && slirpNet.Contains(ip) {
			return Slirp
		}
	}
	return Routed
}
