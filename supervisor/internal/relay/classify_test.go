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

	// IPv4/ICMP: ethertype 0x0800, protocol byte 1 at l3+9.
	ipv4icmp := ethHdr(0x0800, 0x45, 0, 0, 0, 0, 0, 0, 0, 0 /*ttl*/, 1 /*proto=ICMP*/, 0, 0)

	// VLAN-tagged IPv4/TCP: 0x8100 tag, inner 0x0800, protocol byte 6.
	vlanTCP := ethHdr(0x8100,
		0x00, 0x00, /* vlan tci */
		0x08, 0x00, /* inner ethertype */
		0x45, 0, 0, 0, 0, 0, 0, 0, 0, 6 /*proto=TCP*/, 0, 0)

	cases := []struct {
		name  string
		frame []byte
		want  string
	}{
		{"stp", stp, "STP"},
		{"cdp", cdp, "CDP"},
		{"arp", ethHdr(0x0806), "ARP"},
		{"ipv4-icmp", ipv4icmp, "ICMP"},
		{"vlan-ipv4-tcp", vlanTCP, "TCP"},
		{"loop", ethHdr(0x9000), "LOOP"},
		{"lldp", ethHdr(0x88cc), "LLDP"},
		{"mpls", ethHdr(0x8847), "MPLS"},
		{"runt", []byte{0x00, 0x01, 0x02}, "OTHER"},
		{"unknown-ethertype", ethHdr(0x1234), "0x1234"},
	}
	for _, c := range cases {
		if got := Classify(c.frame); got != c.want {
			t.Errorf("%s: Classify = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestClassifyIPv4Protocols spot-checks the IPv4 protocol-byte routing table so
// a wrong offset or missing case is caught.
func TestClassifyIPv4Protocols(t *testing.T) {
	cases := map[byte]string{
		1: "ICMP", 2: "IGMP", 6: "TCP", 17: "UDP", 47: "GRE",
		88: "EIGRP", 89: "OSPF", 103: "PIM", 112: "VRRP", 254: "IPv4",
	}
	for proto, want := range cases {
		f := ethHdr(0x0800, 0x45, 0, 0, 0, 0, 0, 0, 0, 0, proto, 0, 0)
		if got := Classify(f); got != want {
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
		if got := Classify(f); got != want {
			t.Errorf("ipv6 nh %d: Classify = %q, want %q", nh, got, want)
		}
	}
}
