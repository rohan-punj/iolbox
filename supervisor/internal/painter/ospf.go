package painter

import "strings"

// OSPFNeighbor is one adjacency from `show ip ospf neighbor`.
type OSPFNeighbor struct {
	// NeighborID is the neighbor's OSPF router-id.
	NeighborID string `json:"neighborId"`
	// State is the adjacency state ("FULL", "2WAY", "INIT", ...) with the role
	// suffix stripped (see Role).
	State string `json:"state"`
	// Role is the neighbor's role on the segment: "DR", "BDR", "DROTHER" or "".
	Role string `json:"role,omitempty"`
	// Address is the neighbor's interface IP.
	Address string `json:"address,omitempty"`
	// Interface is the local interface this neighbor is seen on (raw form).
	Interface string `json:"interface"`
	// InterfaceNorm is the lowercased space-free form for tolerant matching.
	InterfaceNorm string `json:"interfaceNorm"`
}

// OSPFRoute is the winning OSPF route to the chosen destination on this node.
type OSPFRoute struct {
	// Prefix is the destination network as printed ("10.0.0.0/24" or the
	// classful form IOS used).
	Prefix string `json:"prefix,omitempty"`
	// NextHop is the winning next-hop IP toward the destination.
	NextHop string `json:"nextHop,omitempty"`
	// Interface is the outgoing interface (raw form), if printed.
	Interface string `json:"interface,omitempty"`
	// InterfaceNorm is the lowercased space-free form.
	InterfaceNorm string `json:"interfaceNorm,omitempty"`
	// Cost is the OSPF metric of the winning route.
	Cost int `json:"cost,omitempty"`
}

// OSPFResult is one node's OSPF decision: its adjacencies plus (when a
// destination was requested) the winning route toward it.
type OSPFResult struct {
	Neighbors []OSPFNeighbor `json:"neighbors"`
	// Route is the winning route to the requested destination (zero value when
	// no dest was requested or none was found).
	Route OSPFRoute `json:"route,omitempty"`
}

// Empty reports whether nothing useful was parsed.
func (r OSPFResult) Empty() bool { return len(r.Neighbors) == 0 && r.Route.NextHop == "" }

// ParseOSPFNeighbors parses `show ip ospf neighbor`:
//
//	Neighbor ID     Pri   State           Dead Time   Address         Interface
//	2.2.2.2           1   FULL/DR         00:00:35    10.0.12.2       Ethernet0/0
//	3.3.3.3           1   FULL/BDR        00:00:33    10.0.13.3       Ethernet0/1
//
// The "State" column is "adjacency/role"; role is DR/BDR/DROTHER. A `%` error
// or empty input yields no neighbors.
func ParseOSPFNeighbors(out string) []OSPFNeighbor {
	var res []OSPFNeighbor
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isErrLine(trimmed) {
			continue
		}
		if strings.HasPrefix(trimmed, "Neighbor ID") {
			continue // header
		}
		f := strings.Fields(trimmed)
		if len(f) < 6 {
			continue
		}
		// f[0]=NeighborID f[1]=Pri f[2]=State/Role f[3..]=DeadTime Address Interface
		if !looksLikeID(f[0]) {
			continue
		}
		state, role := splitState(f[2])
		nb := OSPFNeighbor{
			NeighborID:    f[0],
			State:         state,
			Role:          role,
			Address:       f[len(f)-2],
			Interface:     f[len(f)-1],
			InterfaceNorm: normIface(f[len(f)-1]),
		}
		res = append(res, nb)
	}
	return res
}

// splitState splits an OSPF "FULL/DR" state field into ("FULL","DR"). A bare
// "FULL" yields ("FULL",""). "DROTHER" is a role, not a state.
func splitState(s string) (state, role string) {
	if i := strings.IndexByte(s, '/'); i >= 0 {
		state = s[:i]
		role = s[i+1:]
		// Normalise "-" (no role) to empty.
		if role == "-" {
			role = ""
		}
		return state, role
	}
	return s, ""
}

// ParseOSPFRoute parses `show ip route ospf` (or a filtered `show ip route
// <dest>`), returning the winning OSPF next-hop toward the requested dest.
//
//	O        10.0.0.0/24 [110/20] via 10.0.12.2, 00:05:11, Ethernet0/0
//
// When multiple next-hops are present (ECMP) the first is returned. If dest is
// non-empty, only a line whose prefix contains dest is considered; otherwise
// the first OSPF route line is used.
func ParseOSPFRoute(out, dest string) OSPFRoute {
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isErrLine(trimmed) {
			continue
		}
		// OSPF codes: O, O IA, O E1/E2, O N1/N2. The line starts with "O".
		if !ospfRouteLine(trimmed) {
			continue
		}
		r := parseRouteLine(trimmed)
		if r.Prefix == "" {
			continue
		}
		if dest != "" && !prefixMatches(r.Prefix, dest) {
			continue
		}
		return r
	}
	return OSPFRoute{}
}

// ospfRouteLine reports whether a `show ip route` line is an OSPF route (starts
// with the O code, possibly followed by IA/E1/E2/N1/N2 and a prefix).
func ospfRouteLine(s string) bool {
	f := strings.Fields(s)
	return len(f) > 0 && f[0] == "O"
}

// parseRouteLine extracts prefix, [admin/metric], next-hop and interface from a
// generic `show ip route` line. Handles the common single-line form; ignores
// codes it doesn't recognize.
func parseRouteLine(s string) OSPFRoute {
	var r OSPFRoute
	f := strings.Fields(s)
	// Find the prefix token: the first field that looks like a.b.c.d or a.b.c.d/n.
	pi := -1
	for i, tok := range f {
		if looksLikePrefix(tok) {
			pi = i
			r.Prefix = tok
			break
		}
	}
	if pi < 0 {
		return r
	}
	// Metric bracket "[110/20]".
	for _, tok := range f {
		if strings.HasPrefix(tok, "[") && strings.Contains(tok, "/") {
			inner := strings.Trim(tok, "[]")
			if slash := strings.IndexByte(inner, '/'); slash >= 0 {
				r.Cost = atoi(inner[slash+1:])
			}
		}
	}
	// "via 10.0.12.2," and trailing interface.
	for i, tok := range f {
		if tok == "via" && i+1 < len(f) {
			r.NextHop = strings.TrimRight(f[i+1], ",")
		}
	}
	// Interface is the last field when it isn't a time/via token.
	if last := f[len(f)-1]; looksLikeIface(last) {
		r.Interface = last
		r.InterfaceNorm = normIface(last)
	}
	return r
}
