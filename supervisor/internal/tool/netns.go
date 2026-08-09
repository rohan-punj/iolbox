package tool

import "fmt"

// The TCP management fallback is deliberately retained as a complete escape
// hatch even though the P1 gate's only pack uses AF_UNIX. A future TCP pack
// must use this path together with the interface-locks in its launcher and
// GUI, because mgmt0 means the namespace no longer has exactly one non-lo
// interface.

const netnsMgmtIface = "mgmt0"

// netnsCreateNetnsCmds keeps namespace creation separate from veth creation so
// callers can record each kernel object before issuing its command.
func netnsCreateNetnsCmds(nodeID int) []cmdSpec {
	return []cmdSpec{{name: "ip", args: []string{"netns", "add", NetnsName(nodeID)}}}
}

// netnsCreateVethCmds creates the bridge-side veth first and moves a uniquely
// named peer before renaming it inside the namespace; this is what keeps a
// host interface already named eth1 untouched.
func netnsCreateVethCmds(nodeID int) []cmdSpec {
	netnsName := NetnsName(nodeID)
	hostVeth := HostVethName(nodeID)
	peerTemp := PeerTempName(nodeID)
	return []cmdSpec{
		{name: "ip", args: []string{"link", "add", hostVeth, "type", "veth", "peer", "name", peerTemp}},
		{name: "ip", args: []string{"link", "set", peerTemp, "netns", netnsName}},
		{name: "ip", args: NetnsExecArgs(nodeID, []string{"ip", "link", "set", peerTemp, "name", GuestIface})[1:]},
		{name: "ip", args: NetnsExecArgs(nodeID, []string{"ip", "link", "set", GuestIface, "up"})[1:]},
		{name: "ip", args: NetnsExecArgs(nodeID, []string{"ip", "link", "set", "lo", "up"})[1:]},
		{name: "ip", args: []string{"link", "set", hostVeth, "up"}},
	}
}

// NetAddrConfig is an optional static address for a tool node's GuestIface
// (eth1). Set from the node's `config.net` doc field (see labTypes.ts) — a
// zero value means "leave eth1 unaddressed", the long-standing default every
// existing pack already tolerates (most secbench modules operate at L2 or
// forge their own L3 headers; only a few, like arp_scan/dhcp_discover, need
// a real return address).
type NetAddrConfig struct {
	IP        string
	PrefixLen int
	Gateway   string // optional; empty means no default route is added
}

// netnsAddrCmds assigns a static IPv4 address (and optionally a default
// route) to GuestIface inside the node's namespace. Called once, after
// netnsCreateVethCmds has brought eth1 up — assigning an address to a
// down interface is itself harmless, but this ordering matches every other
// netns setup step in this file (create, then configure).
func netnsAddrCmds(nodeID int, cfg NetAddrConfig) []cmdSpec {
	cmds := []cmdSpec{
		{name: "ip", args: NetnsExecArgs(nodeID, []string{"ip", "addr", "add",
			fmt.Sprintf("%s/%d", cfg.IP, cfg.PrefixLen), "dev", GuestIface})[1:]},
	}
	if cfg.Gateway != "" {
		cmds = append(cmds, cmdSpec{name: "ip", args: NetnsExecArgs(nodeID, []string{"ip", "route", "add",
			"default", "via", cfg.Gateway})[1:]})
	}
	return cmds
}

// netnsAttachVethCmds leaves the bridge-side end in the root namespace so
// fabric capture and directional statistics can bind the same device as IOL,
// VPCS, and NAT taps.
func netnsAttachVethCmds(nodeID int, br string) []cmdSpec {
	return []cmdSpec{{name: "ip", args: []string{"link", "set", HostVethName(nodeID), "master", br}}}
}

// netnsDetachVethCmds reverses bridge attachment without deleting the
// bridge-owned device.
func netnsDetachVethCmds(nodeID int) []cmdSpec {
	return []cmdSpec{{name: "ip", args: []string{"link", "set", HostVethName(nodeID), "nomaster"}}}
}

// netnsDeleteNetnsCmds is kept as a single best-effort operation because an
// already removed namespace is the normal result of recovery after a crash.
func netnsDeleteNetnsCmds(nodeID int) []cmdSpec {
	return []cmdSpec{{name: "ip", args: []string{"netns", "del", NetnsName(nodeID)}}}
}

// netnsDeleteVethCmds removes only the root-network-namespace bridge-side
// veth; deleting it also removes its moved peer when that peer still exists.
func netnsDeleteVethCmds(nodeID int) []cmdSpec {
	return []cmdSpec{{name: "ip", args: []string{"link", "del", HostVethName(nodeID)}}}
}

// netnsMgmtCIDRs assigns a deterministic point-to-point link-local /31. The
// node ID occupies the third octet so two valid node IDs never share a pair.
func netnsMgmtCIDRs(nodeID int) (hostCIDR, guestCIDR string, err error) {
	if nodeID < 0 || nodeID > 255 {
		return "", "", fmt.Errorf("tool: node id %d cannot be represented by a management /31", nodeID)
	}
	return fmt.Sprintf("169.254.%d.0/31", nodeID), fmt.Sprintf("169.254.%d.1/31", nodeID), nil
}

