package painter

import "strings"

// BGPPath is one candidate path for a BGP prefix from `show ip bgp <prefix>`.
type BGPPath struct {
	// NextHop is the BGP next-hop IP for this path.
	NextHop string `json:"nextHop"`
	// ASPath is the AS_PATH as printed (space-separated ASNs), "" for a locally
	// originated / iBGP-learned path with empty path.
	ASPath string `json:"asPath,omitempty"`
	// Origin is i (IGP), e (EGP) or ? (incomplete).
	Origin string `json:"origin,omitempty"`
	// Weight is the Cisco weight (highest wins first).
	Weight int `json:"weight,omitempty"`
	// LocalPref is the LOCAL_PREF (highest wins).
	LocalPref int `json:"localPref,omitempty"`
	// MED is the metric / MULTI_EXIT_DISC (lowest wins).
	MED int `json:"med,omitempty"`
	// Best is true for the path IOS marked as best.
	Best bool `json:"best"`
}

// BGPResult is one node's best-path decision for the chosen prefix.
type BGPResult struct {
	// Prefix is the prefix the decision is for (as printed).
	Prefix string `json:"prefix,omitempty"`
	// Paths lists every candidate path considered.
	Paths []BGPPath `json:"paths"`
	// BestNextHop is the winning path's next-hop for path highlighting.
	BestNextHop string `json:"bestNextHop,omitempty"`
	// Reason is a student-readable explanation of which tiebreak selected the
	// best path (weight / local-pref / AS-path length / origin / MED / eBGP>iBGP
	// / router-id).
	Reason string `json:"reason,omitempty"`
}

// Empty reports whether nothing useful was parsed.
func (r BGPResult) Empty() bool { return len(r.Paths) == 0 }

// ParseBGP parses `show ip bgp <prefix>` (IOS 17.x), e.g.:
//
//	BGP routing table entry for 10.0.0.0/24, version 5
//	Paths: (2 available, best #2, table default)
//	  Advertised to update-groups: ...
//	  100 200
//	    10.0.12.2 from 10.0.12.2 (2.2.2.2)
//	      Origin IGP, metric 0, localpref 100, valid, external
//	  300
//	    10.0.13.3 from 10.0.13.3 (3.3.3.3)
//	      Origin IGP, metric 0, localpref 100, weight 32768, valid, internal, best
//
// Each path is a "next-hop from ..." header line preceded by an AS_PATH line
// and followed by an attribute line. A `%` error or empty input yields empty.
func ParseBGP(out string) BGPResult {
	var res BGPResult
	lines := strings.Split(out, "\n")

	var cur *BGPPath
	var pendingASPath string
	havePending := false
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
		// "BGP routing table entry for 10.0.0.0/24, version 5"
		if idx := strings.Index(trimmed, "table entry for "); idx >= 0 {
			rest := trimmed[idx+len("table entry for "):]
			res.Prefix = strings.TrimRight(strings.Fields(rest)[0], ",")
			continue
		}
		// The "next-hop from x (router-id)" path header.
		if isBGPPathHeader(trimmed) {
			flush()
			p := BGPPath{}
			f := strings.Fields(trimmed)
			p.NextHop = f[0]
			if havePending {
				p.ASPath = pendingASPath
				havePending = false
			}
			cur = &p
			continue
		}
		// Attribute line: "Origin IGP, metric 0, localpref 100, weight X, ... best"
		if cur != nil && strings.HasPrefix(trimmed, "Origin") {
			applyBGPAttrs(cur, trimmed)
			continue
		}
		// An AS_PATH line: only digits / spaces (and maybe a trailing origin
		// code), appears just before a path header. "Local" also seen.
		if isBGPASPathLine(trimmed) {
			pendingASPath = trimmed
			havePending = true
			continue
		}
	}
	flush()

	// Winning path + tiebreak reason.
	for _, p := range res.Paths {
		if p.Best {
			res.BestNextHop = p.NextHop
			break
		}
	}
	res.Reason = bgpReason(res.Paths)
	return res
}

