// Package fabric manages the Linux tap/bridge devices that make up the link
// fabric: one tap per node port and one bridge per link, hot-plugged at
// runtime via `ip` commands (proven on real IOL: taps pre-created unbridged,
// then `ip link set <tap> master <bridge>` at runtime passes L2 between two
// already-running IOL instances with no restart of either).
package fabric

import (
	"fmt"
	"strconv"
)

// ifnamsiz is the Linux interface-name limit (IFNAMSIZ), INCLUDING the
// trailing NUL the kernel requires, so the usable name length is 15 bytes.
const ifnamsiz = 16

// tapCreateCmds returns the argv (without the leading "sudo"/"-n") to create a
// tap device owned by uid and bring it up: `ip tuntap add ... user <uid>`,
// disable IPv6 on it, then `ip link set <name> up`.
//
// disable_ipv6 matters: every UP interface with IPv6 enabled (the kernel
// default) auto-assigns a link-local address and periodically emits
// Neighbor Discovery / MLD background traffic sourced from ITS OWN kernel
// MAC — even a tap nothing is cabled to. Because a tap is bidirectional
// (whatever the kernel wants to "transmit" on it is exactly what iouyap
// reads and forwards into IOL as traffic "received" on that port), this
// background noise gets handed to IOL as if it arrived from off-box. IOL
// then does exactly what a real switch does with an unknown-destination
// multicast frame: learns the source MAC and floods it out every other
// port — which is how this leaked from unwired ports onto wired ones too.
// Confirmed live: the Linux bridge's own FDB showed sibling ports' kernel
// MACs "arriving via" a bridged tap, meaning IOL itself was relaying this
// traffic between its ports exactly as flooding predicts. Disabling IPv6
// at creation, before the interface is ever brought up, stops the kernel
// from generating this traffic in the first place.
func tapCreateCmds(name string, uid int) [][]string {
	return [][]string{
		{"ip", "tuntap", "add", "dev", name, "mode", "tap", "user", fmt.Sprintf("%d", uid)},
		{"sysctl", "-w", fmt.Sprintf("net.ipv6.conf.%s.disable_ipv6=1", name)},
		{"ip", "link", "set", name, "up"},
	}
}

// tapDeleteCmds returns the argv to delete a tap device.
func tapDeleteCmds(name string) [][]string {
	return [][]string{
		{"ip", "link", "del", name},
	}
}

// bridgeCreateCmds returns the argv to create a bridge device and bring it up.
//
// group_fwd_mask is set to 0xfff8 at create time so the bridge forwards the
// reserved link-local multicast range 01:80:C2:00:00:03-0F instead of
// dropping it — this is what lets LLDP (...:0E) and 802.1X/EAPOL (...:03)
// cross the per-link bridge fabric. It is deliberately NOT 0xffff: bits 0/1/2
// of that range are STP (...:00), the "reserved for future standardization"
// pause-like group (...:01), and 802.1X/LACP-adjacent (...:02); the kernel
// refuses to forward those and returns EIO if you try to set them via
// group_fwd_mask, so 0xfff8 (bits 3-15) is the widest value the kernel
// actually accepts.
func bridgeCreateCmds(name string) [][]string {
	return [][]string{
		{"ip", "link", "add", name, "type", "bridge", "group_fwd_mask", "0xfff8"},
		{"sysctl", "-w", fmt.Sprintf("net.ipv6.conf.%s.disable_ipv6=1", name)},
		{"ip", "link", "set", name, "up"},
	}
}

// bridgeDeleteCmds returns the argv to delete a bridge device.
func bridgeDeleteCmds(name string) [][]string {
	return [][]string{
		{"ip", "link", "del", name},
	}
}

// attachCmds returns the argv to attach a tap to a bridge (hot-plug: safe to
// run against already-running tap/bridge devices and an already-running IOL
// instance holding the tap open).
func attachCmds(bridge, tap string) [][]string {
	return [][]string{
		{"ip", "link", "set", tap, "master", bridge},
	}
}

