package attackcommon

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

// independentChecksum re-implements RFC 1071 separately from the package
// under test, so a checksum test never just calls itself. It returns the
// ones-complement of the folded sum — same contract as internetChecksum.
// The self-verification invariant this enables: summing a correctly-checksummed
// buffer (data bytes + the checksum field already in place) folds to a raw
// sum of 0xffff, whose ones-complement is 0x0000 — so a valid checksum makes
// independentChecksum(wholeBuffer) return 0, not 0xffff.
func independentChecksum(data []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(data[i])<<8 | uint32(data[i+1])
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for sum > 0xffff {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func TestBuildIPv4Fields(t *testing.T) {
	src := net.IPv4(10, 0, 0, 1)
	dst := net.IPv4(224, 0, 0, 5)
	payload := []byte{0xde, 0xad, 0xbe, 0xef}
	pkt := BuildIPv4(src, dst, 89, payload)

	if len(pkt) != 20+len(payload) {
		t.Fatalf("len = %d, want %d", len(pkt), 20+len(payload))
	}
	if pkt[0] != 0x45 {
		t.Errorf("version/ihl = %#x, want 0x45", pkt[0])
	}
	if pkt[1] != 0 {
		t.Errorf("tos = %d, want 0", pkt[1])
	}
	if got := binary.BigEndian.Uint16(pkt[2:4]); got != uint16(len(pkt)) {
		t.Errorf("total length = %d, want %d", got, len(pkt))
	}
	if got := binary.BigEndian.Uint16(pkt[4:6]); got != 1 {
		t.Errorf("id = %d, want 1 (Scapy default)", got)
	}
	if got := binary.BigEndian.Uint16(pkt[6:8]); got != 0 {
		t.Errorf("flags/fragoffset = %#x, want 0", got)
	}
	if pkt[8] != 64 {
		t.Errorf("ttl = %d, want 64", pkt[8])
	}
	if pkt[9] != 89 {
		t.Errorf("proto = %d, want 89", pkt[9])
	}
	if !bytes.Equal(pkt[12:16], src.To4()) {
		t.Errorf("src = %v, want %v", pkt[12:16], src.To4())
	}
	if !bytes.Equal(pkt[16:20], dst.To4()) {
		t.Errorf("dst = %v, want %v", pkt[16:20], dst.To4())
	}
	if !bytes.Equal(pkt[20:], payload) {
		t.Errorf("payload = %v, want %v", pkt[20:], payload)
	}
	// The header checksum must make the header itself sum to 0xffff — the
	// standard self-verification invariant, computed by an independent
	// implementation, not internetChecksum from the package under test.
	if got := independentChecksum(pkt[:20]); got != 0 {
		t.Errorf("header checksum invariant: independentChecksum(header) = %#04x, want 0", got)
	}
}

func TestBuildUDPChecksumValidIPv4(t *testing.T) {
	src := net.IPv4(192, 168, 1, 10)
	dst := net.IPv4(255, 255, 255, 255)
	payload := []byte("iolbox-secbench-test-payload")
	seg := BuildUDP(src, dst, 68, 67, payload, false)

	if len(seg) != 8+len(payload) {
		t.Fatalf("len = %d, want %d", len(seg), 8+len(payload))
	}
	if got := binary.BigEndian.Uint16(seg[0:2]); got != 68 {
		t.Errorf("sport = %d, want 68", got)
	}
	if got := binary.BigEndian.Uint16(seg[2:4]); got != 67 {
		t.Errorf("dport = %d, want 67", got)
	}
	if got := binary.BigEndian.Uint16(seg[4:6]); got != uint16(len(seg)) {
		t.Errorf("udp length = %d, want %d", got, len(seg))
	}
	pseudo := make([]byte, 12)
	copy(pseudo[0:4], src.To4())
	copy(pseudo[4:8], dst.To4())
	pseudo[9] = 17
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(seg)))
	if got := independentChecksum(append(pseudo, seg...)); got != 0 {
		t.Errorf("IPv4 UDP checksum invariant = %#04x, want 0", got)
	}
}

