package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	dhcpBootRequest     = 1
	dhcpBootReply       = 2
	dhcpOptSubnetMask   = 1
	dhcpOptRouter       = 3
	dhcpOptDNS          = 6
	dhcpOptHostname     = 12
	dhcpOptRequestedIP  = 50
	dhcpOptLeaseTime    = 51
	dhcpOptMessageType  = 53
	dhcpOptServerID     = 54
	dhcpOptT1           = 58
	dhcpOptT2           = 59
	dhcpOptClientID     = 61
	dhcpOptVendorClass  = 60
	dhcpOptParamRequest = 55
	dhcpOptRelayInfo    = 82
	dhcpOptNTP          = 42
	dhcpOptTFTPName     = 66
	dhcpOptTFTPAddress  = 150
)

type DHCPOption struct {
	Code  byte
	Value []byte
}

type DHCPRelayInfo struct{ CircuitID, RemoteID string }

type DHCPPacket struct {
	Op, HType, HLen, Hops          byte
	XID                            uint32
	Secs                           uint16
	Flags                          uint16
	CIAddr, YIAddr, SIAddr, GIAddr net.IP
	CHAddr                         [16]byte
	SName                          [64]byte
	File                           [128]byte
	Options                        []DHCPOption
}

func (p DHCPPacket) Option(code byte) []byte {
	for _, opt := range p.Options {
		if opt.Code == code {
			return append([]byte(nil), opt.Value...)
		}
	}
	return nil
}

func DecodeDHCP(b []byte) (DHCPPacket, error) {
	var p DHCPPacket
	if len(b) < 240 {
		return p, errors.New("DHCP packet shorter than 240-byte header")
	}
	p.Op, p.HType, p.HLen, p.Hops = b[0], b[1], b[2], b[3]
	p.XID, p.Secs, p.Flags = binary.BigEndian.Uint32(b[4:8]), binary.BigEndian.Uint16(b[8:10]), binary.BigEndian.Uint16(b[10:12])
	p.CIAddr, p.YIAddr, p.SIAddr, p.GIAddr = cloneIP(b[12:16]), cloneIP(b[16:20]), cloneIP(b[20:24]), cloneIP(b[24:28])
	copy(p.CHAddr[:], b[28:44])
	copy(p.SName[:], b[44:108])
	copy(p.File[:], b[108:236])
	if binary.BigEndian.Uint32(b[236:240]) != 0x63825363 {
		return p, errors.New("DHCP magic cookie missing")
	}
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
			return p, errors.New("DHCP option length missing")
		}
		n := int(b[i])
		i++
		if i+n > len(b) {
			return p, fmt.Errorf("DHCP option %d exceeds packet", code)
		}
		p.Options = append(p.Options, DHCPOption{Code: code, Value: append([]byte(nil), b[i:i+n]...)})
		i += n
	}
	return p, nil
}

func EncodeDHCP(p DHCPPacket) []byte {
	b := make([]byte, 240)
	b[0], b[1], b[2], b[3] = p.Op, p.HType, p.HLen, p.Hops
	binary.BigEndian.PutUint32(b[4:8], p.XID)
	binary.BigEndian.PutUint16(b[8:10], p.Secs)
	binary.BigEndian.PutUint16(b[10:12], p.Flags)
	copy(b[12:16], ipv4Bytes(p.CIAddr))
	copy(b[16:20], ipv4Bytes(p.YIAddr))
	copy(b[20:24], ipv4Bytes(p.SIAddr))
	copy(b[24:28], ipv4Bytes(p.GIAddr))
	copy(b[28:44], p.CHAddr[:])
	copy(b[44:108], p.SName[:])
	copy(b[108:236], p.File[:])
	binary.BigEndian.PutUint32(b[236:240], 0x63825363)
	for _, opt := range p.Options {
		if opt.Code == 0 || opt.Code == 255 || len(opt.Value) > 255 {
			continue
		}
		b = append(b, opt.Code, byte(len(opt.Value)))
		b = append(b, opt.Value...)
	}
	b = append(b, 255)
	return b
}

func ipv4Bytes(ip net.IP) []byte {
	if ip4 := ip.To4(); ip4 != nil {
		return ip4
	}
	return []byte{0, 0, 0, 0}
}

func cloneIP(ip []byte) net.IP { return net.IPv4(ip[0], ip[1], ip[2], ip[3]).To4() }

func appendIPsOpt(code byte, ips []string) DHCPOption {
	value := make([]byte, 0, 4*len(ips))
	for _, text := range ips {
		if ip := net.ParseIP(strings.TrimSpace(text)).To4(); ip != nil {
			value = append(value, ip...)
		}
	}
	return DHCPOption{Code: code, Value: value}
}

func dhcpMessageType(p DHCPPacket) byte {
	v := p.Option(dhcpOptMessageType)
	if len(v) == 1 {
		return v[0]
	}
	return 0
}

