// Package fabric manages the Linux tap/bridge devices that make up the link
// fabric: one tap per node port and one bridge per link, hot-plugged at
// runtime via `ip` commands (proven on real IOL: taps pre-created unbridged,
// then `ip link set <tap> master <bridge>` at runtime passes L2 between two
// already-running IOL instances with no restart of either).
package fabric

import "fmt"

// ifnamsiz is the Linux interface-name limit (IFNAMSIZ), INCLUDING the
// trailing NUL the kernel requires, so the usable name length is 15 bytes.
const ifnamsiz = 16

// tapCreateCmds returns the argv (without the leading "sudo"/"-n") to create a
// tap device owned by uid and bring it up: `ip tuntap add ... user <uid>`
// then `ip link set <name> up`.
func tapCreateCmds(name string, uid int) [][]string {
	return [][]string{
		{"ip", "tuntap", "add", "dev", name, "mode", "tap", "user", fmt.Sprintf("%d", uid)},
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