func TestBuildUDPChecksumValidIPv6(t *testing.T) {
	src := net.ParseIP("fe80::1")
	dst := net.ParseIP("ff02::1")
	payload := []byte("ra-spoof-dhcpv6")
	seg := BuildUDP(src, dst, 547, 546, payload, true)

	pseudo := make([]byte, 40)
	copy(pseudo[0:16], src.To16())
	copy(pseudo[16:32], dst.To16())
	binary.BigEndian.PutUint32(pseudo[32:36], uint32(len(seg)))
	pseudo[39] = 17
	if got := independentChecksum(append(pseudo, seg...)); got != 0 {
		t.Errorf("IPv6 UDP checksum invariant = %#04x, want 0", got)
	}
}

func TestBuildICMPEchoFields(t *testing.T) {
	pkt := BuildICMPEcho(0x1234, 0x0001, nil)
	if len(pkt) != 8 {
		t.Fatalf("len = %d, want 8", len(pkt))
	}
	if pkt[0] != 8 || pkt[1] != 0 {
		t.Errorf("type/code = %d/%d, want 8/0", pkt[0], pkt[1])
	}
	if got := binary.BigEndian.Uint16(pkt[4:6]); got != 0x1234 {
		t.Errorf("id = %#04x, want 0x1234", got)
	}
	if got := binary.BigEndian.Uint16(pkt[6:8]); got != 0x0001 {
		t.Errorf("seq = %#04x, want 0x0001", got)
	}
	if got := independentChecksum(pkt); got != 0 {
		t.Errorf("ICMP checksum invariant = %#04x, want 0", got)
	}
}

func macFor(t *testing.T, s string) net.HardwareAddr {
	t.Helper()
	mac, err := net.ParseMAC(s)
	if err != nil {
		t.Fatalf("ParseMAC(%q): %v", s, err)
	}
	return mac
}

func TestBuildDHCPDiscoverGolden(t *testing.T) {
	srcMAC := macFor(t, "02:00:00:aa:bb:cc")
	var chaddr [6]byte
	copy(chaddr[:], []byte{0x02, 0x00, 0x00, 0x11, 0x22, 0x33})
	frame := BuildDHCPDiscover(0xdeadbeef, chaddr, srcMAC)

	dstMAC, gotSrcMAC, ethertype, _, ipPayload := ParseEthernet(frame)
	if !bytes.Equal(dstMAC, net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) {
		t.Errorf("dst MAC = %v, want broadcast", dstMAC)
	}
	if !bytes.Equal(gotSrcMAC, srcMAC) {
		t.Errorf("src MAC = %v, want %v", gotSrcMAC, srcMAC)
	}
	if ethertype != 0x0800 {
		t.Fatalf("ethertype = %#04x, want 0x0800", ethertype)
	}
	srcIP, dstIP, sport, dport, udpPayload, ok := ParseIPv4UDP(ipPayload)
	if !ok {
		t.Fatalf("ParseIPv4UDP failed")
	}
	if !srcIP.Equal(net.IPv4zero) {
		t.Errorf("IP src = %v, want 0.0.0.0", srcIP)
	}
	if !dstIP.Equal(net.IPv4bcast) {
		t.Errorf("IP dst = %v, want 255.255.255.255", dstIP)
	}
	if sport != 68 || dport != 67 {
		t.Errorf("ports = %d/%d, want 68/67", sport, dport)
	}
	if len(udpPayload) != 236+4+4 {
		t.Fatalf("BOOTP+options length = %d, want %d", len(udpPayload), 236+4+4)
	}
	if udpPayload[0] != 1 {
		t.Errorf("bootp op = %d, want 1 (BOOTREQUEST)", udpPayload[0])
	}
	if got := binary.BigEndian.Uint32(udpPayload[4:8]); got != 0xdeadbeef {
		t.Errorf("xid = %#08x, want 0xdeadbeef", got)
	}
	if got := binary.BigEndian.Uint16(udpPayload[10:12]); got != 0x8000 {
		t.Errorf("flags = %#04x, want 0x8000 (broadcast bit)", got)
	}
	if !bytes.Equal(udpPayload[28:34], chaddr[:]) {
		t.Errorf("chaddr = %v, want %v", udpPayload[28:34], chaddr)
	}
	// chaddr padding: the 10 bytes after the real 6-byte MAC must stay zero.
	if !bytes.Equal(udpPayload[34:44], make([]byte, 10)) {
		t.Errorf("chaddr padding not zero: %v", udpPayload[34:44])
	}
	if !bytes.Equal(udpPayload[236:240], []byte{0x63, 0x82, 0x53, 0x63}) {
		t.Errorf("DHCP magic cookie = %v, want 63:82:53:63", udpPayload[236:240])
	}
	if !bytes.Equal(udpPayload[240:], []byte{53, 1, 1, 255}) {
		t.Errorf("options = %v, want [53 1 1 255] (message-type=discover, end)", udpPayload[240:])
	}
}