func dhcpMAC(p DHCPPacket) string {
	n := int(p.HLen)
	if n <= 0 || n > len(p.CHAddr) {
		n = 6
	}
	return strings.ToLower(net.HardwareAddr(p.CHAddr[:n]).String())
}

func parseRelayInfo(v []byte) DHCPRelayInfo {
	var info DHCPRelayInfo
	for i := 0; i+2 <= len(v); {
		code, n := v[i], int(v[i+1])
		i += 2
		if i+n > len(v) {
			break
		}
		value := string(v[i : i+n])
		if code == 1 {
			info.CircuitID = value
		}
		if code == 2 {
			info.RemoteID = value
		}
		i += n
	}
	return info
}

type DHCPOptionView struct {
	Code  byte
	Value string
}

type Lease struct {
	MAC, IP, Hostname   string
	BoundAt, Expires    time.Time
	Requested           []byte
	Received            []DHCPOptionView
	Sent                []DHCPOptionView
	GIAddr              string
	CircuitID, RemoteID string
}

func leaseOptions(p DHCPPacket) []DHCPOptionView {
	out := make([]DHCPOptionView, 0, len(p.Options))
	for _, opt := range p.Options {
		out = append(out, DHCPOptionView{Code: opt.Code, Value: dhcpOptionDisplay(opt)})
	}
	return out
}

type DHCPServer struct {
	store  *Store
	mu     sync.RWMutex
	leases map[string]Lease
}

func NewDHCPServer(store *Store) *DHCPServer {
	return &DHCPServer{store: store, leases: map[string]Lease{}}
}

func (s *DHCPServer) Leases() []Lease {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Lease, 0, len(s.leases))
	for _, lease := range s.leases {
		out = append(out, lease)
	}
	return out
}

func (s *DHCPServer) handlePacket(conn *net.UDPConn, data []byte, addr *net.UDPAddr) {
	p, err := DecodeDHCP(data)
	if err != nil || dhcpMessageType(p) == 0 {
		return
	}
	reply, lease, send, err := s.Handle(p, time.Now())
	if err != nil || !send {
		return
	}
	dest := dhcpReplyDestination(p, lease)
	_, _ = conn.WriteToUDP(EncodeDHCP(reply), dest)
}

func dhcpReplyDestination(p DHCPPacket, lease Lease) *net.UDPAddr {
	if p.GIAddr.To4() != nil && !p.GIAddr.Equal(net.IPv4zero) {
		return &net.UDPAddr{IP: p.GIAddr.To4(), Port: 67}
	}
	if p.CIAddr.To4() != nil && !p.CIAddr.Equal(net.IPv4zero) {
		return &net.UDPAddr{IP: p.CIAddr.To4(), Port: 68}
	}
	if p.Flags&0x8000 == 0 && lease.IP != "" {
		return &net.UDPAddr{IP: net.ParseIP(lease.IP).To4(), Port: 68}
	}
	return &net.UDPAddr{IP: net.IPv4(255, 255, 255, 255), Port: 68}
}