// isBGPPathHeader reports whether a line is a path's "next-hop from peer (rid)"
// header: it starts with an IPv4 next-hop and contains "from".
func isBGPPathHeader(s string) bool {
	f := strings.Fields(s)
	if len(f) < 3 {
		return false
	}
	if !looksLikeID(f[0]) {
		return false
	}
	return f[1] == "from"
}

// isBGPASPathLine reports whether a line is a bare AS_PATH (all ASNs, or the
// literal "Local"). It must not look like an attribute or header line.
func isBGPASPathLine(s string) bool {
	if s == "Local" {
		return true
	}
	f := strings.Fields(s)
	if len(f) == 0 {
		return false
	}
	for _, tok := range f {
		// Strip a trailing origin code (i/e/?) sometimes appended.
		t := strings.TrimRight(tok, "ie?")
		if t == "" {
			continue
		}
		for _, c := range t {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// applyBGPAttrs reads Origin / metric(MED) / localpref / weight and the best
// marker from a BGP path attribute line.
func applyBGPAttrs(p *BGPPath, line string) {
	// Normalise commas to spaces for easy token scanning.
	toks := strings.Fields(strings.ReplaceAll(line, ",", " "))
	for i, tok := range toks {
		switch tok {
		case "Origin":
			if i+1 < len(toks) {
				switch strings.ToLower(toks[i+1]) {
				case "igp":
					p.Origin = "i"
				case "egp":
					p.Origin = "e"
				case "incomplete":
					p.Origin = "?"
				}
			}
		case "metric":
			if i+1 < len(toks) {
				p.MED = atoi(toks[i+1])
			}
		case "localpref":
			if i+1 < len(toks) {
				p.LocalPref = atoi(toks[i+1])
			}
		case "weight":
			if i+1 < len(toks) {
				p.Weight = atoi(toks[i+1])
			}
		case "best":
			p.Best = true
		}
	}
}

// bgpReason explains, in student terms, which tiebreak selected the best path
// over the runner-up, walking the standard IOS decision order:
// weight > local-pref > AS-path length > origin > MED > eBGP-over-iBGP >
// router-id. Returns "" when there is no best path or only one path.
func bgpReason(paths []BGPPath) string {
	var best *BGPPath
	for i := range paths {
		if paths[i].Best {
			best = &paths[i]
			break
		}
	}
	if best == nil || len(paths) < 2 {
		if best != nil {
			return "Only one valid path is available, so it is selected as best."
		}
		return ""
	}
	// Compare best against each other path; report the first (highest-priority)
	// attribute on which best strictly wins.
	for i := range paths {
		other := &paths[i]
		if other == best {
			continue
		}
		if best.Weight > other.Weight {
			return "Best path: higher Weight (" + itoa(best.Weight) + " vs " + itoa(other.Weight) + ") — weight is the first BGP tiebreaker."
		}
		if best.LocalPref > other.LocalPref {
			return "Best path: higher Local Preference (" + itoa(best.LocalPref) + " vs " + itoa(other.LocalPref) + ")."
		}
		bl, ol := asPathLen(best.ASPath), asPathLen(other.ASPath)
		if bl < ol {
			return "Best path: shorter AS-path (" + itoa(bl) + " vs " + itoa(ol) + " hops)."
		}
		if originRank(best.Origin) < originRank(other.Origin) {
			return "Best path: lower Origin code (" + originName(best.Origin) + " preferred over " + originName(other.Origin) + ")."
		}
		if best.MED < other.MED {
			return "Best path: lower MED (" + itoa(best.MED) + " vs " + itoa(other.MED) + ")."
		}
	}
	return "Best path selected by BGP tiebreak (eBGP over iBGP / lowest router-id)."
}

func asPathLen(s string) int {
	if s == "" || s == "Local" {
		return 0
	}
	return len(strings.Fields(s))
}

// originRank orders origin codes by BGP preference (IGP < EGP < incomplete).
func originRank(o string) int {
	switch o {
	case "i":
		return 0
	case "e":
		return 1
	case "?":
		return 2
	}
	return 3
}

func originName(o string) string {
	switch o {
	case "i":
		return "IGP"
	case "e":
		return "EGP"
	case "?":
		return "incomplete"
	}
	return o
}
