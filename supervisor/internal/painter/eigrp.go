package painter

import "strings"

// EIGRPPath is one path (successor or feasible successor) to the destination.
type EIGRPPath struct {
	// NextHop is the via next-hop IP.
	NextHop string `json:"nextHop"`
	// Interface is the outgoing interface (raw form), if printed.
	Interface string `json:"interface,omitempty"`
	// InterfaceNorm is the lowercased space-free form.
	InterfaceNorm string `json:"interfaceNorm,omitempty"`
	// FD is the feasible distance (composite metric) via this path.
	FD int64 `json:"fd"`
	// RD is the reported distance (the neighbor's advertised distance).
	RD int64 `json:"rd"`
	// Successor is true when this path is the (installed) successor.
	Successor bool `json:"successor"`
	// FeasibleSuccessor is true when this path is a feasible successor (backup
	// that satisfies the feasibility condition RD < FD_successor).
	FeasibleSuccessor bool `json:"feasibleSuccessor"`
}

// EIGRPResult is one node's EIGRP topology decision toward the chosen dest.
type EIGRPResult struct {
	// Prefix is the destination as printed by the topology command.
	Prefix string `json:"prefix,omitempty"`
	// FD is the destination's feasible distance (the successor's metric).
	FD int64 `json:"fd,omitempty"`
	// Paths lists the successor(s) and feasible successor(s).
	Paths []EIGRPPath `json:"paths"`
	// NextHop is the winning (successor) next-hop for path highlighting.
	NextHop string `json:"nextHop,omitempty"`
}

// Empty reports whether nothing useful was parsed.
func (r EIGRPResult) Empty() bool { return len(r.Paths) == 0 && r.NextHop == "" }

// ParseEIGRPTopology parses `show ip eigrp topology <dest>` (IOS 17.x):
//
//	IP-EIGRP (AS 1): Topology entry for 10.0.0.0/24
//	  State is Passive, Query origin flag is 1, 1 Successor(s), FD is 3072000
//	  Routing Descriptor Blocks:
//	  10.0.12.2 (Ethernet0/0), from 10.0.12.2, Send flag is 0x0
//	      Composite metric is (3072000/2816000), route is Internal
//	      ...
//	  10.0.13.3 (Ethernet0/1), from 10.0.13.3, Send flag is 0x0
//	      Composite metric is (3584000/2816000), route is Internal
//
// The first descriptor block whose FD equals the entry FD is the successor; a
// later block with RD < entry FD is a feasible successor. A `%` error or empty
// input yields an empty result.
func ParseEIGRPTopology(out string) EIGRPResult {
	var res EIGRPResult
	lines := strings.Split(out, "\n")

	var cur *EIGRPPath
	flush := func() {
		if cur != nil {
			res.Paths = append(res.Paths, *cur)
			cur = nil
		}
	}

	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isErrLine(trimmed) {
			continue
		}
		// "Topology entry for 10.0.0.0/24"
		if idx := strings.Index(trimmed, "Topology entry for "); idx >= 0 {
			rest := strings.Fields(trimmed[idx+len("Topology entry for "):])
			if len(rest) > 0 {
				res.Prefix = rest[0]
			}
			continue
		}
		// "... FD is 3072000"
		if idx := strings.Index(trimmed, "FD is "); idx >= 0 {
			rest := strings.Fields(trimmed[idx+len("FD is "):])
			if len(rest) > 0 {
				res.FD = atoi64(strings.TrimRight(rest[0], ","))
			}
			continue
		}
		// Descriptor-block header: "10.0.12.2 (Ethernet0/0), from 10.0.12.2, ..."
		if p, ok := parseEIGRPBlockHeader(trimmed); ok {
			flush()
			cp := p
			cur = &cp
			continue
		}
		// "Composite metric is (FD/RD), route is Internal"
		if cur != nil {
			if idx := strings.Index(trimmed, "Composite metric is ("); idx >= 0 {
				inner := trimmed[idx+len("Composite metric is ("):]
				if close := strings.IndexByte(inner, ')'); close >= 0 {
					pair := inner[:close]
					if slash := strings.IndexByte(pair, '/'); slash >= 0 {
						cur.FD = atoi64(pair[:slash])
						cur.RD = atoi64(pair[slash+1:])
					}
				}
			}
		}
	}
	flush()

	classifyEIGRP(&res)
	return res
}

// parseEIGRPBlockHeader parses a routing-descriptor-block header line:
//
//	10.0.12.2 (Ethernet0/0), from 10.0.12.2, Send flag is 0x0
//
// Returns the next-hop + interface. ok=false when the line isn't a block header.
func parseEIGRPBlockHeader(line string) (EIGRPPath, bool) {
	f := strings.Fields(line)
	if len(f) < 2 {
		return EIGRPPath{}, false
	}
	if !looksLikeID(strings.TrimRight(f[0], ",")) {
		return EIGRPPath{}, false
	}
	p := EIGRPPath{NextHop: strings.TrimRight(f[0], ",")}
	// "(Ethernet0/0),"
	if strings.HasPrefix(f[1], "(") {
		iface := strings.Trim(f[1], "(),")
		if looksLikeIface(iface) {
			p.Interface = iface
			p.InterfaceNorm = normIface(iface)
		}
	}
	return p, true
}

// classifyEIGRP marks successors and feasible successors and sets the winning
// next-hop. The successor(s) are the path(s) whose FD equals the entry FD; a
// non-successor path with RD strictly less than the entry FD is a feasible
// successor (satisfies the feasibility condition).
func classifyEIGRP(res *EIGRPResult) {
	// Fall back: entry FD unknown -> use the minimum path FD.
	entryFD := res.FD
	if entryFD == 0 {
		for _, p := range res.Paths {
			if p.FD > 0 && (entryFD == 0 || p.FD < entryFD) {
				entryFD = p.FD
			}
		}
		res.FD = entryFD
	}
	for i := range res.Paths {
		p := &res.Paths[i]
		if entryFD > 0 && p.FD == entryFD {
			p.Successor = true
			if res.NextHop == "" {
				res.NextHop = p.NextHop
			}
			continue
		}
		if entryFD > 0 && p.RD > 0 && p.RD < entryFD {
			p.FeasibleSuccessor = true
		}
	}
}
