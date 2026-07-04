package relay

import "testing"

// ethHdr builds a 14-byte ethernet header (dst, src, ethertype/len) followed by
// the given payload, so the test vectors read like on-the-wire frames.
func ethHdr(etOrLen uint16, payload ...byte) []byte {
	f := make([]byte, 14, 14+len(payload))
	// dst/src are left zero except where a test overrides them; only the
	// ethertype/length field and payload drive Classify.
	f[12] = byte(etOrLen >> 8)
	f[13] = byte(etOrLen)
	return append(f, payload...)
}

func TestClassify(t *testing.T) {
	// STP BPDU: 802.3 length frame, dst 01:80:c2:00:00:00, LLC DSAP/SSAP/ctrl
	// 42 42 03.
	stp := ethHdr(0x0026, 0x42, 0x42, 0x03, 0x00, 0x00)
	copy(stp[0:6], []byte{0x01, 0x80, 0xc2, 0x00, 0x00, 0x00})

	// CDP: 802.3 length, dst 01:00:0c:cc:cc:cc, SNAP AA AA 03, OUI 00:00:0c,
	// PID 0x2000.
	cdp := ethHdr(0x0032, 0xAA, 0xAA, 0x03, 0x00, 0x00, 0x0c, 0x20, 0x00)
	copy(cdp[0:6], []byte{0x01, 0x00, 0x0c, 0xcc, 0xcc, 0xcc})

	// IPv4/ICMP echo request (type 8) -> PING; a non-echo ICMP type -> ICMP.
	// IHL=5 (20-byte header) so L4 sits at l3+20; the ICMP type is its 1st byte.
	ping := ipv4(1 /*ICMP*/, 8 /*echo request*/, 0)
	icmpOther := ipv4(1 /*ICMP*/, 3 /*dest unreach*/, 0)

	// VLAN-tagged IPv4/TCP: 0x8100 tag, inner 0x0800, protocol byte 6.
	vlanTCP := ethHdr(0x8100,
		0x00, 0x00, /* vlan tci */
		0x08, 0x00, /* inner ethertype */
		0x45, 0, 0, 0, 0, 0, 0, 0, 0, 6 /*proto=TCP*/, 0, 0)

	// ISIS: 802.3 length frame, raw LLC DSAP/SSAP 0xFE/0xFE.
	isis := ethHdr(0x0026, 0xFE, 0xFE, 0x03, 0x00, 0x00)

	cases := []struct {
		name  string
		frame []byte
		want  string
	}{
		{"stp", stp, "STP"},
		{"cdp", cdp, "CDP"},
		{"isis", isis, "ISIS"},
		{"arp", ethHdr(0x0806), "ARP"},
		{"ping", ping, "PING"},
		{"icmp-other", icmpOther, "ICMP"},
		{"vlan-ipv4-tcp", vlanTCP, "TCP"},
		{"loop", ethHdr(0x9000), "LOOP"},
		{"lldp", ethHdr(0x88cc), "LLDP"},
		{"mpls", ethHdr(0x8847), "MPLS"},
		{"runt", []byte{0x00, 0x01, 0x02}, "OTHER"},
		{"unknown-ethertype", ethHdr(0x1234), "0x1234"},
	}
	for _, c := range cases {
		if got, _ := Classify(c.frame); got != c.want {
			t.Errorf("%s: Classify = %q, want %q", c.name, got, c.want)
		}
	}
}

// ipv4 builds an ethernet-II IPv4 frame (IHL=5) carrying the given protocol,
// followed by up to two L4 header bytes so port/type peeks have something to
// read. Extra L4 bytes beyond b0/b1 aren't needed by the current classifier.
func ipv4(proto, b0, b1 byte) []byte {
	// L3: ver/IHL=0x45, then 8 bytes to the proto at offset 9, then src/dst IP.
	return ethHdr(0x0800,
		0x45, 0, 0, 0, /* ver/ihl, tos, total len */
		0, 0, 0, 0, /* id, flags/frag=0 */
		64, proto, 0, 0, /* ttl, proto, checksum */
		0, 0, 0, 0, /* src ip */
		0, 0, 0, 0, /* dst ip */
		b0, b1) // L4 first two bytes (icmp type/code, or high half of a port pair)
}

