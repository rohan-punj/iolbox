package relay

import "fmt"

// Classify labels an ethernet frame by the highest-layer protocol cheaply
// identifiable from a few fixed-offset byte peeks, and reports whether the frame
// carried an 802.1Q VLAN tag. It is called on the relay's forward hot path (once
// per forwarded datagram) to build the per-proto fps breakdown carried in
// link.stats events, so it must not allocate on the common path and must never
// panic on a short/malformed frame: every offset is bounds checked and anything
// unrecognised falls through to a coarse label.
//
// The returned label is the single primary protocol label; tagged is orthogonal
// (a tagged frame still classifies to its inner protocol) so the relay can count
// tagged frames under an overlapping "DOT1Q" label WITHOUT disturbing the
// primary label totals.
//
// The frame is a raw ethernet frame (dst[6] src[6] ethertype/len[2] payload) —
// the mesh is headerless, so what the pump reads IS the frame on the wire.
// Frames shorter than a 14-byte ethernet header can't be classified and are
// reported as "OTHER" rather than dropped, so they still count toward totals.
func Classify(frame []byte) (label string, tagged bool) {
	if len(frame) < 14 {
		return "OTHER", false
	}

	// EtherType/length lives at offset 12. A single 802.1Q tag (0x8100) pushes
	// the real type out by 4 bytes; we peel at most one tag (Q-in-Q is rare in
	// these labs and not worth the hot-path cost) and report tagged=true.
	et := uint16(frame[12])<<8 | uint16(frame[13])
	l3 := 14
	if et == 0x8100 {
		tagged = true
		if len(frame) < 18 {
			return "OTHER", tagged
		}
		et = uint16(frame[16])<<8 | uint16(frame[17])
		l3 = 18
	}

	if et >= 0x0600 {
		return classifyEtherType(et, frame, l3), tagged
	}
	// et < 0x0600 is an 802.3 length field: the payload is an LLC/SNAP header,
	// which is how STP, CDP, DTP, VTP and IS-IS appear on the wire.
	return classifyLLC(frame, l3), tagged
}

// classifyEtherType handles DIX/Ethernet-II frames (ethertype >= 0x0600),
// including the SNAP recursion which reuses these same labels.
func classifyEtherType(et uint16, frame []byte, l3 int) string {
	switch et {
	case 0x0806:
		return "ARP"
	case 0x0800:
		return classifyIPv4(frame, l3)
	case 0x86dd:
		return classifyIPv6(frame, l3)
	case 0x88cc:
		return "LLDP"
	case 0x9000:
		return "LOOP"
	case 0x8847:
		return "MPLS"
	default:
		return fmt.Sprintf("0x%04x", et)
	}
}

// classifyIPv4 reads the IPv4 protocol byte (offset 9 within the L3 header) to
// name the transport/routing protocol. IHL/options don't move that field. For
// TCP/UDP it peeks the L4 ports to name a well-known application; port reading
// is guarded by the IHL-derived L4 offset and skipped on fragments carrying a
// nonzero fragment offset (their L4 header isn't present at the front).
func classifyIPv4(frame []byte, l3 int) string {
	if len(frame) < l3+10 {
		return "IPv4"
	}
	proto := frame[l3+9]
	switch proto {
	case 1:
		return classifyICMPv4(frame, l3)
	case 2:
		return "IGMP"
	case 6, 17:
		// TCP/UDP: peek ports unless this is a non-first fragment. The fragment
		// offset is the low 13 bits of the flags/frag-offset word at l3+6..7.
		frag := (uint16(frame[l3+6])<<8 | uint16(frame[l3+7])) & 0x1fff
		if frag != 0 {
			return transportName(proto)
		}
		// IHL (low nibble of byte 0) is the L3 header length in 32-bit words.
		ihl := int(frame[l3]&0x0f) * 4
		if ihl < 20 {
			ihl = 20 // malformed IHL; assume a minimal header
		}
		l4 := l3 + ihl
		return classifyPorts(proto, frame, l4)
	case 47:
		return "GRE"
	case 50:
		return "ESP"
	case 51:
		return "AH"
	case 88:
		return "EIGRP"
	case 89:
		return "OSPF"
	case 103:
		return "PIM"
	case 112:
		return "VRRP"
	default:
		return "IPv4"
	}
}

// classifyICMPv4 splits ICMPv4 echo request/reply (types 8/0) out as "PING"
// from other ICMPv4 ("ICMP"). The type byte is the first byte of the ICMP
// header at the L4 offset, derived from IHL like classifyIPv4's port peek.
func classifyICMPv4(frame []byte, l3 int) string {
	ihl := int(frame[l3]&0x0f) * 4
	if ihl < 20 {
		ihl = 20
	}
	l4 := l3 + ihl
	if len(frame) < l4+1 {
		return "ICMP"
	}
	switch frame[l4] {
	case 0, 8:
		return "PING"
	default:
		return "ICMP"
	}
}

