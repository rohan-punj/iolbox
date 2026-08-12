package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
)

const dhcpCookie uint32 = 0x63825363

type DHCPPacket struct {
	Op, HType, HLen  uint8
	Flags            uint16
	XID              uint32
	ClientHW         [16]byte
	YourIP, ServerIP net.IP
	Options          map[byte][]byte
}

func NewDHCPPacket() DHCPPacket {
	return DHCPPacket{Op: 1, HType: 1, HLen: 6, Options: map[byte][]byte{}}
}

func (p DHCPPacket) Encode() []byte {
	b := make([]byte, 240)
	b[0], b[1], b[2] = p.Op, p.HType, p.HLen
	b[4], b[5], b[6], b[7] = byte(p.XID>>24), byte(p.XID>>16), byte(p.XID>>8), byte(p.XID)
	b[10], b[11] = byte(p.Flags>>8), byte(p.Flags)
	if ip := p.YourIP.To4(); ip != nil {
		copy(b[16:20], ip)
	}
	if ip := p.ServerIP.To4(); ip != nil {
		copy(b[20:24], ip)
	}
	copy(b[28:44], p.ClientHW[:])
	binary.BigEndian.PutUint32(b[236:240], dhcpCookie)
	keys := make([]int, 0, len(p.Options))
	for k := range p.Options {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)
	for _, key := range keys {
		if key == 0 || key == 255 {
			continue
		}
		value := p.Options[byte(key)]
		if len(value) > 255 {
			continue
		}
		b = append(b, byte(key), byte(len(value)))
		b = append(b, value...)
	}
	return append(b, 255)
}

func DecodeDHCP(b []byte) (DHCPPacket, error) {
	if len(b) < 240 || binary.BigEndian.Uint32(b[236:240]) != dhcpCookie {
		return DHCPPacket{}, fmt.Errorf("invalid DHCP packet")
	}
	p := NewDHCPPacket()
	p.Op, p.HType, p.HLen = b[0], b[1], b[2]
	p.XID = binary.BigEndian.Uint32(b[4:8])
	p.Flags = binary.BigEndian.Uint16(b[10:12])
	copy(p.ClientHW[:], b[28:44])
	p.YourIP = net.IPv4(b[16], b[17], b[18], b[19])
	p.ServerIP = net.IPv4(b[20], b[21], b[22], b[23])
	for i := 240; i < len(b); {
		code := b[i]
		i++
		if code == 0 {
			continue
		}
		if code == 255 {
			break
		}
		if i >= len(b) {
			return DHCPPacket{}, fmt.Errorf("truncated DHCP option")
		}
		n := int(b[i])
		i++
		if i+n > len(b) {
			return DHCPPacket{}, fmt.Errorf("truncated DHCP option %d", code)
		}
		p.Options[code] = append([]byte(nil), b[i:i+n]...)
		i += n
	}
	return p, nil
}

func BuildDHCPDiscover(xid uint32, mac net.HardwareAddr) DHCPPacket {
	p := NewDHCPPacket()
	p.XID = xid
	p.Flags = 0x8000
	copy(p.ClientHW[:], mac)
	p.Options[53] = []byte{1}
	p.Options[55] = []byte{1, 3, 6, 51, 54, 66, 150}
	p.Options[61] = append([]byte{1}, mac...)
	return p
}

func BuildDHCPRequest(xid uint32, mac net.HardwareAddr, requested, server net.IP) DHCPPacket {
	p := BuildDHCPDiscover(xid, mac)
	p.Options[53] = []byte{3}
	p.Options[50] = requested.To4()
	p.Options[54] = server.To4()
	return p
}

func dhcpString(options map[byte][]byte, code byte) string {
	return strings.TrimRight(string(options[code]), "\x00")
}

func leaseFromOffer(p DHCPPacket) Lease {
	lease := Lease{ServerID: p.ServerIP.String(), TFTPName: dhcpString(p.Options, 66), Options: cloneDHCPOptions(p.Options)}
	if v := p.Options[1]; len(v) == 4 {
		lease.SubnetMask = net.IP(v).String()
	}
	if v := p.Options[54]; len(v) == 4 {
		lease.ServerID = net.IP(v).String()
	}
	if v := p.Options[3]; len(v) >= 4 {
		lease.Router = net.IP(v[:4]).String()
	}
	if v := p.Options[6]; len(v)%4 == 0 {
		for i := 0; i < len(v); i += 4 {
			lease.DNS = append(lease.DNS, net.IP(v[i:i+4]).String())
		}
	}
	if v := p.Options[51]; len(v) == 4 {
		lease.LeaseSeconds = binary.BigEndian.Uint32(v)
	}
	if v := p.Options[150]; len(v)%4 == 0 {
		for i := 0; i < len(v); i += 4 {
			lease.TFTPServers = append(lease.TFTPServers, net.IP(v[i:i+4]).String())
		}
	}
	return lease
}

func prefixFromSubnetMask(mask string) int {
	ip := net.ParseIP(mask).To4()
	if ip == nil {
		return 24
	}
	ones, bits := net.IPMask(ip).Size()
	if bits != 32 || ones < 1 || ones > 32 {
		return 24
	}
	return ones
}

func cloneDHCPOptions(in map[byte][]byte) map[byte][]byte {
	out := make(map[byte][]byte, len(in))
	for code, value := range in {
		out[code] = append([]byte(nil), value...)
	}
	return out
}

func runDHCP(app *App) string {
	iface, err := net.InterfaceByName("eth1")
	if err != nil {
		return "% DHCP: eth1 unavailable"
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 68})
	if err != nil {
		return "% DHCP: " + err.Error()
	}
	defer conn.Close()
	xid := uint32(time.Now().UnixNano())
	discover := BuildDHCPDiscover(xid, iface.HardwareAddr)
	dest := &net.UDPAddr{IP: net.IPv4bcast, Port: 67}
	if _, err := conn.WriteToUDP(discover.Encode(), dest); err != nil {
		return "% DHCP: " + err.Error()
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 1500)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return "% DHCP: no offer"
	}
	offer, err := DecodeDHCP(buf[:n])
	if err != nil || offer.XID != xid {
		return "% DHCP: invalid offer"
	}
	server := net.IP(offer.Options[54])
	request := BuildDHCPRequest(xid, iface.HardwareAddr, offer.YourIP, server)
	if _, err := conn.WriteToUDP(request.Encode(), dest); err != nil {
		return "% DHCP: " + err.Error()
	}
	lease := leaseFromOffer(offer)
	app.state.SetLease(lease)
	prefix := prefixFromSubnetMask(lease.SubnetMask)
	_ = app.state.SetAddress(offer.YourIP.String(), prefix, lease.Router)
	if lease.Router != "" {
		_ = runIP("route", "replace", "default", "via", lease.Router, "dev", "eth1")
	}
	return fmt.Sprintf("DHCP lease acquired: %s/%d", offer.YourIP, prefix)
}
