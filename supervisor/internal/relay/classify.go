package relay

import "fmt"

// Classify labels an ethernet frame by the highest-layer protocol cheaply
// identifiable from a few fixed-offset byte peeks. It is called on the relay's
// forward hot path (once per forwarded datagram) to build the per-proto fps
// breakdown carried in link.stats events, so it must not allocate on the common
// path and must never panic on a short/malformed frame: every offset is bounds
// checked and anything unrecognised falls through to a coarse label.
//
// The frame is a raw ethernet frame (dst[6] src[6] ethertype/len[2] payload) —
// the mesh is headerless, so what the pump reads IS the frame on the wire.
// Frames shorter than a 14-byte ethernet header can't be classified and are
// reported as "OTHER" rather than dropped, so they still count toward totals.
func Classify(frame []byte) string {
	if len(frame) < 14 {
		return "OTHER"
	}

	// EtherType/length lives at offset 12. A single 802.1Q tag (0x8100) pushes
	// the real type out by 4 bytes; we peel at most one tag (Q-in-Q is rare in
	// these labs and not worth the hot-path cost).
	et := uint16(frame[12])<<8 | uint16(frame[13])
	l3 := 14
	if et == 0x8100 {
		if len(frame) < 18 {
			return "OTHER"
		}
		et = uint16(frame[16])<<8 | uint16(frame[17])
		l3 = 18
	}

	if et >= 0x0600 {
		return classifyEtherType(et, frame, l3)
	}
	// et < 0x0600 is an 802.3 length field: the payload is an LLC/SNAP header,
	// which is how STP, CDP, DTP and VTP appear on the wire.
	return classifyLLC(frame, l3)
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
// name the transport/routing protocol. IHL/options don't move that field.
func classifyIPv4(frame []byte, l3 int) string {
	if len(frame) < l3+10 {
		return "IPv4"
	}
	switch frame[l3+9] {
	case 1:
		return "ICMP"
	case 2:
		return "IGMP"
	case 6:
		return "TCP"
	case 17:
		return "UDP"
	case 47:
		return "GRE"
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

// classifyIPv6 reads the fixed IPv6 next-header byte (offset 6 within the L3
// header). Extension headers would shift the true upper-layer proto, but naming
// the first next-header is enough for the traffic breakdown.
func classifyIPv6(frame []byte, l3 int) string {
	if len(frame) < l3+7 {
		return "IPv6"
	}
	switch frame[l3+6] {
	case 6:
		return "TCP"
	case 17:
		return "UDP"
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

// classifyLLC decodes the 802.2 LLC (and optional SNAP) header that follows an
// 802.3 length field. STP rides raw LLC (DSAP/SSAP 0x42); CDP/DTP/VTP ride SNAP
// with Cisco's OUI; a SNAP OUI of 00:00:00 is really a tunnelled ethertype, so
// we recurse into the ethertype labels for it.
func classifyLLC(frame []byte, l3 int) string {
	if len(frame) < l3+3 {
		return "LLC"
	}
	dsap, ssap, ctrl := frame[l3], frame[l3+1], frame[l3+2]
	if dsap == 0x42 && ssap == 0x42 {
		return "STP"
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
