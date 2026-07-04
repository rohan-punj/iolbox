package painter

import "strings"

// looksLikeID reports whether a token is a dotted router-id / IPv4-shaped value
// (a.b.c.d). Used to skip header/blank lines in column tables.
func looksLikeID(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 3 {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// looksLikePrefix reports whether a token is a network prefix a.b.c.d or
// a.b.c.d/nn.
func looksLikePrefix(s string) bool {
	if slash := strings.IndexByte(s, '/'); slash >= 0 {
		mask := s[slash+1:]
		if mask == "" {
			return false
		}
		for _, c := range mask {
			if c < '0' || c > '9' {
				return false
			}
		}
		s = s[:slash]
	}
	return looksLikeID(s)
}

// looksLikeIface reports whether a token looks like an IOS interface name
// (letter-led, contains a digit). Deliberately loose to accept Et0/0,
// Ethernet0/0, Gi0/0, Serial1/0, Loopback0, etc.
func looksLikeIface(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
		return false
	}
	hasDigit := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			hasDigit = true
			break
		}
	}
	return hasDigit
}

// prefixMatches reports whether a parsed route prefix satisfies the requested
// destination. dest may be a bare host (10.1.1.1) or a prefix (10.1.1.0/24).
// The match is intentionally lenient: exact string match, or the prefix's
// network portion equals dest's, so "10.1.1.1" matches a route printed as
// "10.1.1.0/24" only when the network base matches. For teaching overlays we
// accept an equal network-base OR an exact token match.
func prefixMatches(routePrefix, dest string) bool {
	if routePrefix == dest {
		return true
	}
	rBase := beforeSlash(routePrefix)
	dBase := beforeSlash(dest)
	if rBase == dBase {
		return true
	}
	// Host inside the route's classful/base network: compare the leading octets
	// up to the route prefix's network base non-zero octets. Cheap heuristic: if
	// dest starts with the route network's non-zero prefix.
	return hostInNetworkBase(rBase, dBase)
}

func beforeSlash(s string) string {
	if i := strings.IndexByte(s, '/'); i >= 0 {
		return s[:i]
	}
	return s
}

// hostInNetworkBase reports whether host (a.b.c.d) falls in the network whose
// base is net (a.b.c.d), matching only the leading octets of net that are
// non-zero. This is a lenient teaching heuristic, not a real longest-prefix
// match — e.g. host 10.1.1.5 is "in" net 10.1.1.0.
func hostInNetworkBase(net, host string) bool {
	np := strings.Split(net, ".")
	hp := strings.Split(host, ".")
	if len(np) != 4 || len(hp) != 4 {
		return false
	}
	for i := 0; i < 4; i++ {
		if np[i] == "0" {
			return true // remaining octets are the host part
		}
		if np[i] != hp[i] {
			return false
		}
	}
	return true
}
