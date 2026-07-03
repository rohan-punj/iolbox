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
