package extnet

import "fmt"

// This file builds the privileged-command argv lists for nat setup and
// teardown as pure data, so the exact commands are unit-testable on any OS
// without invoking sudo. runCmds (Linux) executes them via `sudo -n`; the
// stub platform never runs them.
//
// Every teardown rule is the exact-spec inverse of a setup rule (iptables -D
// mirrors the -A/-I that added it), so teardown removes precisely what setup
// added and nothing else.

// cmd is one privileged command: the program name plus its arguments. runCmds
// prepends `sudo -n` and wraps a failure with the command's stderr.
type cmd struct {
	args []string
}

func (c cmd) String() string {
	out := ""
	for i, a := range c.args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

// The nat tap is an L2 member of the link's Linux bridge (br-<linkid>) and the
// gateway address + NAT live on the BRIDGE interface, not the tap: a lab node
// reaches the gateway at L2 over the bridge, the kernel answers ARP + routes +
// MASQUERADEs, and the userspace DHCP server reads/writes the tap fd (it sees the
// broadcast DISCOVER/REQUEST the bridge floods). The tap is created at Start
// (unbridged); attach/detach happen when the link is drawn/removed, so a link
// drawn to a running nat hot-connects with no restart.

// natBridgeTapCmds creates the nat tap owned by us and brings it up, but does NOT
// address it or attach it to a bridge (that is natBridgeAttachCmds, run when the
// link exists). The tap sits unbridged until then, exactly like an IOL fabric
// interface's static tap.
func natBridgeTapCmds(iface, owner string) []cmd {
	return []cmd{
		{[]string{"ip", "tuntap", "add", "dev", iface, "mode", "tap", "user", owner}},
		{[]string{"ip", "link", "set", iface, "up"}},
	}
}

// natBridgeTapDelCmds deletes the nat tap (used at teardown and as the Start
// preclean for a stale device).
func natBridgeTapDelCmds(iface string) []cmd {
	return []cmd{{[]string{"ip", "tuntap", "del", "dev", iface, "mode", "tap"}}}
}

// natBridgeAttachCmds attaches the tap to the link bridge and puts the gateway
// address + NAT on the BRIDGE interface. Forward rules key on the bridge as the
// lab-facing interface (traffic enters the host via br's L3 gateway).
func natBridgeAttachCmds(iface, br string, sub Subnet, defaultIface string) []cmd {
	return []cmd{
		{[]string{"ip", "link", "set", iface, "master", br}},
		{[]string{"ip", "addr", "add", sub.CIDR(), "dev", br}},
		{[]string{"sysctl", "-w", "net.ipv4.ip_forward=1"}},
		{maskCmd(sub, defaultIface)},
		{fwdOutCmd(sub, br, defaultIface)},
		{fwdInCmd(sub, br, defaultIface)},
	}
}

// natBridgeDetachCmds reverses natBridgeAttachCmds: remove the iptables rules and
// the gateway address, then detach the tap from the bridge. The bridge device
// itself is owned by the fabric (deleted there), not here.
func natBridgeDetachCmds(iface, br string, sub Subnet, defaultIface string) []cmd {
	return []cmd{
		{delRule(maskCmd(sub, defaultIface))},
		{delRule(fwdOutCmd(sub, br, defaultIface))},
		{delRule(fwdInCmd(sub, br, defaultIface))},
		{[]string{"ip", "addr", "del", sub.CIDR(), "dev", br}},
		{[]string{"ip", "link", "set", iface, "nomaster"}},
	}
}

// maskCmd is the -A MASQUERADE rule (nat table, POSTROUTING) for the pool out
// the default interface.
func maskCmd(sub Subnet, defaultIface string) []string {
	return []string{"iptables", "-t", "nat", "-A", "POSTROUTING",
		"-s", sub.Network(), "-o", defaultIface, "-j", "MASQUERADE"}
}

// fwdOutCmd accepts forwarded traffic FROM the pool tap OUT the default iface.
func fwdOutCmd(sub Subnet, iface, defaultIface string) []string {
	return []string{"iptables", "-A", "FORWARD",
		"-i", iface, "-o", defaultIface, "-s", sub.Network(), "-j", "ACCEPT"}
}

// fwdInCmd accepts established/related return traffic FROM the default iface
// back INTO the pool tap.
func fwdInCmd(sub Subnet, iface, defaultIface string) []string {
	return []string{"iptables", "-A", "FORWARD",
		"-i", defaultIface, "-o", iface, "-d", sub.Network(),
		"-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"}
}

// delRule turns an -A/-I add spec into its exact -D delete spec by swapping the
// insert verb for -D, so teardown removes precisely the rule setup added.
func delRule(add []string) []string {
	out := make([]string, len(add))
	copy(out, add)
	for i, a := range out {
		if a == "-A" || a == "-I" {
			out[i] = "-D"
			break
		}
	}
	return out
}

// devName returns the tap device name for a node of the given kind.
func devName(kind Kind, nodeID int) (string, error) {
	switch kind {
	case KindNAT:
		return tapName(nodeID), nil
	default:
		return "", fmt.Errorf("extnet: unknown kind %q", kind)
	}
}
