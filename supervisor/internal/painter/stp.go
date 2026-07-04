package painter

import "strings"

// STPPort is the per-port spanning-tree decision for one interface.
type STPPort struct {
	// Interface is the raw interface name as IOS printed it (e.g. "Et0/0").
	Interface string `json:"interface"`
	// InterfaceNorm is a lowercased, space-free form (e.g. "et0/0") for tolerant
	// matching against the lab doc's endpoint interface strings.
	InterfaceNorm string `json:"interfaceNorm"`
	// Role is Root|Desg|Altn|Back.
	Role PortRole `json:"role"`
	// State is FWD|BLK|LRN|LIS|DIS.
	State PortState `json:"state"`
	// Cost is the port path cost.
	Cost int `json:"cost"`
	// Prio is the port priority.number field ("128.1" -> we keep the priority).
	Prio int `json:"prio,omitempty"`
	// Blocked is true for a non-forwarding port (Altn/Back or BLK state).
	Blocked bool `json:"blocked"`
	// Reason, for a blocked/alternate port, is a student-readable explanation of
	// WHY spanning-tree blocked it (empty for forwarding ports).
	Reason string `json:"reason,omitempty"`
}

// STPResult is one node's `show spanning-tree` decision, keyed for the canvas
// by the node's interfaces. Node-level facts (root bridge id, isRoot) sit at
// the top; per-port data is in Ports.
type STPResult struct {
	// RootID is the elected root bridge id ("priority.macaddr", e.g.
	// "32768.aabb.cc00.0100"). Empty if not parseable.
	RootID string `json:"rootId,omitempty"`
	// BridgeID is THIS node's own bridge id.
	BridgeID string `json:"bridgeId,omitempty"`
	// IsRoot is true when this node is the root bridge for the (first) VLAN.
	IsRoot bool `json:"isRoot"`
	// RootCost is this node's cost to the root (0 when it is the root).
	RootCost int `json:"rootCost,omitempty"`
	// RootPort is the interface this node uses to reach the root ("" on root).
	RootPort string `json:"rootPort,omitempty"`
	// Ports is the per-interface decision.
	Ports []STPPort `json:"ports"`
}

// Empty reports whether nothing useful was parsed (treated as "no data").
func (r STPResult) Empty() bool { return r.RootID == "" && len(r.Ports) == 0 }

// ParseSTP parses `show spanning-tree` (IOS 17.x). It handles the common
// per-VLAN block layout:
//
//	VLAN0001
//	  Spanning tree enabled protocol rstp
//	  Root ID    Priority    32768
//	             Address     aabb.cc00.0100
//	             This bridge is the root            (present only on the root)
//	             Hello Time ...
//	  Bridge ID  Priority    32769  (priority 32768 sys-id-ext 1)
//	             Address     aabb.cc00.0200
//	  Interface           Role Sts Cost      Prio.Nbr Type
//	  ------------------- ---- --- --------- -------- ----
//	  Et0/0               Root FWD 100       128.1    Shr
//	  Et0/1               Altn BLK 100       128.2    Shr
//
// Only the FIRST VLAN block is used (single-VLAN teaching labs); a superior
// "Root ID ... via <port>" cost is used to enrich blocked-port reasons. A
// leading `%` error or empty input yields an empty result.
func ParseSTP(out string) STPResult {
	var res STPResult
	lines := strings.Split(out, "\n")

	var (
		inRootID   bool
		inBridgeID bool
		rootPrio   string
		rootAddr   string
		brPrio     string
		brAddr     string
		sawTable   bool
	)

	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if isErrLine(trimmed) {
			continue
		}

		// "Root ID    Priority    32768"
		if strings.HasPrefix(trimmed, "Root ID") {
			inRootID, inBridgeID = true, false
			if f := afterLabel(trimmed, "Priority"); f != "" {
				rootPrio = f
			}
			continue
		}
		// "Bridge ID  Priority    32769  (priority 32768 sys-id-ext 1)"
		if strings.HasPrefix(trimmed, "Bridge ID") {
			inRootID, inBridgeID = false, true
			if f := afterLabel(trimmed, "Priority"); f != "" {
				brPrio = f
			}
			continue
		}
		// Continuation lines inside a Root/Bridge ID stanza.
		if inRootID {
			if a := afterLabel(trimmed, "Address"); a != "" {
				rootAddr = a
			}
			if strings.Contains(trimmed, "This bridge is the root") {
				res.IsRoot = true
			}
			if c := afterLabel(trimmed, "Cost"); c != "" {
				res.RootCost = atoi(c)
			}
			// "Port  1 (Ethernet0/0)" or "... via Et0/0"
			if p := stpRootPort(trimmed); p != "" && res.RootPort == "" {
				res.RootPort = p
			}
		}
		if inBridgeID {
			if a := afterLabel(trimmed, "Address"); a != "" {
				brAddr = a
			}
		}

		// The interface table header ends the ID stanzas.
		if strings.HasPrefix(trimmed, "Interface") && strings.Contains(trimmed, "Role") {
			inRootID, inBridgeID = false, false
			sawTable = true
			continue
		}
		if strings.HasPrefix(trimmed, "---") {
			continue
		}

		if sawTable {
			if p, ok := parseSTPPortLine(line); ok {
				res.Ports = append(res.Ports, p)
			}
		}
	}

	if rootPrio != "" && rootAddr != "" {
		res.RootID = rootPrio + "." + rootAddr
	}
	if brPrio != "" && brAddr != "" {
		res.BridgeID = brPrio + "." + brAddr
	}
	// A root bridge lists every port as Desg; if we saw "This bridge is the root"
	// keep IsRoot. Otherwise infer from RootID==BridgeID.
	if !res.IsRoot && res.RootID != "" && res.RootID == res.BridgeID {
		res.IsRoot = true
	}

	enrichSTPReasons(&res)
	return res
}