// ipv4Ports builds an IPv4 TCP/UDP frame (IHL=5) with explicit src/dst L4 ports
// so the well-known-port labels can be exercised.
func ipv4Ports(proto byte, srcPort, dstPort uint16) []byte {
	return ethHdr(0x0800,
		0x45, 0, 0, 0,
		0, 0, 0, 0,
		64, proto, 0, 0,
		0, 0, 0, 0,
		0, 0, 0, 0,
		byte(srcPort>>8), byte(srcPort), byte(dstPort>>8), byte(dstPort))
}

// TestClassifyPorts checks the well-known TCP/UDP port labels, matching on
// either source or destination port.
func TestClassifyPorts(t *testing.T) {
	cases := []struct {
		name     string
		proto    byte
		src, dst uint16
		want     string
	}{
		{"bgp-dst", 6, 50000, 179, "BGP"},
		{"bgp-src", 6, 179, 50000, "BGP"},
		{"tacacs", 6, 40000, 49, "TACACS"},
		{"tcp-plain", 6, 12345, 23, "TCP"},
		{"rip", 17, 520, 520, "RIP"},
		{"vxlan", 17, 44000, 4789, "VXLAN"},
		{"radius-1812", 17, 44000, 1812, "RADIUS"},
		{"radius-1646", 17, 1646, 44000, "RADIUS"},
		{"udp-plain", 17, 12345, 53, "UDP"},
	}
	for _, c := range cases {
		if got, _ := Classify(ipv4Ports(c.proto, c.src, c.dst)); got != c.want {
			t.Errorf("%s: Classify = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestClassifyEspAh checks the IPsec protocol labels on both IPv4 and IPv6.
func TestClassifyEspAh(t *testing.T) {
	if got, _ := Classify(ipv4(50, 0, 0)); got != "ESP" {
		t.Errorf("ipv4 esp: got %q, want ESP", got)
	}
	if got, _ := Classify(ipv4(51, 0, 0)); got != "AH" {
		t.Errorf("ipv4 ah: got %q, want AH", got)
	}
	// IPv6 next-header at offset 6 within L3.
	esp6 := ethHdr(0x86dd, 0x60, 0, 0, 0, 0, 0, 50, 64)
	ah6 := ethHdr(0x86dd, 0x60, 0, 0, 0, 0, 0, 51, 64)
	if got, _ := Classify(esp6); got != "ESP" {
		t.Errorf("ipv6 esp: got %q, want ESP", got)
	}
	if got, _ := Classify(ah6); got != "AH" {
		t.Errorf("ipv6 ah: got %q, want AH", got)
	}
}

// TestClassifyTagged verifies an 802.1Q frame reports tagged=true and still
// classifies to its inner protocol, while an untagged frame reports false.
func TestClassifyTagged(t *testing.T) {
	vlanTCP := ethHdr(0x8100,
		0x00, 0x00,
		0x08, 0x00,
		0x45, 0, 0, 0, 0, 0, 0, 0, 0, 6, 0, 0)
	label, tagged := Classify(vlanTCP)
	if label != "TCP" || !tagged {
		t.Errorf("tagged tcp: got (%q, %v), want (TCP, true)", label, tagged)
	}
	if _, tagged := Classify(ethHdr(0x0806)); tagged {
		t.Errorf("untagged arp: tagged = true, want false")
	}
}

// TestClassifyFragmentNoPortMisread ensures a non-first fragment (nonzero
// fragment offset) is NOT port-peeked: its L4 header isn't at the front, so
// reading "ports" there would mislabel it. Even with bytes that look like a
// BGP port at the L4 offset, the fragment must fall back to the transport label.
func TestClassifyFragmentNoPortMisread(t *testing.T) {
	// IPv4 TCP with frag offset = 1 (flags/frag word at l3+6..7 = 0x0001), and
	// bytes at the L4 offset that would read as dst port 179 (BGP) if peeked.
	f := ethHdr(0x0800,
		0x45, 0, 0, 0,
		0, 0, 0x00, 0x01, /* flags/frag: nonzero offset */
		64, 6 /*TCP*/, 0, 0,
		0, 0, 0, 0,
		0, 0, 0, 0,
		0, 0, 0x00, 0xB3 /* would be dst port 179 */)
	if got, _ := Classify(f); got != "TCP" {
		t.Errorf("fragment: got %q, want TCP (ports must not be read)", got)
	}
}

// TestClassifyIPv4Protocols spot-checks the IPv4 protocol-byte routing table so
// a wrong offset or missing case is caught.
func TestClassifyIPv4Protocols(t *testing.T) {
	// proto 1 with echo-request type -> PING (see TestClassify for the ICMP
	// split); the plain protocol-byte table below uses non-ICMP protos plus a
	// bare ICMP (type 0 = echo reply also PING, so use type 3 for the "ICMP"
	// label case in TestClassify instead). Here proto 6/17 have no ports set
	// (zero) so they fall back to TCP/UDP.
	cases := map[byte]string{
		2: "IGMP", 6: "TCP", 17: "UDP", 47: "GRE", 50: "ESP", 51: "AH",
		88: "EIGRP", 89: "OSPF", 103: "PIM", 112: "VRRP", 254: "IPv4",
	}
	for proto, want := range cases {
		f := ethHdr(0x0800, 0x45, 0, 0, 0, 0, 0, 0, 0, 0, proto, 0, 0)
		if got, _ := Classify(f); got != want {
			t.Errorf("ipv4 proto %d: Classify = %q, want %q", proto, got, want)
		}
	}
}

// TestClassifyIPv6NextHeader checks the IPv6 next-header labels including the
// ICMPv6-specific value.
func TestClassifyIPv6NextHeader(t *testing.T) {
	cases := map[byte]string{58: "ICMPv6", 6: "TCP", 17: "UDP", 89: "OSPF", 44: "IPv6"}
	for nh, want := range cases {
		// IPv6 header: next-header sits at offset 6 within L3.
		f := ethHdr(0x86dd, 0x60, 0, 0, 0, 0, 0, nh, 64)
		if got, _ := Classify(f); got != want {
			t.Errorf("ipv6 nh %d: Classify = %q, want %q", nh, got, want)
		}
	}
}

// arpFrame builds an ethernet-II ARP frame (ethertype 0x0806) whose operation
// field (offset 6 in the ARP header) is op. Only the op field drives the ARP
// subtype, so the preceding hardware/protocol type fields are left zero.
func arpFrame(op uint16) []byte {
	return ethHdr(0x0806,
		0, 0, /* htype */
		0, 0, /* ptype */
		0, 0, /* hlen, plen */
		byte(op >> 8), byte(op))
}

// ipv4Proto builds an ethernet-II IPv4 frame (IHL=5) carrying the given
// protocol, followed by an arbitrary-length L4 payload so a subtype byte at a
// fixed offset can be reached. Mirrors the ipv4 helper but without the fixed
// two-byte cap so BGP's deep offset is reachable.
func ipv4Proto(proto byte, l4 ...byte) []byte {
	return ethHdr(0x0800, append([]byte{
		0x45, 0, 0, 0,
		0, 0, 0, 0,
		64, proto, 0, 0,
		0, 0, 0, 0,
		0, 0, 0, 0,
	}, l4...)...)
}

// bgpFrame builds an IPv4 TCP frame to/from port 179 with a minimal 20-byte TCP
// header (data offset nibble = 5) followed by a BGP header whose message-type
// byte (offset 18: 16-byte marker + 2-byte length + type) is msgType.
func bgpFrame(msgType byte) []byte {
	tcp := []byte{
		0x00, 0xB3, 0xC3, 0x50, /* src 179, dst 50000 */
		0, 0, 0, 0, /* seq */
		0, 0, 0, 0, /* ack */
		0x50, 0, 0, 0, /* data offset=5 (20 bytes), flags, window */
		0, 0, 0, 0, /* checksum, urgent */
	}
	bgp := make([]byte, 19) // 16 marker + 2 len + 1 type
	bgp[18] = msgType
	return ipv4Proto(6, append(tcp, bgp...)...)
}

// TestClassifyDetailedSubtypes exercises the per-protocol subtype vocabulary
// emitted by ClassifyDetailed with crafted frames per subtype, and confirms the
// primary label + tagged bits still match Classify.
func TestClassifyDetailedSubtypes(t *testing.T) {
	cases := []struct {
		name        string
		frame       []byte
		wantLabel   string
		wantSubtype string
	}{
		// ICMP: type byte is the first byte of the L4 payload (l3+20 for IHL=5).
		{"icmp-echo-request", ipv4(1, 8, 0), "PING", "echo-request"},
		{"icmp-echo-reply", ipv4(1, 0, 0), "PING", "echo-reply"},
		{"icmp-unreachable", ipv4(1, 3, 0), "ICMP", "unreachable"},
		{"icmp-redirect", ipv4(1, 5, 0), "ICMP", "redirect"},
		{"icmp-time-exceeded", ipv4(1, 11, 0), "ICMP", "time-exceeded"},
		{"icmp-other", ipv4(1, 9, 0), "ICMP", "other"},

		// OSPF (proto 89): version byte then packet-type byte.
		{"ospf-hello", ipv4Proto(89, 2, 1), "OSPF", "hello"},
		{"ospf-db-desc", ipv4Proto(89, 2, 2), "OSPF", "db-desc"},
		{"ospf-ls-request", ipv4Proto(89, 2, 3), "OSPF", "ls-request"},
		{"ospf-ls-update", ipv4Proto(89, 2, 4), "OSPF", "ls-update"},
		{"ospf-ls-ack", ipv4Proto(89, 2, 5), "OSPF", "ls-ack"},
		{"ospf-unknown", ipv4Proto(89, 2, 9), "OSPF", ""},

		// EIGRP (proto 88): version byte then opcode byte.
		{"eigrp-update", ipv4Proto(88, 2, 1), "EIGRP", "update"},
		{"eigrp-request", ipv4Proto(88, 2, 2), "EIGRP", "request"},
		{"eigrp-query", ipv4Proto(88, 2, 3), "EIGRP", "query"},
		{"eigrp-reply", ipv4Proto(88, 2, 4), "EIGRP", "reply"},
		{"eigrp-hello", ipv4Proto(88, 2, 5), "EIGRP", "hello"},
		{"eigrp-unknown", ipv4Proto(88, 2, 9), "EIGRP", ""},

		// BGP (TCP :179): message-type byte after the 16-byte marker + length.
		{"bgp-open", bgpFrame(1), "BGP", "open"},
		{"bgp-update", bgpFrame(2), "BGP", "update"},
		{"bgp-notification", bgpFrame(3), "BGP", "notification"},
		{"bgp-keepalive", bgpFrame(4), "BGP", "keepalive"},
		{"bgp-route-refresh", bgpFrame(5), "BGP", "route-refresh"},
		{"bgp-unknown", bgpFrame(9), "BGP", ""},

		// ARP op field.
		{"arp-request", arpFrame(1), "ARP", "request"},
		{"arp-reply", arpFrame(2), "ARP", "reply"},
		{"arp-rarp", arpFrame(3), "ARP", ""},

		// Labels with no subtype vocabulary → "".
		{"tcp-plain", ipv4Ports(6, 12345, 23), "TCP", ""},
		{"stp-no-subtype", func() []byte {
			f := ethHdr(0x0026, 0x42, 0x42, 0x03, 0x00, 0x00)
			copy(f[0:6], []byte{0x01, 0x80, 0xc2, 0x00, 0x00, 0x00})
			return f
		}(), "STP", ""},
	}
	for _, c := range cases {
		label, subtype, _ := ClassifyDetailed(c.frame)
		if label != c.wantLabel || subtype != c.wantSubtype {
			t.Errorf("%s: ClassifyDetailed = (%q, %q), want (%q, %q)",
				c.name, label, subtype, c.wantLabel, c.wantSubtype)
		}
		// Classify must agree on the label and delegate cleanly.
		if got, _ := Classify(c.frame); got != c.wantLabel {
			t.Errorf("%s: Classify label = %q, want %q", c.name, got, c.wantLabel)
		}
	}
}

// TestClassifyDetailedShortFrameNoSubtype ensures a frame too short to reach the
// subtype byte yields an empty subtype (never a panic or a misread) while the
// coarse label still resolves. A BGP TCP frame truncated before the BGP header
// is the segmentation guard case.
func TestClassifyDetailedShortFrameNoSubtype(t *testing.T) {
	// IPv4 TCP to port 179 with only the 20-byte TCP header, no BGP header.
	tcpOnly := ipv4Proto(6,
		0x00, 0xB3, 0xC3, 0x50,
		0, 0, 0, 0,
		0, 0, 0, 0,
		0x50, 0, 0, 0,
		0, 0, 0, 0)
	if label, subtype, _ := ClassifyDetailed(tcpOnly); label != "BGP" || subtype != "" {
		t.Errorf("segmented BGP: got (%q, %q), want (BGP, \"\")", label, subtype)
	}
	// A runt frame classifies OTHER with no subtype.
	if label, subtype, _ := ClassifyDetailed([]byte{0, 1, 2}); label != "OTHER" || subtype != "" {
		t.Errorf("runt: got (%q, %q), want (OTHER, \"\")", label, subtype)
	}
}