func TestBuildDHCPOfferGolden(t *testing.T) {
	srcMAC := macFor(t, "02:00:00:aa:bb:cc")
	var chaddr [6]byte
	copy(chaddr[:], []byte{0x02, 0x00, 0x00, 0x11, 0x22, 0x33})
	gateway := net.IPv4(192, 168, 1, 66)
	dns := net.IPv4(192, 168, 1, 66)
	offerIP := net.IPv4(192, 168, 1, 100)
	frame := BuildDHCPOffer(0xdeadbeef, chaddr, offerIP, gateway, dns, 3600, srcMAC)

	_, _, _, _, ipPayload := ParseEthernet(frame)
	srcIP, dstIP, sport, dport, udpPayload, ok := ParseIPv4UDP(ipPayload)
	if !ok {
		t.Fatalf("ParseIPv4UDP failed")
	}
	if !srcIP.Equal(gateway) {
		t.Errorf("IP src = %v, want gateway %v", srcIP, gateway)
	}
	if !dstIP.Equal(net.IPv4bcast) {
		t.Errorf("IP dst = %v, want 255.255.255.255", dstIP)
	}
	if sport != 67 || dport != 68 {
		t.Errorf("ports = %d/%d, want 67/68", sport, dport)
	}
	if udpPayload[0] != 2 {
		t.Errorf("bootp op = %d, want 2 (BOOTREPLY)", udpPayload[0])
	}
	if !bytes.Equal(udpPayload[16:20], offerIP.To4()) {
		t.Errorf("yiaddr = %v, want %v", udpPayload[16:20], offerIP.To4())
	}
	if !bytes.Equal(udpPayload[20:24], gateway.To4()) {
		t.Errorf("siaddr = %v, want gateway %v", udpPayload[20:24], gateway.To4())
	}

	wantOptions := []byte{
		53, 1, 2, // message-type = offer
		54, 4, 192, 168, 1, 66, // server_id = gateway
		51, 4, 0, 0, 0x0e, 0x10, // lease_time = 3600 = 0x0e10
		3, 4, 192, 168, 1, 66, // router = gateway
		6, 4, 192, 168, 1, 66, // name_server = dns
		1, 4, 255, 255, 255, 0, // subnet_mask
		255, // end
	}
	gotOptions := udpPayload[240:]
	if !bytes.Equal(gotOptions, wantOptions) {
		t.Errorf("options =\n%v\nwant\n%v", gotOptions, wantOptions)
	}
}

func TestParseEthernetDoubleTagged(t *testing.T) {
	frame := []byte{}
	frame = append(frame, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff) // dst
	frame = append(frame, 0x02, 0x00, 0x00, 0x11, 0x22, 0x33) // src
	frame = append(frame, 0x81, 0x00, 0x00, 0x01)             // outer Dot1Q, vlan=1, next=Dot1Q
	frame = append(frame, 0x81, 0x00, 0x00, 0x14)             // inner Dot1Q, vlan=20, next=IPv4
	frame = append(frame, 0x08, 0x00)                         // ethertype IPv4
	frame = append(frame, 0xaa, 0xbb, 0xcc)                   // payload stub

	dst, src, ethertype, vlans, payload := ParseEthernet(frame)
	if !bytes.Equal(dst, net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) {
		t.Errorf("dst = %v", dst)
	}
	if !bytes.Equal(src, net.HardwareAddr{0x02, 0x00, 0x00, 0x11, 0x22, 0x33}) {
		t.Errorf("src = %v", src)
	}
	if ethertype != 0x0800 {
		t.Errorf("ethertype = %#04x, want 0x0800", ethertype)
	}
	if len(vlans) != 2 || vlans[0] != 1 || vlans[1] != 20 {
		t.Errorf("vlans = %v, want [1 20]", vlans)
	}
	if !bytes.Equal(payload, []byte{0xaa, 0xbb, 0xcc}) {
		t.Errorf("payload = %v, want [aa bb cc]", payload)
	}
}

