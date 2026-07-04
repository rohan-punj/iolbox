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
	// State is FWD|BLK|LRN|LIS|DIS, preserved verbatim including the
	// transitional LRN/LIS states so the frontend can mark them distinctly
	// (this is a single snapshot, not an auto-poll, so a port caught mid-
	// transition stays LRN/LIS in the result rather than being resolved).
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

// STPResult is one node's spanning-tree decision for a SINGLE VLAN, keyed for
// the canvas by the node's interfaces. Node-level facts (root bridge id,
// isRoot) sit at the top; per-port data is in Ports. STP is per-VLAN with
// exactly one root per VLAN, so a result always carries the VLAN it was
// scraped for.
type STPResult struct {
	// VLAN is the VLAN id this result was parsed for (0 if the source output
	// did not carry a VLAN header, e.g. a synthetic single-instance test).
	VLAN int `json:"vlan,omitempty"`
	// RootID is the elected root bridge id ("priority.macaddr", e.g.
	// "32768.aabb.cc00.0100"). Empty if not parseable.
	RootID string `json:"rootId,omitempty"`
	// BridgeID is THIS node's own bridge id.
	BridgeID string `json:"bridgeId,omitempty"`
	// IsRoot is true ONLY when this node's BridgeID equals RootID — exactly one
	// node in the VLAN's topology may carry IsRoot=true.
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

// STPVlan is one STP-enabled VLAN instance discovered on a node, for the
// VLAN-picker step of the painter flow (enumerate -> pick -> paint).
type STPVlan struct {
	ID   int    `json:"id"`
	Name string `json:"name,omitempty"`
}

// stpBlock is one raw "VLANxxxx ... Interface table" section of
// `show spanning-tree` output, split out before field parsing so the
// multi-VLAN parser and the single-VLAN parser share one block scanner.
type stpBlock struct {
	vlan  int    // 0 if no VLAN header was found for this block
	lines []string
}

// splitSTPBlocks splits `show spanning-tree` (all-VLAN) output into one block
// per "VLANxxxx" / "VLAN0001, Spanning tree ..." header. Output with no VLAN
// header at all (rare, or a synthetic single-instance fixture) is returned as
// a single block with vlan=0.
func splitSTPBlocks(out string) []stpBlock {
	lines := strings.Split(out, "\n")
	var blocks []stpBlock
	var cur *stpBlock
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if v, ok := stpVlanHeader(trimmed); ok {
			blocks = append(blocks, stpBlock{vlan: v})
			cur = &blocks[len(blocks)-1]
			continue
		}
		if cur == nil {
			if trimmed == "" || isErrLine(trimmed) {
				continue
			}
			blocks = append(blocks, stpBlock{})
			cur = &blocks[len(blocks)-1]
		}
		cur.lines = append(cur.lines, line)
	}
	return blocks
}

// stpVlanHeader recognizes a VLAN block header line and returns its VLAN id.
// Observed IOS forms:
//
//	VLAN0001
//	VLAN0010
//	Spanning tree instance(s) for VLAN0001
func stpVlanHeader(trimmed string) (int, bool) {
	if strings.HasPrefix(trimmed, "VLAN") && len(trimmed) > 4 {
		digits := trimmed[4:]
		// Allow a trailing comma/suffix, e.g. "VLAN0010, Spanning tree ...".
		if comma := strings.IndexAny(digits, ", \t"); comma >= 0 {
			digits = digits[:comma]
		}
		if n, ok := parseAllDigits(digits); ok {
			return n, true
		}
	}
	const instPrefix = "Spanning tree instance(s) for VLAN"
	if strings.HasPrefix(trimmed, instPrefix) {
		rest := strings.TrimSpace(trimmed[len(instPrefix):])
		if comma := strings.IndexAny(rest, ", \t"); comma >= 0 {
			rest = rest[:comma]
		}
		if n, ok := parseAllDigits(rest); ok {
			return n, true
		}
	}
	return 0, false
}

// parseAllDigits parses s as an int only if every rune is a digit (avoids
// mistaking "0001 something" for a VLAN id).
func parseAllDigits(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	return atoi(s), true
}

// ParseSTP parses `show spanning-tree` (IOS 17.x, no VLAN qualifier — dumps
// every STP-enabled VLAN as its own block) or `show spanning-tree vlan <N>`
// (a single block). It returns the result for the FIRST parsed block only;
// callers that need a specific VLAN must run `show spanning-tree vlan <N>`
// (see ParseSTPVlanBlock) so exactly one VLAN's tree is returned — STP is
// per-VLAN with exactly one root per VLAN, so mixing blocks together would be
// wrong. A leading `%` error or empty input yields an empty result.
func ParseSTP(out string) STPResult {
	blocks := splitSTPBlocks(out)
	if len(blocks) == 0 {
		return STPResult{}
	}
	return parseSTPBlock(blocks[0])
}

