package attackcommon

import (
	"encoding/binary"
	"fmt"
	"net"
)

const (
	ethernetHeaderLen = 14
	ipv4HeaderLen     = 20
	udpHeaderLen      = 8
	bootpLen          = 236
	dhcpMagicLen      = 4
)

var dhcpMagicCookie = [dhcpMagicLen]byte{0x63, 0x82, 0x53, 0x63}

// internetChecksum returns the RFC 1071 ones-complement checksum. An odd
// final byte is treated as the high byte of a zero-padded 16-bit word.
func internetChecksum(data []byte) uint16 {
	var sum uint32
	for len(data) >= 2 {
		sum += uint32(binary.BigEndian.Uint16(data[:2]))
		data = data[2:]
	}
	if len(data) == 1 {
		sum += uint32(data[0]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func ipv4Bytes(ip net.IP) [4]byte {
	var out [4]byte
	if value := ip.To4(); value != nil {
		copy(out[:], value)
	}
	return out
}

func ipv6Bytes(ip net.IP) [16]byte {
	var out [16]byte
	if value := ip.To16(); value != nil {
		copy(out[:], value)
	}
	return out
}

// BuildIPv4 builds an IPv4 packet with no options. The fixed header values
// match Scapy's bare IP defaults: ID 1, TOS 0, flags 0, fragment offset 0,
// and TTL 64.
func BuildIPv4(src, dst net.IP, proto byte, payload []byte) []byte {
	packet := make([]byte, ipv4HeaderLen+len(payload))
	packet[0] = 0x45
	packet[1] = 0
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	binary.BigEndian.PutUint16(packet[4:6], 1)
	binary.BigEndian.PutUint16(packet[6:8], 0)
	packet[8] = 64
	packet[9] = proto
	srcBytes := ipv4Bytes(src)
	dstBytes := ipv4Bytes(dst)
	copy(packet[12:16], srcBytes[:])
	copy(packet[16:20], dstBytes[:])
	binary.BigEndian.PutUint16(packet[10:12], internetChecksum(packet[:ipv4HeaderLen]))
	copy(packet[ipv4HeaderLen:], payload)
	return packet
}

// BuildUDP builds a UDP segment and computes its checksum. IPv4 uses the
// RFC 768 IPv4 pseudo-header; IPv6 uses the RFC 2460 section 8.1 pseudo-header.
func BuildUDP(src, dst net.IP, sport, dport uint16, payload []byte, ipv6 bool) []byte {
	segment := make([]byte, udpHeaderLen+len(payload))
	binary.BigEndian.PutUint16(segment[0:2], sport)
	binary.BigEndian.PutUint16(segment[2:4], dport)
	binary.BigEndian.PutUint16(segment[4:6], uint16(len(segment)))
	copy(segment[udpHeaderLen:], payload)

	var pseudo []byte
	if ipv6 {
		pseudo = make([]byte, 40)
		srcBytes := ipv6Bytes(src)
		dstBytes := ipv6Bytes(dst)
		copy(pseudo[0:16], srcBytes[:])
		copy(pseudo[16:32], dstBytes[:])
		binary.BigEndian.PutUint32(pseudo[32:36], uint32(len(segment)))
		pseudo[39] = 17
	} else {
		pseudo = make([]byte, 12)
		srcBytes := ipv4Bytes(src)
		dstBytes := ipv4Bytes(dst)
		copy(pseudo[0:4], srcBytes[:])
		copy(pseudo[4:8], dstBytes[:])
		pseudo[9] = 17
		binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(segment)))
	}

	checksum := internetChecksum(append(pseudo, segment...))
	if checksum == 0 {
		// A zero UDP checksum means "not provided" for IPv4 and is invalid for
		// IPv6, so retain the computed checksum without leaving this field zero.
		checksum = 0xffff
	}
	binary.BigEndian.PutUint16(segment[6:8], checksum)
	return segment
}

// BuildICMPEcho builds an ICMPv4 Echo Request with the supplied identifier,
// sequence number, and payload.
func BuildICMPEcho(id, seq uint16, payload []byte) []byte {
	packet := make([]byte, 8+len(payload))
	packet[0] = 8
	packet[1] = 0
	binary.BigEndian.PutUint16(packet[4:6], id)
	binary.BigEndian.PutUint16(packet[6:8], seq)
	copy(packet[8:], payload)
	binary.BigEndian.PutUint16(packet[2:4], internetChecksum(packet))
	return packet
}

// InterfaceIPv4 returns the first configured IPv4 address on iface, or
// 0.0.0.0 when the interface cannot be resolved or has no IPv4 address.
func InterfaceIPv4(iface string) net.IP {
	device, err := net.InterfaceByName(iface)
	if err != nil {
		return net.IPv4zero
	}
	addresses, err := device.Addrs()
	if err != nil {
		return net.IPv4zero
	}
	for _, address := range addresses {
		var ip net.IP
		switch value := address.(type) {
		case *net.IPNet:
			ip = value.IP
		case *net.IPAddr:
			ip = value.IP
		default:
			ip, _, _ = net.ParseCIDR(address.String())
		}
		if value := ip.To4(); value != nil {
			return append(net.IP(nil), value...)
		}
	}
	return net.IPv4zero
}

func buildEtherIPv4UDP(srcMAC net.HardwareAddr, srcIP, dstIP net.IP, sport, dport uint16, payload []byte) []byte {
	packet := make([]byte, ethernetHeaderLen)
	copy(packet[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	copy(packet[6:12], srcMAC)
	binary.BigEndian.PutUint16(packet[12:14], 0x0800)
	packet = append(packet, BuildIPv4(srcIP, dstIP, 17, BuildUDP(srcIP, dstIP, sport, dport, payload, false))...)
	return packet
}

func buildBOOTP(op byte, xid uint32, chaddr [6]byte, yiaddr, siaddr net.IP) []byte {
	bootp := make([]byte, bootpLen)
	bootp[0] = op
	bootp[1] = 1
	bootp[2] = 6
	binary.BigEndian.PutUint32(bootp[4:8], xid)
	if op == 1 {
		binary.BigEndian.PutUint16(bootp[10:12], 0x8000)
	}
	yiaddrBytes := ipv4Bytes(yiaddr)
	siaddrBytes := ipv4Bytes(siaddr)
	copy(bootp[16:20], yiaddrBytes[:])
	copy(bootp[20:24], siaddrBytes[:])
	copy(bootp[28:34], chaddr[:])
	return bootp
}

// BuildDHCPDiscover builds the complete broadcast Ethernet/IP/UDP/DHCPDISCOVER
// frame with Scapy-compatible BOOTP defaults and option ordering.
func BuildDHCPDiscover(xid uint32, chaddr [6]byte, srcMAC net.HardwareAddr) []byte {
	bootp := buildBOOTP(1, xid, chaddr, nil, nil)
	payload := append(bootp, dhcpMagicCookie[:]...)
	payload = append(payload, 53, 1, 1, 255)
	return buildEtherIPv4UDP(srcMAC, net.IPv4zero, net.IPv4bcast, 68, 67, payload)
}

// BuildDHCPOffer builds the complete broadcast Ethernet/IP/UDP/DHCPOFFER
// frame. DHCP options are serialized in the exact order used by the Python
// rogue-server script.
func BuildDHCPOffer(xid uint32, chaddr [6]byte, offerIP, gateway, dns net.IP, leaseTime uint32, srcMAC net.HardwareAddr) []byte {
	bootp := buildBOOTP(2, xid, chaddr, offerIP, gateway)
	payload := append(bootp, dhcpMagicCookie[:]...)
	payload = append(payload, 53, 1, 2)
	payload = append(payload, 54, 4)
	serverBytes := ipv4Bytes(gateway)
	payload = append(payload, serverBytes[:]...)
	payload = append(payload, 51, 4)
	leaseBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(leaseBytes, leaseTime)
	payload = append(payload, leaseBytes...)
	payload = append(payload, 3, 4)
	payload = append(payload, serverBytes[:]...)
	payload = append(payload, 6, 4)
	dnsBytes := ipv4Bytes(dns)
	payload = append(payload, dnsBytes[:]...)
	payload = append(payload, 1, 4, 255, 255, 255, 0, 255)
	return buildEtherIPv4UDP(srcMAC, gateway, net.IPv4bcast, 67, 68, payload)
}

// ParseEthernet parses an Ethernet frame and peels any stacked 802.1Q tags.
// The returned payload starts immediately after the final EtherType.
func ParseEthernet(frame []byte) (dstMAC, srcMAC net.HardwareAddr, ethertype uint16, vlanTags []uint16, payload []byte) {
	if len(frame) < ethernetHeaderLen {
		return nil, nil, 0, nil, nil
	}
	dstMAC = append(net.HardwareAddr(nil), frame[0:6]...)
	srcMAC = append(net.HardwareAddr(nil), frame[6:12]...)
	offset := ethernetHeaderLen
	ethertype = binary.BigEndian.Uint16(frame[12:14])
	for ethertype == 0x8100 {
		if len(frame) < offset+4 {
			return dstMAC, srcMAC, 0, vlanTags, nil
		}
		tci := binary.BigEndian.Uint16(frame[offset : offset+2])
		vlanTags = append(vlanTags, tci&0x0fff)
		ethertype = binary.BigEndian.Uint16(frame[offset+2 : offset+4])
		offset += 4
	}
	if offset > len(frame) {
		return dstMAC, srcMAC, 0, vlanTags, nil
	}
	return dstMAC, srcMAC, ethertype, vlanTags, frame[offset:]
}

// ParseARP parses a standard Ethernet/IPv4 ARP payload.
func ParseARP(payload []byte) (op uint16, srcMAC net.HardwareAddr, srcIP net.IP, dstIP net.IP, ok bool) {
	if len(payload) < 28 || binary.BigEndian.Uint16(payload[0:2]) != 1 ||
		binary.BigEndian.Uint16(payload[2:4]) != 0x0800 || payload[4] != 6 || payload[5] != 4 {
		return 0, nil, nil, nil, false
	}
	op = binary.BigEndian.Uint16(payload[6:8])
	srcMAC = append(net.HardwareAddr(nil), payload[8:14]...)
	srcIP = net.IPv4(payload[14], payload[15], payload[16], payload[17])
	dstIP = net.IPv4(payload[24], payload[25], payload[26], payload[27])
	return op, srcMAC, srcIP, dstIP, true
}

// ParseIPv4UDP parses an IPv4 packet containing a complete UDP datagram.
func ParseIPv4UDP(payload []byte) (srcIP, dstIP net.IP, sport, dport uint16, udpPayload []byte, ok bool) {
	if len(payload) < ipv4HeaderLen || payload[0]>>4 != 4 || payload[9] != 17 {
		return nil, nil, 0, 0, nil, false
	}
	headerLen := int(payload[0]&0x0f) * 4
	if headerLen < ipv4HeaderLen || len(payload) < headerLen+udpHeaderLen {
		return nil, nil, 0, 0, nil, false
	}
	totalLen := int(binary.BigEndian.Uint16(payload[2:4]))
	if totalLen < headerLen+udpHeaderLen || totalLen > len(payload) {
		return nil, nil, 0, 0, nil, false
	}
	udp := payload[headerLen:totalLen]
	udpLen := int(binary.BigEndian.Uint16(udp[4:6]))
	if udpLen < udpHeaderLen || udpLen > len(udp) {
		return nil, nil, 0, 0, nil, false
	}
	srcIP = net.IPv4(payload[12], payload[13], payload[14], payload[15])
	dstIP = net.IPv4(payload[16], payload[17], payload[18], payload[19])
	sport = binary.BigEndian.Uint16(udp[0:2])
	dport = binary.BigEndian.Uint16(udp[2:4])
	udpPayload = udp[udpHeaderLen:udpLen]
	return srcIP, dstIP, sport, dport, udpPayload, true
}

// ParseBOOTP parses the fixed BOOTP header, DHCP magic cookie, and DHCP
// message-type option from a UDP payload.
func ParseBOOTP(udpPayload []byte) (xid uint32, chaddr [6]byte, yiaddr net.IP, msgType byte, ok bool) {
	if len(udpPayload) < bootpLen+dhcpMagicLen ||
		(udpPayload[236] != dhcpMagicCookie[0] || udpPayload[237] != dhcpMagicCookie[1] ||
			udpPayload[238] != dhcpMagicCookie[2] || udpPayload[239] != dhcpMagicCookie[3]) {
		return 0, chaddr, nil, 0, false
	}
	xid = binary.BigEndian.Uint32(udpPayload[4:8])
	copy(chaddr[:], udpPayload[28:34])
	yiaddr = net.IPv4(udpPayload[16], udpPayload[17], udpPayload[18], udpPayload[19])
	options := udpPayload[bootpLen+dhcpMagicLen:]
	for len(options) > 0 {
		switch options[0] {
		case 0:
			options = options[1:]
		case 255:
			return xid, chaddr, yiaddr, msgType, true
		default:
			if len(options) < 2 || int(options[1])+2 > len(options) {
				return 0, chaddr, nil, 0, false
			}
			if options[0] == 53 && options[1] >= 1 {
				msgType = options[2]
			}
			options = options[2+int(options[1]):]
		}
	}
	return xid, chaddr, yiaddr, msgType, true
}

// ParseDHCP6 parses a DHCPv6 message and returns option 1 exactly as it was
// received, including the option code and length fields.
func ParseDHCP6(udpPayload []byte) (msgType byte, trid [3]byte, clientIDOpt []byte, ok bool) {
	if len(udpPayload) < 4 {
		return 0, trid, nil, false
	}
	msgType = udpPayload[0]
	copy(trid[:], udpPayload[1:4])
	options := udpPayload[4:]
	for len(options) > 0 {
		if len(options) < 4 {
			return 0, trid, nil, false
		}
		optionLen := int(binary.BigEndian.Uint16(options[2:4]))
		if optionLen > len(options)-4 {
			return 0, trid, nil, false
		}
		if binary.BigEndian.Uint16(options[0:2]) == 1 && clientIDOpt == nil {
			clientIDOpt = append([]byte(nil), options[:4+optionLen]...)
		}
		options = options[4+optionLen:]
	}
	return msgType, trid, clientIDOpt, true
}

// HostsInCIDR expands an IPv4 CIDR into its inclusive network-to-broadcast
// address range, matching Scapy's Net() behavior. Prefixes shorter than /22
// are rejected as a safety rail against accidental large allocations.
func HostsInCIDR(cidr string) ([]net.IP, error) {
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("parse CIDR %q: %w", cidr, err)
	}
	ip = ip.To4()
	if ip == nil || network == nil {
		return nil, fmt.Errorf("CIDR %q is not an IPv4 network", cidr)
	}
	ones, bits := network.Mask.Size()
	if bits != 32 {
		return nil, fmt.Errorf("CIDR %q is not an IPv4 network", cidr)
	}
	if ones < 22 {
		return nil, fmt.Errorf("CIDR %q is too broad; prefix must be /22 or longer", cidr)
	}
	count := uint64(1) << uint(bits-ones)
	base := binary.BigEndian.Uint32(network.IP.To4())
	hosts := make([]net.IP, 0, count)
	for offset := uint64(0); offset < count; offset++ {
		value := base + uint32(offset)
		address := make(net.IP, net.IPv4len)
		binary.BigEndian.PutUint32(address, value)
		hosts = append(hosts, address)
	}
	return hosts, nil
}