// parseSTPPortLine parses one interface row of the STP port table. Layout:
//
//	Et0/0               Root FWD 100       128.1    Shr
//
// Columns after the interface: Role, State(Sts), Cost, Prio.Nbr, Type. Extra
// trailing columns / wrapped types are ignored. Returns ok=false for a
// non-matching line (blank, header remnant, etc.).
func parseSTPPortLine(line string) (STPPort, bool) {
	f := strings.Fields(line)
	if len(f) < 4 {
		return STPPort{}, false
	}
	role, ok := normRole(f[1])
	if !ok {
		return STPPort{}, false
	}
	state, ok := normState(f[2])
	if !ok {
		return STPPort{}, false
	}
	p := STPPort{
		Interface:     f[0],
		InterfaceNorm: normIface(f[0]),
		Role:          role,
		State:         state,
		Cost:          atoi(f[3]),
	}
	// Prio.Nbr like "128.1" -> priority 128.
	if len(f) >= 5 {
		if dot := strings.IndexByte(f[4], '.'); dot > 0 {
			p.Prio = atoi(f[4][:dot])
		} else {
			p.Prio = atoi(f[4])
		}
	}
	p.Blocked = role == RoleAltn || role == RoleBack || state == StateBLK
	return p, true
}

// normRole maps IOS role spellings to the canonical PortRole.
func normRole(s string) (PortRole, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "root":
		return RoleRoot, true
	case "desg", "designated":
		return RoleDesg, true
	case "altn", "alt", "alternate":
		return RoleAltn, true
	case "back", "backup":
		return RoleBack, true
	}
	return "", false
}

// normState maps IOS state spellings to the canonical PortState.
func normState(s string) (PortState, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "fwd", "forwarding":
		return StateFWD, true
	case "blk", "blocking":
		return StateBLK, true
	case "lrn", "learning":
		return StateLRN, true
	case "lis", "listening":
		return StateLIS, true
	case "dis", "disabled":
		return StateDIS, true
	}
	return "", false
}

// enrichSTPReasons fills a student-readable Reason for every blocked/alternate
// port, using the parsed root port / root cost to explain the decision.
func enrichSTPReasons(res *STPResult) {
	rootPort := res.RootPort
	if rootPort == "" {
		// Derive from a port marked Root role.
		for _, p := range res.Ports {
			if p.Role == RoleRoot {
				rootPort = p.Interface
				break
			}
		}
	}
	for i := range res.Ports {
		p := &res.Ports[i]
		if !p.Blocked {
			continue
		}
		var b strings.Builder
		switch p.Role {
		case RoleAltn:
			b.WriteString("Alternate port: a superior BPDU was received here, so the root bridge is reachable at a lower cost")
			if rootPort != "" {
				b.WriteString(" via the root port ")
				b.WriteString(rootPort)
			}
			if res.RootCost > 0 {
				b.WriteString(" (root path cost ")
				b.WriteString(itoa(res.RootCost))
				b.WriteString(")")
			}
			b.WriteString(". Spanning-tree blocks this redundant path to break the loop.")
		case RoleBack:
			b.WriteString("Backup port: this is a redundant connection to the same segment already served by a designated port on this bridge. Spanning-tree blocks it to break the loop.")
		default:
			// Blocked but role not Altn/Back (e.g. transitional BLK).
			b.WriteString("Port is blocking: it is not part of the active spanning tree toward the root")
			if rootPort != "" {
				b.WriteString(" (root is reached via ")
				b.WriteString(rootPort)
				b.WriteString(")")
			}
			b.WriteString(", so it is blocked to break the loop.")
		}
		p.Reason = b.String()
	}
}

// afterLabel returns the token immediately following label on the line, or "".
// e.g. afterLabel("Root ID    Priority    32768", "Priority") == "32768".
func afterLabel(line, label string) string {
	idx := strings.Index(line, label)
	if idx < 0 {
		return ""
	}
	rest := strings.Fields(line[idx+len(label):])
	if len(rest) == 0 {
		return ""
	}
	return rest[0]
}

// stpRootPort extracts a root-port interface name from a "via"/"Port" hint line.
func stpRootPort(line string) string {
	// "... via Et0/0" style.
	if idx := strings.Index(line, "via "); idx >= 0 {
		f := strings.Fields(line[idx+4:])
		if len(f) > 0 {
			return strings.TrimRight(f[0], ",")
		}
	}
	return ""
}
