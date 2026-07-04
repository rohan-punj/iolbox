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
	label, _, tagged = ClassifyDetailed(frame)
	return label, tagged
}

// ClassifyDetailed is Classify plus a coarse packet-type subtype for the
// protocols whose message type is cheaply readable from a fixed offset near the
// front of the frame (ICMP, BGP, OSPF, EIGRP, ARP). It shares Classify's
// single-pass traversal, so it has the same hot-path/no-panic contract: every
// offset is bounds checked and anything unrecognised falls through to a coarse
// label with an empty subtype. The subtype is "" whenever the label has no
// known sub-discrimination, the type byte can't be reached (short/segmented
// frame), or the value is unrecognised — callers omit an empty subtype.
//
// Endpoint/direction attribution is the caller's job (the AF_PACKET per-tap
// classifier splits by sll_pkttype); this only names the frame.
func ClassifyDetailed(frame []byte) (label, subtype string, tagged bool) {
	if len(frame) < 14 {
		return "OTHER", "", false
	}

	// EtherType/length lives at offset 12. A single 802.1Q tag (0x8100) pushes
	// the real type out by 4 bytes; we peel at most one tag (Q-in-Q is rare in
	// these labs and not worth the hot-path cost) and report tagged=true.
	et := uint16(frame[12])<<8 | uint16(frame[13])
	l3 := 14
	if et == 0x8100 {
		tagged = true
		if len(frame) < 18 {
			return "OTHER", "", tagged
		}
		et = uint16(frame[16])<<8 | uint16(frame[17])
		l3 = 18
	}

	if et >= 0x0600 {
		label, subtype = classifyEtherTypeDetailed(et, frame, l3)
		return label, subtype, tagged
	}
	// et < 0x0600 is an 802.3 length field: the payload is an LLC/SNAP header,
	// which is how STP, CDP, DTP, VTP and IS-IS appear on the wire. None of the
	// LLC/SNAP labels carry a subtype we decode.
	return classifyLLC(frame, l3), "", tagged
}

// classifyEtherType handles DIX/Ethernet-II frames (ethertype >= 0x0600),
// including the SNAP recursion which reuses these same labels. It discards the
// subtype classifyEtherTypeDetailed computes (LLC/SNAP-tunnelled frames don't
// surface subtypes to callers).
func classifyEtherType(et uint16, frame []byte, l3 int) string {
	label, _ := classifyEtherTypeDetailed(et, frame, l3)
	return label
}

// classifyEtherTypeDetailed is classifyEtherType plus the subtype for the
// ethertypes that carry one (ARP op, and the IPv4/IPv6 upper-layer message
// types). SNAP-tunnelled ethertypes recurse through classifyEtherType (no
// subtype), so this is only reached for the outer DIX frame.
func classifyEtherTypeDetailed(et uint16, frame []byte, l3 int) (label, subtype string) {
	switch et {
	case 0x0806:
		return "ARP", classifyARP(frame, l3)
	case 0x0800:
		return classifyIPv4(frame, l3)
	case 0x86dd:
		return classifyIPv6(frame, l3)
	case 0x88cc:
		return "LLDP", ""
	case 0x9000:
		return "LOOP", ""
	case 0x8847:
		return "MPLS", ""
	default:
		return fmt.Sprintf("0x%04x", et), ""
	}
}

// classifyARP reads the ARP operation field (offset 6 within the ARP header):
// 1 = request, 2 = reply. Anything else (RARP op 3/4, unreadable) → "".
func classifyARP(frame []byte, l3 int) string {
	if len(frame) < l3+8 {
		return ""
	}
	switch uint16(frame[l3+6])<<8 | uint16(frame[l3+7]) {
	case 1:
		return "request"
	case 2:
		return "reply"
	}
	return ""
}

// classifyIPv4 reads the IPv4 protocol byte (offset 9 within the L3 header) to
// name the transport/routing protocol. IHL/options don't move that field. For
// TCP/UDP it peeks the L4 ports to name a well-known application; port reading
// is guarded by the IHL-derived L4 offset and skipped on fragments carrying a
// nonzero fragment offset (their L4 header isn't present at the front).
func classifyIPv4(frame []byte, l3 int) (label, subtype string) {
	if len(frame) < l3+10 {
		return "IPv4", ""
	}
	proto := frame[l3+9]
	switch proto {
	case 1:
		return classifyICMPv4(frame, l3)
	case 2:
		return "IGMP", ""
	case 6, 17:
		// TCP/UDP: peek ports unless this is a non-first fragment. The fragment
		// offset is the low 13 bits of the flags/frag-offset word at l3+6..7.
		frag := (uint16(frame[l3+6])<<8 | uint16(frame[l3+7])) & 0x1fff
		if frag != 0 {
			return transportName(proto), ""
		}
		// IHL (low nibble of byte 0) is the L3 header length in 32-bit words.
		ihl := int(frame[l3]&0x0f) * 4
		if ihl < 20 {
			ihl = 20 // malformed IHL; assume a minimal header
		}
		l4 := l3 + ihl
		return classifyTransport(proto, frame, l4)
	case 47:
		return "GRE", ""
	case 50:
		return "ESP", ""
	case 51:
		return "AH", ""
	case 88:
		return "EIGRP", classifyEIGRP(frame, l3)
	case 89:
		return "OSPF", classifyOSPF(frame, l3)
	case 103:
		return "PIM", ""
	case 112:
		return "VRRP", ""
	default:
		return "IPv4", ""
	}
}

