package lab

import "github.com/rohanpunj/iolbox/supervisor/internal/netmap"

// Interfaces returns the lab-document spelling of every interface exposed by
// a node, in the same deterministic order used by the GUI. It is deliberately
// based on adapter counts rather than links so an unconnected interface still
// appears in a node's MAC list.
func Interfaces(n Node) []string {
	switch n.Kind {
	case KindVPCS, KindNAT:
		return []string{"eth0"}
	case KindPC, KindTool:
		return []string{"eth1"}
	}

	ethGroups := 1
	if n.Ethernet != nil && *n.Ethernet > 0 {
		ethGroups = *n.Ethernet
	}
	serialGroups := 0
	if n.Serial != nil && *n.Serial > 0 {
		serialGroups = *n.Serial
	}

	ifaces := netmap.IfacesForCounts(ethGroups, serialGroups)
	out := make([]string, 0, len(ifaces))
	for _, iface := range ifaces {
		out = append(out, iface.String())
	}
	return out
}