func TestParseDHCP6ClientIDVerbatim(t *testing.T) {
	// optcode=1 (Client-ID), optlen=10, duid = the same raw 10-byte DUID
	// literal ra_spoof.py embeds ("0003000102000000aabb") — real-shaped data,
	// not an arbitrary fixture.
	duid := []byte{0x00, 0x03, 0x00, 0x01, 0x02, 0x00, 0x00, 0x00, 0xaa, 0xbb}
	clientID := []byte{0x00, 0x01, 0x00, byte(len(duid))}
	clientID = append(clientID, duid...)
	msg := []byte{1, 0x11, 0x22, 0x33} // msgtype=1 (Solicit), trid=0x112233
	msg = append(msg, clientID...)

	msgType, trid, gotClientID, ok := ParseDHCP6(msg)
	if !ok {
		t.Fatalf("ParseDHCP6 failed")
	}
	if msgType != 1 {
		t.Errorf("msgType = %d, want 1", msgType)
	}
	if trid != ([3]byte{0x11, 0x22, 0x33}) {
		t.Errorf("trid = %v, want [11 22 33]", trid)
	}
	if !bytes.Equal(gotClientID, clientID) {
		t.Errorf("clientID = %v, want %v (verbatim, not reconstructed)", gotClientID, clientID)
	}
}

func TestHostsInCIDR(t *testing.T) {
	t.Run("/31 includes both addresses", func(t *testing.T) {
		hosts, err := HostsInCIDR("192.168.1.0/31")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"192.168.1.0", "192.168.1.1"}
		if len(hosts) != len(want) {
			t.Fatalf("got %d hosts, want %d: %v", len(hosts), len(want), hosts)
		}
		for i, h := range hosts {
			if h.String() != want[i] {
				t.Errorf("hosts[%d] = %v, want %v", i, h, want[i])
			}
		}
	})

	t.Run("/32 is a single address", func(t *testing.T) {
		hosts, err := HostsInCIDR("10.0.0.5/32")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(hosts) != 1 || hosts[0].String() != "10.0.0.5" {
			t.Fatalf("hosts = %v, want [10.0.0.5]", hosts)
		}
	})

	t.Run("/22 is the allowed boundary", func(t *testing.T) {
		hosts, err := HostsInCIDR("172.16.0.0/22")
		if err != nil {
			t.Fatalf("unexpected error at /22 boundary: %v", err)
		}
		if len(hosts) != 1024 {
			t.Fatalf("got %d hosts, want 1024", len(hosts))
		}
		// Inclusive range: network AND broadcast addresses are both present,
		// matching Scapy's Net() (not a routing-aware host enumerator).
		if hosts[0].String() != "172.16.0.0" {
			t.Errorf("first host = %v, want network address 172.16.0.0", hosts[0])
		}
		if hosts[len(hosts)-1].String() != "172.16.3.255" {
			t.Errorf("last host = %v, want broadcast address 172.16.3.255", hosts[len(hosts)-1])
		}
	})

	t.Run("/21 is rejected as too broad", func(t *testing.T) {
		_, err := HostsInCIDR("172.16.0.0/21")
		if err == nil {
			t.Fatalf("expected an error for a /21 (broader than the /22 limit)")
		}
	})

	t.Run("malformed CIDR is rejected", func(t *testing.T) {
		_, err := HostsInCIDR("not-a-cidr")
		if err == nil {
			t.Fatalf("expected an error for a malformed CIDR")
		}
	})
}
