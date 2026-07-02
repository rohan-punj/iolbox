package extnet

import "fmt"

// This file builds the privileged-command argv lists for nat/mgmt setup and
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

// natSetupCmds returns the ordered privileged commands to bring up a nat node's
// tap: create the tap owned by us, address it as the gateway, bring it up,
// enable IPv4 forwarding, and install the MASQUERADE + FORWARD-accept rules for
// the pool out the VM's default-route interface. iface is the tap device name.
func natSetupCmds(iface string, sub Subnet, defaultIface, owner string) []cmd {
	return []cmd{
		{[]string{"ip", "tuntap", "add", "dev", iface, "mode", "tap", "user", owner}},
		{[]string{"ip", "addr", "add", sub.CIDR(), "dev", iface}},
		{[]string{"ip", "link", "set", iface, "up"}},
		{[]string{"sysctl", "-w", "net.ipv4.ip_forward=1"}},
		{maskCmd(sub, defaultIface)},
		{fwdOutCmd(sub, iface, defaultIface)},
		{fwdInCmd(sub, iface, defaultIface)},
	}
}

// natTeardownCmds reverses natSetupCmds: remove the iptables rules by exact -D
// spec first (while the interfaces they name still exist), then delete the tap.
// sysctl ip_forward is intentionally NOT reverted — other labs/nat nodes may
// depend on it, and it is a harmless global once on.
func natTeardownCmds(iface string, sub Subnet, defaultIface string) []cmd {
	return []cmd{
		{delRule(maskCmd(sub, defaultIface))},
		{delRule(fwdOutCmd(sub, iface, defaultIface))},
		{delRule(fwdInCmd(sub, iface, defaultIface))},
		{[]string{"ip", "tuntap", "del", "dev", iface, "mode", "tap"}},
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

// mgmtSetupCmds returns the ordered privileged commands to bring up a mgmt
// node's macvtap in bridge mode on the management interface and set it up. No
// IP/NAT is configured — the connected lab nodes provide their own L2 identity.
func mgmtSetupCmds(iface, mgmtIface string) []cmd {
	return []cmd{
		{[]string{"ip", "link", "add", "link", mgmtIface, "name", iface, "type", "macvtap", "mode", "bridge"}},
		{[]string{"ip", "link", "set", iface, "up"}},
	}
}

// mgmtTeardownCmds deletes the macvtap; deleting the link removes its /dev/tapN
// char device too.
func mgmtTeardownCmds(iface string) []cmd {
	return []cmd{
		{[]string{"ip", "link", "delete", iface, "type", "macvtap"}},
	}
}

// devName returns the tap/macvtap device name for a node of the given kind.
func devName(kind Kind, nodeID int) (string, error) {
	switch kind {
	case KindNAT:
		return tapName(nodeID), nil
	case KindMgmt:
		return mvtapName(nodeID), nil
	default:
		return "", fmt.Errorf("extnet: unknown kind %q", kind)
	}
}