// classifyIPv6 reads the fixed IPv6 next-header byte (offset 6 within the L3
// header). Extension headers would shift the true upper-layer proto, but naming
// the first next-header is enough for the traffic breakdown. TCP/UDP peek ports
// at the fixed 40-byte IPv6 header offset (no options to skip).
func classifyIPv6(frame []byte, l3 int) string {
	if len(frame) < l3+7 {
		return "IPv6"
	}
	switch frame[l3+6] {
	case 6, 17:
		return classifyPorts(frame[l3+6], frame, l3+40)
	case 50:
		return "ESP"
	case 51:
		return "AH"
	case 58:
		return "ICMPv6"
	case 88:
		return "EIGRP"
	case 89:
		return "OSPF"
	default:
		return "IPv6"
	}
}

// transportName is the fallback label for a TCP/UDP frame whose ports weren't
// (or couldn't be) read.
func transportName(proto byte) string {
	if proto == 6 {
		return "TCP"
	}
	return "UDP"
}

// classifyPorts names a well-known application from the TCP/UDP source OR
// destination port at the given L4 offset, falling back to the bare transport
// label. A match on either port wins so a reply (well-known port as source) is
// labelled the same as the request. Returns the transport label if the frame is
// too short to hold both ports.
func classifyPorts(proto byte, frame []byte, l4 int) string {
	if len(frame) < l4+4 {
		return transportName(proto)
	}
	src := uint16(frame[l4])<<8 | uint16(frame[l4+1])
	dst := uint16(frame[l4+2])<<8 | uint16(frame[l4+3])
	if proto == 6 {
		if name := tcpPort(src, dst); name != "" {
			return name
		}
		return "TCP"
	}
	if name := udpPort(src, dst); name != "" {
		return name
	}
	return "UDP"
}

// tcpPort maps a well-known TCP port (src or dst) to a protocol label, or "".
func tcpPort(src, dst uint16) string {
	switch {
	case src == 179 || dst == 179:
		return "BGP"
	case src == 49 || dst == 49:
		return "TACACS"
	}
	return ""
}

// udpPort maps a well-known UDP port (src or dst) to a protocol label, or "".
func udpPort(src, dst uint16) string {
	switch {
	case src == 520 || dst == 520:
		return "RIP"
	case src == 4789 || dst == 4789:
		return "VXLAN"
	case src == 1812 || dst == 1812 || src == 1813 || dst == 1813 ||
		src == 1645 || dst == 1645 || src == 1646 || dst == 1646:
		return "RADIUS"
	}
	return ""
}

// classifyLLC decodes the 802.2 LLC (and optional SNAP) header that follows an
// 802.3 length field. STP rides raw LLC (DSAP/SSAP 0x42); IS-IS rides raw LLC
// (DSAP/SSAP 0xFE); CDP/DTP/VTP ride SNAP with Cisco's OUI; a SNAP OUI of
// 00:00:00 is really a tunnelled ethertype, so we recurse into the ethertype
// labels for it.
func classifyLLC(frame []byte, l3 int) string {
	if len(frame) < l3+3 {
		return "LLC"
	}
	dsap, ssap, ctrl := frame[l3], frame[l3+1], frame[l3+2]
	if dsap == 0x42 && ssap == 0x42 {
		return "STP"
	}
	if dsap == 0xFE && ssap == 0xFE {
		return "ISIS"
	}
	if dsap == 0xAA && ssap == 0xAA && ctrl == 0x03 {
		// SNAP: 3-byte OUI then a 2-byte PID (ethertype under OUI 00:00:00).
		if len(frame) < l3+8 {
			return "LLC"
		}
		oui := frame[l3+3 : l3+6]
		pid := uint16(frame[l3+6])<<8 | uint16(frame[l3+7])
		if oui[0] == 0x00 && oui[1] == 0x00 && oui[2] == 0x0c {
			switch pid {
			case 0x2000:
				return "CDP"
			case 0x2003:
				return "VTP"
			case 0x2004:
				return "DTP"
			}
			return "LLC"
		}
		if oui[0] == 0x00 && oui[1] == 0x00 && oui[2] == 0x00 {
			// RFC 1042 ethertype-over-SNAP: reuse the ethertype labels. The
			// SNAP-carried payload begins right after the PID, so L3 advances.
			return classifyEtherType(pid, frame, l3+8)
		}
		return "LLC"
	}
	return "LLC"
}