// netnsMgmtTempName prevents a host-side mgmt0 collision while the second
// veth is being moved; the final peer name inside the namespace is mgmt0.
func netnsMgmtTempName(nodeID int) string {
	return fmt.Sprintf("mtoolp%d", nodeID)
}

// netnsMgmtFirewallCmds expresses the host-only policy in the namespace: the
// GUI can answer the supervisor on the /31, loopback remains usable locally,
// and no packet can use mgmt0 to reach or be reached through eth1.
func netnsMgmtFirewallCmds(nodeID int, add bool) []cmdSpec {
	hostCIDR, guestCIDR, _ := netnsMgmtCIDRs(nodeID)
	verb := "-A"
	if !add {
		verb = "-D"
	}
	return []cmdSpec{
		{name: "ip", args: NetnsExecArgs(nodeID, []string{"iptables", verb, "INPUT", "-i", "lo", "-j", "ACCEPT"})[1:]},
		{name: "ip", args: NetnsExecArgs(nodeID, []string{"iptables", verb, "OUTPUT", "-o", "lo", "-j", "ACCEPT"})[1:]},
		{name: "ip", args: NetnsExecArgs(nodeID, []string{"iptables", verb, "INPUT", "-i", netnsMgmtIface, "-s", hostCIDR, "-d", guestCIDR, "-j", "ACCEPT"})[1:]},
		{name: "ip", args: NetnsExecArgs(nodeID, []string{"iptables", verb, "OUTPUT", "-o", netnsMgmtIface, "-s", guestCIDR, "-d", hostCIDR, "-j", "ACCEPT"})[1:]},
		{name: "ip", args: NetnsExecArgs(nodeID, []string{"iptables", verb, "FORWARD", "-i", netnsMgmtIface, "-o", GuestIface, "-j", "DROP"})[1:]},
		{name: "ip", args: NetnsExecArgs(nodeID, []string{"iptables", verb, "FORWARD", "-i", GuestIface, "-o", netnsMgmtIface, "-j", "DROP"})[1:]},
	}
}

// netnsSetupMgmtCmds builds the complete TCP-only management setup. The
// management veth is intentionally never attached to a lab bridge.
func netnsSetupMgmtCmds(nodeID int) []cmdSpec {
	hostCIDR, guestCIDR, _ := netnsMgmtCIDRs(nodeID)
	hostVeth := MgmtVethName(nodeID)
	temp := netnsMgmtTempName(nodeID)
	cmds := []cmdSpec{
		{name: "ip", args: []string{"link", "add", hostVeth, "type", "veth", "peer", "name", temp}},
		{name: "ip", args: []string{"link", "set", temp, "netns", NetnsName(nodeID)}},
		{name: "ip", args: NetnsExecArgs(nodeID, []string{"ip", "link", "set", temp, "name", netnsMgmtIface})[1:]},
		{name: "ip", args: NetnsExecArgs(nodeID, []string{"ip", "link", "set", netnsMgmtIface, "up"})[1:]},
		{name: "ip", args: []string{"link", "set", hostVeth, "up"}},
		{name: "ip", args: []string{"addr", "add", hostCIDR, "dev", hostVeth}},
		{name: "ip", args: NetnsExecArgs(nodeID, []string{"ip", "addr", "add", guestCIDR, "dev", netnsMgmtIface})[1:]},
		{name: "ip", args: NetnsExecArgs(nodeID, []string{"iptables", "-P", "INPUT", "DROP"})[1:]},
		{name: "ip", args: NetnsExecArgs(nodeID, []string{"iptables", "-P", "OUTPUT", "DROP"})[1:]},
		{name: "ip", args: NetnsExecArgs(nodeID, []string{"iptables", "-P", "FORWARD", "DROP"})[1:]},
	}
	return append(cmds, netnsMgmtFirewallCmds(nodeID, true)...)
}

// netnsTeardownMgmtCmds reverses the policy and addresses before removing the
// root-side veth. Every command is run best-effort so a partially torn-down
// namespace cannot prevent later cleanup.
func netnsTeardownMgmtCmds(nodeID int) []cmdSpec {
	hostCIDR, guestCIDR, _ := netnsMgmtCIDRs(nodeID)
	cmds := netnsMgmtFirewallCmds(nodeID, false)
	cmds = append(cmds,
		cmdSpec{name: "ip", args: NetnsExecArgs(nodeID, []string{"ip", "addr", "del", guestCIDR, "dev", netnsMgmtIface})[1:]},
		cmdSpec{name: "ip", args: []string{"addr", "del", hostCIDR, "dev", MgmtVethName(nodeID)}},
		cmdSpec{name: "ip", args: NetnsExecArgs(nodeID, []string{"iptables", "-P", "INPUT", "ACCEPT"})[1:]},
		cmdSpec{name: "ip", args: NetnsExecArgs(nodeID, []string{"iptables", "-P", "OUTPUT", "ACCEPT"})[1:]},
		cmdSpec{name: "ip", args: NetnsExecArgs(nodeID, []string{"iptables", "-P", "FORWARD", "ACCEPT"})[1:]},
		cmdSpec{name: "ip", args: []string{"link", "del", MgmtVethName(nodeID)}},
	)
	return cmds
}