func (s *DHCPServer) Handle(p DHCPPacket, now time.Time) (DHCPPacket, Lease, bool, error) {
	var zero Lease
	mt := dhcpMessageType(p)
	if mt != 1 && mt != 3 {
		return DHCPPacket{}, zero, false, nil
	}
	cfg := s.store.Snapshot().DHCP
	serverIP := localIPv4(cfg.ServerIP)
	if serverIP == nil {
		serverIP = net.IPv4zero
	}
	if selected := p.Option(dhcpOptServerID); len(selected) == 4 && !net.IP(selected).Equal(serverIP) && !serverIP.Equal(net.IPv4zero) {
		return DHCPPacket{}, zero, false, nil
	}
	pool, network, err := selectDHCPPool(cfg, p)
	if err != nil {
		return DHCPPacket{}, zero, false, err
	}
	mac := dhcpMAC(p)
	s.mu.Lock()
	old, exists := s.leases[mac]
	ip := ""
	if cfg.Reservations != nil {
		ip = cfg.Reservations[mac]
	}
	if ip == "" && exists && old.Expires.After(now) {
		ip = old.IP
	}
	if requested := p.Option(dhcpOptRequestedIP); len(requested) == 4 && ip == "" {
		ip = net.IP(requested).String()
	}
	if ip == "" {
		ip = nextPoolIP(pool, network, s.leases)
	}
	if ip == "" || !network.Contains(net.ParseIP(ip).To4()) {
		s.mu.Unlock()
		return DHCPPacket{}, zero, false, errors.New("DHCP pool has no available address")
	}
	leaseSeconds := cfg.LeaseSeconds
	if leaseSeconds == 0 {
		leaseSeconds = 3600
	}
	relay := parseRelayInfo(p.Option(dhcpOptRelayInfo))
	lease := Lease{MAC: mac, IP: ip, Hostname: string(p.Option(dhcpOptHostname)), BoundAt: now, Expires: now.Add(time.Duration(leaseSeconds) * time.Second), Requested: append([]byte(nil), p.Option(dhcpOptParamRequest)...), GIAddr: p.GIAddr.String(), CircuitID: relay.CircuitID, RemoteID: relay.RemoteID}
	lease.Received = leaseOptions(p)
	options := []DHCPOption{{Code: dhcpOptMessageType, Value: []byte{2}}}
	if mt == 3 {
		options[0].Value[0] = 5
	}
	if mask := network.Mask; len(mask) == 4 {
		options = append(options, DHCPOption{Code: dhcpOptSubnetMask, Value: append([]byte(nil), mask...)})
	}
	if pool.Router != "" {
		options = append(options, DHCPOption{Code: dhcpOptRouter, Value: ipv4Bytes(net.ParseIP(pool.Router))})
	}
	if opt := appendIPsOpt(dhcpOptDNS, cfg.DNSServers); len(opt.Value) > 0 {
		options = append(options, opt)
	}
	if opt := appendIPsOpt(dhcpOptNTP, cfg.NTPServers); len(opt.Value) > 0 {
		options = append(options, opt)
	}
	leaseValue := make([]byte, 4)
	binary.BigEndian.PutUint32(leaseValue, leaseSeconds)
	options = append(options, DHCPOption{Code: dhcpOptLeaseTime, Value: leaseValue})
	t1, t2 := make([]byte, 4), make([]byte, 4)
	binary.BigEndian.PutUint32(t1, leaseSeconds/2)
	binary.BigEndian.PutUint32(t2, leaseSeconds*7/8)
	options = append(options, DHCPOption{Code: dhcpOptT1, Value: t1}, DHCPOption{Code: dhcpOptT2, Value: t2})
	if !serverIP.Equal(net.IPv4zero) {
		options = append(options, DHCPOption{Code: dhcpOptServerID, Value: append([]byte(nil), serverIP...)})
	}
	if cfg.TFTPName != "" {
		options = append(options, DHCPOption{Code: dhcpOptTFTPName, Value: []byte(cfg.TFTPName)})
	}
	if opt := appendIPsOpt(dhcpOptTFTPAddress, cfg.TFTPAddresses); len(opt.Value) > 0 {
		options = append(options, opt)
	}
	for _, opt := range options {
		lease.Sent = append(lease.Sent, DHCPOptionView{Code: opt.Code, Value: dhcpOptionDisplay(opt)})
	}
	s.leases[mac] = lease
	s.mu.Unlock()
	reply := DHCPPacket{Op: dhcpBootReply, HType: p.HType, HLen: p.HLen, Hops: p.Hops, XID: p.XID, Secs: p.Secs, Flags: p.Flags, YIAddr: net.ParseIP(ip), SIAddr: serverIP, GIAddr: p.GIAddr, CHAddr: p.CHAddr, Options: options}
	return reply, lease, true, nil
}

func selectDHCPPool(cfg DHCPConfig, p DHCPPacket) (DHCPPool, *net.IPNet, error) {
	selector := p.GIAddr.To4()
	if selector == nil || selector.Equal(net.IPv4zero) {
		selector = p.CIAddr.To4()
	}
	if selector != nil && selector.Equal(net.IPv4zero) {
		selector = nil
	}
	for _, pool := range cfg.Pools {
		_, network, err := net.ParseCIDR(pool.Subnet)
		if err != nil {
			continue
		}
		if selector == nil || network.Contains(selector) {
			return pool, network, nil
		}
	}
	return DHCPPool{}, nil, errors.New("no DHCP pool matches relay address")
}

func nextPoolIP(pool DHCPPool, network *net.IPNet, leases map[string]Lease) string {
	start, end := net.ParseIP(pool.RangeStart).To4(), net.ParseIP(pool.RangeEnd).To4()
	if start == nil {
		start = network.IP.To4()
		start[3]++
	}
	if end == nil {
		end = net.IPv4(0, 0, 0, 0).To4()
		copy(end, network.IP.To4())
		end[3] = 254
	}
	for candidate := append(net.IP(nil), start...); ipLessEqual(candidate, end); incrementIP(candidate) {
		used := false
		for _, lease := range leases {
			if lease.IP == candidate.String() && lease.Expires.After(time.Now()) {
				used = true
				break
			}
		}
		if !used {
			return candidate.String()
		}
	}
	return ""
}

func incrementIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
}
func ipLessEqual(a, b net.IP) bool {
	for i := 0; i < 4; i++ {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return true
}

func dhcpOptionDisplay(opt DHCPOption) string {
	if opt.Code == dhcpOptTFTPName {
		return string(opt.Value)
	}
	if opt.Code == dhcpOptTFTPAddress || opt.Code == dhcpOptDNS || opt.Code == dhcpOptNTP || opt.Code == dhcpOptRouter || opt.Code == dhcpOptSubnetMask {
		var ips []string
		for i := 0; i+4 <= len(opt.Value); i += 4 {
			ips = append(ips, net.IP(opt.Value[i:i+4]).String())
		}
		return strings.Join(ips, ", ")
	}
	return fmt.Sprintf("%x", opt.Value)
}