// detachCmds returns the argv to detach a tap from whatever bridge it is
// currently a member of.
func detachCmds(tap string) [][]string {
	return [][]string{
		{"ip", "link", "set", tap, "nomaster"},
	}
}

// Netem is the flat, per-device impairment applied by SetNetem. Zero values
// mean that the corresponding netem primitive is omitted. The server validates
// the user-facing LinkFault before converting it to this command shape.
type Netem struct {
	DelayMs      float64
	JitterMs     float64
	LossPct      float64
	DuplicatePct float64
	ReorderPct   float64
	RateKbit     int
}

func netemNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// netemCmds returns the exact argv for one atomic flat netem replacement.
// It deliberately emits no distribution or separate shaping qdisc. The v1
// model has no correlation field; reorder still needs tc's correlation token,
// so it uses a fixed 50% correlation value.
func netemCmds(dev string, n Netem) [][]string {
	argv := []string{"tc", "qdisc", "replace", "dev", dev, "root", "netem"}
	if n.DelayMs > 0 {
		argv = append(argv, "delay", netemNumber(n.DelayMs)+"ms")
		if n.JitterMs > 0 {
			argv = append(argv, netemNumber(n.JitterMs)+"ms")
		}
	}
	if n.LossPct > 0 {
		argv = append(argv, "loss", netemNumber(n.LossPct)+"%")
	}
	if n.DuplicatePct > 0 {
		argv = append(argv, "duplicate", netemNumber(n.DuplicatePct)+"%")
	}
	if n.ReorderPct > 0 {
		// LinkFault v1 has no separate reorder-correlation knob. Use the
		// netem default convention explicitly so the argv satisfies tc's
		// reorder grammar while keeping the wire model intentionally small.
		argv = append(argv, "reorder", netemNumber(n.ReorderPct)+"%", "50%")
	}
	if n.RateKbit > 0 {
		argv = append(argv, "rate", strconv.Itoa(n.RateKbit)+"kbit")
	}
	return [][]string{argv}
}

// netemClearCmds returns the idempotent qdisc removal argv for one device.
func netemClearCmds(dev string) [][]string {
	return [][]string{{"tc", "qdisc", "del", "dev", dev, "root"}}
}

// sudoArgv chooses how to exec a privileged argv given the caller's effective
// uid: root (euid 0, the supervisor's normal systemd identity) needs no sudo
// at all — `sudo -n ip ...` still costs a fork+exec of sudo plus its PAM/policy
// checks (~9ms measured vs ~2ms bare on the real OVA) for a command that's
// already privileged. Non-root callers (e.g. the builder's `iolab` smoke user,
// which relies on NOPASSWD sudo) keep the `sudo -n` prefix. Pure data/no I/O so
// it's unit-testable without a process exec on any OS.
func sudoArgv(euid int, argv []string) (name string, args []string) {
	if euid == 0 {
		return argv[0], argv[1:]
	}
	full := append([]string{"-n"}, argv...)
	return "sudo", full
}

// TapName returns the tap device name for a node instance's flat port index
// (netmap's adapter*16+port), e.g. "iol3_17". It errors if the resulting name
// would exceed the Linux IFNAMSIZ limit (15 usable bytes).
func TapName(instanceID, portFlatIndex int) (string, error) {
	name := fmt.Sprintf("iol%d_%d", instanceID, portFlatIndex)
	if len(name) > ifnamsiz-1 {
		return "", fmt.Errorf("fabric: tap name %q exceeds %d-byte interface name limit", name, ifnamsiz-1)
	}
	return name, nil
}

// BridgeName returns the bridge device name for a link ID, e.g. "iolbr12". It
// errors if the resulting name would exceed the Linux IFNAMSIZ limit (15
// usable bytes).
func BridgeName(linkID int) (string, error) {
	name := fmt.Sprintf("iolbr%d", linkID)
	if len(name) > ifnamsiz-1 {
		return "", fmt.Errorf("fabric: bridge name %q exceeds %d-byte interface name limit", name, ifnamsiz-1)
	}
	return name, nil
}