// ParseSTPVlanBlock parses `show spanning-tree vlan <N>` output for exactly
// that VLAN. If the output has no matching VLAN block (L3 node, VLAN not
// running STP here, `%` error) it returns an empty STPResult. If the device
// echoed a different VLAN id in its header than requested (shouldn't happen
// with a scoped command, but tolerated), the parsed VLAN id from the output
// wins so the result is self-describing.
func ParseSTPVlanBlock(out string, vlan int) STPResult {
	blocks := splitSTPBlocks(out)
	for _, b := range blocks {
		if b.vlan == vlan || (b.vlan == 0 && len(blocks) == 1) {
			return parseSTPBlock(b)
		}
	}
	return STPResult{}
}

// parseSTPBlock parses one VLAN's worth of `show spanning-tree` lines (the
// per-VLAN block layout):
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
func parseSTPBlock(b stpBlock) STPResult {
	res := STPResult{VLAN: b.vlan}

	var (
		inRootID   bool
		inBridgeID bool
		rootPrio   string
		rootAddr   string
		brPrio     string
		brAddr     string
		sawTable   bool
	)

	for _, line := range b.lines {
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
	// Root determination: the ONE node whose BridgeID == RootID is the root.
	// "This bridge is the root" (if present) is corroborating, but the id
	// comparison is authoritative so a stray/duplicate banner can never crown
	// more than one node per VLAN.
	res.IsRoot = res.RootID != "" && res.RootID == res.BridgeID

	enrichSTPReasons(&res)
	return res
}

// ParseSTPVlans parses `show spanning-tree` (all-VLAN, no qualifier) and/or
// `show vlan brief` output and returns the VLAN ids that have an STP instance
// running, for the enumerate-then-pick step of the painter flow. vlanBriefOut
// may be empty (VLAN names are best-effort enrichment only — the STP output
// alone is authoritative for "has STP running"). Tolerant of L3 nodes / no
// STP at all: a `%` error or output with no VLAN header yields an empty list,
// never an error.
func ParseSTPVlans(stpOut, vlanBriefOut string) []STPVlan {
	names := parseVlanBriefNames(vlanBriefOut)

	blocks := splitSTPBlocks(stpOut)
	seen := make(map[int]bool)
	var out []STPVlan
	for _, b := range blocks {
		if b.vlan == 0 {
			continue // no recognizable VLAN header -> not a real STP instance
		}
		if seen[b.vlan] {
			continue
		}
		// A block only counts as "has STP running" if it actually carries a
		// bridge/root id or a port table — skip stray/empty blocks (e.g. a
		// trailing header with no body).
		blk := parseSTPBlock(b)
		if blk.Empty() {
			continue
		}
		seen[b.vlan] = true
		out = append(out, STPVlan{ID: b.vlan, Name: names[b.vlan]})
	}
	return out
}

// parseVlanBriefNames parses `show vlan brief` into a vlan-id -> name map,
// best-effort. Layout:
//
//	VLAN Name                             Status    Ports
//	---- -------------------------------- --------- -------------------------------
//	1    default                          active    Et0/0, Et0/1
//	10   Engineering                      active    Et0/2
func parseVlanBriefNames(out string) map[int]string {
	names := map[int]string{}
	if strings.TrimSpace(out) == "" {
		return names
	}
	sawHeader := false
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isErrLine(trimmed) {
			continue
		}
		if strings.HasPrefix(trimmed, "VLAN") && strings.Contains(trimmed, "Name") {
			sawHeader = true
			continue
		}
		if strings.HasPrefix(trimmed, "---") {
			continue
		}
		if !sawHeader {
			continue
		}
		f := strings.Fields(trimmed)
		if len(f) < 2 {
			continue
		}
		id, ok := parseAllDigits(f[0])
		if !ok {
			continue
		}
		names[id] = f[1]
	}
	return names
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

// normState maps IOS state spellings to the canonical PortState. LRN/LIS
// (learning/listening) are transitional states preserved verbatim — they are
// NOT folded into FWD or BLK — so a snapshot caught mid-convergence lets the
// frontend mark those ports distinctly rather than showing a settled state
// that hasn't actually been reached yet.
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
