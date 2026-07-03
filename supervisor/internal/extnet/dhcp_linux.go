//go:build linux

package extnet

import (
	"encoding/binary"
	"net"
)

// dhcpServer answers DHCP requests a lab node broadcasts on the relay side of a
// nat endpoint. A lab node's `ip dhcp` sends DISCOVER/REQUEST to UDP :67 as a
// raw ethernet frame; that frame arrives over the relay (NOT off the tap — the
// tap is the kernel/NAT side), so the server is consulted on the relay->node
// pump, decodes the ethernet+IPv4+UDP envelope, runs the pure DHCP codec
// (dhcp.go), and returns a fully-framed ethernet reply the pump sends straight
// back toward the lab node over the relay. The exchange never reaches the
// kernel, so there are no privileged sockets and no host DHCP conflicts; real
// IP traffic and ARP (the kernel owns the gateway IP on the tap) still flow
// through the tap normally.
type dhcpServer struct {
	serverIP net.IP // the gateway/server address (172.31.<n>.1)
	mask     net.IP // 255.255.255.0
	leaser   *leaser
	serverMA net.HardwareAddr // our source MAC on the tap (derived from the gateway IP)
}

// newDHCPServer builds the server for a nat subnet.
func newDHCPServer(gateway net.IP, sub Subnet) *dhcpServer {
	return &dhcpServer{
		serverIP: gateway.To4(),
		mask:     net.IPv4(255, 255, 255, 0).To4(),
		leaser:   newLeaser(net.ParseIP(sub.PoolStart()), net.ParseIP(sub.PoolEnd())),
		serverMA: gatewayMAC(gateway),
	}
}

// ethernet/IP/UDP header sizes for the frames we parse and build.
const (
	ethHdrLen      = 14
	ipHdrLen       = 20
	udpHdrLen      = 8
	etherTypeIPv4  = 0x0800
	ipProtoUDP     = 17
	dhcpServerPort = 67
	dhcpClientPort = 68
)

// consume inspects a frame arriving from the lab node (the relay side): if it
// is a DHCP request (IPv4/UDP to port 67) it returns the fully-framed ethernet
// reply to send back toward the lab node and consumed=true, so the pump does
// NOT deliver the request into the tap/kernel. A consumed request with no reply
// (e.g. RELEASE) returns (nil, true). Any other frame returns (nil, false) and
// is delivered to the tap normally.
func (d *dhcpServer) consume(frame []byte) (reply []byte, consumed bool) {
	req, srcMAC, ok := parseDHCP(frame)
	if !ok {
		return nil, false
	}
	offer := d.leaser.lease(srcMAC)
	replyType, r, ok := req.Handle(offer)
	if !ok {
		return nil, true // consumed (e.g. RELEASE) but no reply to send
	}
	payload := r.Encode(replyType, d.serverIP, d.mask)
	return d.buildFrame(srcMAC, r.YIAddr, payload, req.Flags&flagBroadcast != 0), true
}

// parseDHCP validates that a frame is an IPv4/UDP:67 DHCP request and returns
// the decoded packet plus the client's source MAC. ok=false for anything else.
func parseDHCP(frame []byte) (pkt *Packet, srcMAC net.HardwareAddr, ok bool) {
	if len(frame) < ethHdrLen+ipHdrLen+udpHdrLen {
		return nil, nil, false
	}
	if binary.BigEndian.Uint16(frame[12:14]) != etherTypeIPv4 {
		return nil, nil, false
	}
	src := net.HardwareAddr(append([]byte(nil), frame[6:12]...))
	ip := frame[ethHdrLen:]
	ihl := int(ip[0]&0x0f) * 4
	if ihl < ipHdrLen || len(ip) < ihl+udpHdrLen {
		return nil, nil, false
	}
	if ip[9] != ipProtoUDP {
		return nil, nil, false
	}
	udp := ip[ihl:]
	if binary.BigEndian.Uint16(udp[2:4]) != dhcpServerPort {
		return nil, nil, false
	}
	ulen := int(binary.BigEndian.Uint16(udp[4:6]))
	if ulen < udpHdrLen || len(udp) < ulen {
		return nil, nil, false
	}
	p, err := DecodePacket(udp[udpHdrLen:ulen])
	if err != nil || p.Op != opBootRequest {
		return nil, nil, false
	}
	return p, src, true
}

// buildFrame wraps a DHCP payload in ethernet+IPv4+UDP, addressed from the
// gateway to the client. When broadcast is set (client has no IP yet) the
// destination MAC/IP are the broadcast addresses, matching how a client with a
// zero ciaddr expects the reply.
func (d *dhcpServer) buildFrame(clientMAC net.HardwareAddr, yiaddr net.IP, payload []byte, broadcast bool) []byte {
	dstMAC := clientMAC
	dstIP := yiaddr.To4()
	if broadcast || dstIP == nil || dstIP.Equal(net.IPv4zero.To4()) {
		dstMAC = net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
		dstIP = net.IPv4bcast.To4()
	}

	total := ethHdrLen + ipHdrLen + udpHdrLen + len(payload)
	frame := make([]byte, total)

	// Ethernet header.
	copy(frame[0:6], dstMAC)
	copy(frame[6:12], d.serverMA)
	binary.BigEndian.PutUint16(frame[12:14], etherTypeIPv4)

	// IPv4 header.
	ip := frame[ethHdrLen:]
	ip[0] = 0x45 // version 4, IHL 5
	binary.BigEndian.PutUint16(ip[2:4], uint16(ipHdrLen+udpHdrLen+len(payload)))
	ip[8] = 64 // TTL
	ip[9] = ipProtoUDP
	copy(ip[12:16], d.serverIP.To4())
	copy(ip[16:20], dstIP)
	binary.BigEndian.PutUint16(ip[10:12], ipChecksum(ip[:ipHdrLen]))

	// UDP header (checksum 0 = not computed, legal for IPv4).
	udp := ip[ipHdrLen:]
	binary.BigEndian.PutUint16(udp[0:2], dhcpServerPort)
	binary.BigEndian.PutUint16(udp[2:4], dhcpClientPort)
	binary.BigEndian.PutUint16(udp[4:6], uint16(udpHdrLen+len(payload)))
	copy(udp[udpHdrLen:], payload)

	return frame
}

// ipChecksum computes the standard IPv4 header checksum over a header whose
// checksum field is zero.
func ipChecksum(hdr []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(hdr); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(hdr[i : i+2]))
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// gatewayMAC derives a stable locally-administered unicast MAC for the tap's
// gateway side from its IPv4 address (02:00 prefix + the 4 address octets), so
// the DHCP server has a consistent source MAC without querying the device.
func gatewayMAC(ip net.IP) net.HardwareAddr {
	v := ip.To4()
	if v == nil {
		v = net.IPv4zero.To4()
	}
	return net.HardwareAddr{0x02, 0x00, v[0], v[1], v[2], v[3]}
}