// classifyOSPF reads the OSPFv2 packet-type byte at offset 1 of the OSPF header
// (which begins at the IPv4 payload, l3 + IHL): 1=hello 2=db-desc 3=ls-request
// 4=ls-update 5=ls-ack. The version byte at offset 0 is ignored. Unreadable or
// unknown type → "".
func classifyOSPF(frame []byte, l3 int) string {
	l4 := l3 + ipv4IHL(frame, l3)
	if len(frame) < l4+2 {
		return ""
	}
	switch frame[l4+1] {
	case 1:
		return "hello"
	case 2:
		return "db-desc"
	case 3:
		return "ls-request"
	case 4:
		return "ls-update"
	case 5:
		return "ls-ack"
	}
	return ""
}

// classifyEIGRP reads the EIGRP opcode byte at offset 1 of the EIGRP header
// (version byte at offset 0, opcode at offset 1): 1=update 2=request 3=query
// 4=reply 5=hello. Unreadable or unknown opcode → "".
func classifyEIGRP(frame []byte, l3 int) string {
	l4 := l3 + ipv4IHL(frame, l3)
	if len(frame) < l4+2 {
		return ""
	}
	switch frame[l4+1] {
	case 1:
		return "update"
	case 2:
		return "request"
	case 3:
		return "query"
	case 4:
		return "reply"
	case 5:
		return "hello"
	}
	return ""
}

// ipv4IHL returns the IPv4 header length in bytes from the IHL nibble, clamped
// to a 20-byte minimum for a malformed value — the same derivation the ICMP and
// port peeks use.
func ipv4IHL(frame []byte, l3 int) int {
	ihl := int(frame[l3]&0x0f) * 4
	if ihl < 20 {
		ihl = 20
	}
	return ihl
}

// classifyICMPv4 splits ICMPv4 echo request/reply (types 8/0) out as "PING"
// from other ICMPv4 ("ICMP"), and names the message subtype from the type byte:
// echo-request (8), echo-reply (0), unreachable (3), redirect (5),
// time-exceeded (11), else "other". The type byte is the first byte of the ICMP
// header at the L4 offset, derived from IHL like classifyIPv4's port peek. A
// frame too short to reach the type byte yields ("ICMP", "").
func classifyICMPv4(frame []byte, l3 int) (label, subtype string) {
	l4 := l3 + ipv4IHL(frame, l3)
	if len(frame) < l4+1 {
		return "ICMP", ""
	}
	switch frame[l4] {
	case 8:
		return "PING", "echo-request"
	case 0:
		return "PING", "echo-reply"
	case 3:
		return "ICMP", "unreachable"
	case 5:
		return "ICMP", "redirect"
	case 11:
		return "ICMP", "time-exceeded"
	default:
		return "ICMP", "other"
	}
}

// classifyIPv6 reads the fixed IPv6 next-header byte (offset 6 within the L3
// header). Extension headers would shift the true upper-layer proto, but naming
// the first next-header is enough for the traffic breakdown. TCP/UDP peek ports
// at the fixed 40-byte IPv6 header offset (no options to skip).
func classifyIPv6(frame []byte, l3 int) (label, subtype string) {
	if len(frame) < l3+7 {
		return "IPv6", ""
	}
	switch frame[l3+6] {
	case 6, 17:
		// IPv6 has no options, so the L4 header sits at the fixed 40-byte offset.
		return classifyTransport(frame[l3+6], frame, l3+40)
	case 50:
		return "ESP", ""
	case 51:
		return "AH", ""
	case 58:
		return "ICMPv6", ""
	case 88:
		return "EIGRP", ""
	case 89:
		return "OSPF", ""
	default:
		return "IPv6", ""
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

// classifyTransport names the TCP/UDP frame (via classifyPorts) and, for the
// application labels that carry a decodable message type, its subtype. Only BGP
// (TCP :179) decodes a subtype today; every other transport label returns "".
func classifyTransport(proto byte, frame []byte, l4 int) (label, subtype string) {
	label = classifyPorts(proto, frame, l4)
	if label == "BGP" {
		// The BGP header follows the TCP header. l4 is the TCP header offset; the
		// TCP data offset (header length) is the high nibble of the byte at l4+12,
		// in 32-bit words. Only decode when the whole TCP header — and the BGP
		// header behind it — is present in this frame (guard against a segmented
		// stream where the BGP header spilled into a later segment).
		if len(frame) >= l4+13 {
			tcpHdr := int(frame[l4+12]>>4) * 4
			if tcpHdr >= 20 {
				subtype = classifyBGP(frame, l4+tcpHdr)
			}
		}
	}
	return label, subtype
}

// classifyBGP reads the BGP message-type byte at offset 18 of the BGP header —
// 16-byte marker, 2-byte length, then the type byte: 1=open 2=update
// 3=notification 4=keepalive 5=route-refresh. bgp is the offset of the BGP
// header (start of the marker). Unreadable (segmented/short) or unknown → "".
func classifyBGP(frame []byte, bgp int) string {
	if len(frame) < bgp+19 {
		return ""
	}
	switch frame[bgp+18] {
	case 1:
		return "open"
	case 2:
		return "update"
	case 3:
		return "notification"
	case 4:
		return "keepalive"
	case 5:
		return "route-refresh"
	}
	return ""
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
