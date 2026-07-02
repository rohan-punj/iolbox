package extnet

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
)

// This file is a MINIMAL BOOTP/DHCP server (stdlib net only, no persistence).
// It handles exactly the DISCOVER->OFFER, REQUEST->ACK exchange a lab node's
// `ip dhcp` needs: fixed 1h lease, router+DNS = the nat gateway, addresses
// handed round-robin from the /24 pool. It is NOT a general DHCP server (no
// RENEW state machine, no conflict detection, no relay support) — the scope is
// "let an IOL/VPCS node on this tap get an address and a default route".
//
// Layout references: RFC 2131 (message format) + RFC 2132 (options). The packet
// codec is pure and unit-tested; dhcp_linux.go binds the UDP:67 server to the
// tap and calls Handle for each request.

const (
	// dhcpMinLen is the shortest valid BOOTP message: the fixed header up to and
	// including the 4-byte magic cookie. Anything shorter is not DHCP.
	dhcpMinLen = 240
	// dhcpMagic is the BOOTP magic cookie at offset 236 that marks the start of
	// the DHCP options field.
	dhcpMagic = 0x63825363

	opBootRequest = 1
	opBootReply   = 2

	htypeEthernet = 1
	hlenEthernet  = 6

	// flagBroadcast is the high bit of the flags field: when the client has no
	// IP yet it asks the server to broadcast the reply.
	flagBroadcast = 0x8000
)

// DHCP option codes (RFC 2132) this server reads/writes.
const (
	optSubnetMask    = 1
	optRouter        = 3
	optDNS           = 6
	optRequestedIP   = 50
	optLeaseTime     = 51
	optMessageType   = 53
	optServerID      = 54
	optParameterList = 55
	optRenewalT1     = 58
	optRebindingT2   = 59
	optEnd           = 255
	optPad           = 0
)

// DHCP message types (option 53 values).
const (
	msgDiscover = 1
	msgOffer    = 2
	msgRequest  = 3
	msgDecline  = 4
	msgACK      = 5
	msgNAK      = 6
	msgRelease  = 7
	msgInform   = 8
)

// leaseSeconds is the fixed lease time this server offers (1h). T1/T2 are the
// conventional 0.5/0.875 fractions.
const leaseSeconds = 3600

// Packet is a decoded BOOTP/DHCP message carrying only the fields this minimal
// server needs to form a reply.
type Packet struct {
	Op      byte             // opBootRequest / opBootReply
	XID     uint32           // transaction id, echoed into the reply
	Flags   uint16           // broadcast flag lives here
	CIAddr  net.IP           // client IP (set when already bound)
	YIAddr  net.IP           // "your" IP the server assigns
	GIAddr  net.IP           // relay agent IP (0 for a directly-attached client)
	CHAddr  net.HardwareAddr // client MAC (6 bytes for ethernet)
	MsgType byte             // option 53
	// ReqIP is the client's option-50 requested address (in a REQUEST).
	ReqIP net.IP
}

// DecodePacket parses a raw DHCP datagram (the UDP payload) into a Packet. It
// returns an error for a datagram too short to be DHCP or without the magic
// cookie; unknown options are ignored.
func DecodePacket(b []byte) (*Packet, error) {
	if len(b) < dhcpMinLen {
		return nil, fmt.Errorf("dhcp: datagram too short (%d < %d)", len(b), dhcpMinLen)
	}
	if binary.BigEndian.Uint32(b[236:240]) != dhcpMagic {
		return nil, fmt.Errorf("dhcp: bad magic cookie")
	}
	p := &Packet{
		Op:     b[0],
		XID:    binary.BigEndian.Uint32(b[4:8]),
		Flags:  binary.BigEndian.Uint16(b[10:12]),
		CIAddr: net.IP(append([]byte(nil), b[12:16]...)),
		YIAddr: net.IP(append([]byte(nil), b[16:20]...)),
		GIAddr: net.IP(append([]byte(nil), b[24:28]...)),
		CHAddr: net.HardwareAddr(append([]byte(nil), b[28:34]...)),
	}
	// Parse options past the magic cookie (TLV, terminated by 255).
	opts := b[240:]
	for i := 0; i < len(opts); {
		code := opts[i]
		if code == optEnd {
			break
		}
		if code == optPad {
			i++
			continue
		}
		if i+1 >= len(opts) {
			break
		}
		length := int(opts[i+1])
		if i+2+length > len(opts) {
			break
		}
		val := opts[i+2 : i+2+length]
		switch code {
		case optMessageType:
			if length == 1 {
				p.MsgType = val[0]
			}
		case optRequestedIP:
			if length == 4 {
				p.ReqIP = net.IP(append([]byte(nil), val...))
			}
		}
		i += 2 + length
	}
	return p, nil
}

// Encode serialises a Packet as a reply (BOOTREPLY) with the given message type
// and the server's addressing (server/gateway ip = serverIP, mask, router, dns
// all the nat gateway). chaddr is echoed from the request. The result is a full
// DHCP datagram ready to hand to a UDP write.
func (p *Packet) Encode(msgType byte, serverIP net.IP, mask net.IP) []byte {
	buf := make([]byte, dhcpMinLen)
	buf[0] = opBootReply
	buf[1] = htypeEthernet
	buf[2] = hlenEthernet
	buf[3] = 0 // hops
	binary.BigEndian.PutUint32(buf[4:8], p.XID)
	binary.BigEndian.PutUint16(buf[10:12], p.Flags) // echo broadcast flag
	copy(buf[16:20], p.YIAddr.To4())                // yiaddr
	copy(buf[20:24], serverIP.To4())                // siaddr (next server = us)
	copy(buf[24:28], p.GIAddr.To4())                // giaddr echoed
	if hw := p.CHAddr; len(hw) >= hlenEthernet {
		copy(buf[28:34], hw[:hlenEthernet])
	}
	binary.BigEndian.PutUint32(buf[236:240], dhcpMagic)

	opts := []byte{optMessageType, 1, msgType}
	opts = appendIPOpt(opts, optServerID, serverIP)
	opts = appendU32Opt(opts, optLeaseTime, leaseSeconds)
	opts = appendU32Opt(opts, optRenewalT1, leaseSeconds/2)
	opts = appendU32Opt(opts, optRebindingT2, leaseSeconds*7/8)
	opts = appendIPOpt(opts, optSubnetMask, mask)
	opts = appendIPOpt(opts, optRouter, serverIP)
	opts = appendIPOpt(opts, optDNS, serverIP)
	opts = append(opts, optEnd)

	return append(buf, opts...)
}

func appendIPOpt(opts []byte, code byte, ip net.IP) []byte {
	v := ip.To4()
	if v == nil {
		v = net.IPv4zero.To4()
	}
	return append(append(opts, code, 4), v...)
}

func appendU32Opt(opts []byte, code byte, v uint32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return append(append(opts, code, 4), b[:]...)
}

// Handle applies the minimal server policy to a decoded request and returns the
// reply message type (msgOffer / msgACK) plus the offered address, or ok=false
// when the request needs no reply (a message type we don't serve, e.g. RELEASE).
// The caller (dhcp_linux.go) has already assigned an address via the leaser and
// passes it in as offer; Handle only maps request-type -> reply-type so the
// policy is testable without sockets.
func (p *Packet) Handle(offer net.IP) (replyType byte, reply *Packet, ok bool) {
	switch p.MsgType {
	case msgDiscover:
		out := p.replyShell(offer)
		return msgOffer, out, true
	case msgRequest:
		// If the client requested a specific address that doesn't match what we
		// would assign, NAK it (our pool is authoritative for this subnet).
		if p.ReqIP != nil && !p.ReqIP.Equal(offer) && !p.CIAddr.Equal(offer) {
			out := p.replyShell(net.IPv4zero)
			return msgNAK, out, true
		}
		out := p.replyShell(offer)
		return msgACK, out, true
	default:
		// DECLINE / RELEASE / INFORM: nothing to hand back for this minimal server.
		return 0, nil, false
	}
}

// replyShell builds a reply Packet carrying the fields Encode echoes (xid,
// flags, chaddr, giaddr) and the offered yiaddr.
func (p *Packet) replyShell(yiaddr net.IP) *Packet {
	return &Packet{
		XID:    p.XID,
		Flags:  p.Flags,
		GIAddr: dup4(p.GIAddr),
		CHAddr: append(net.HardwareAddr(nil), p.CHAddr...),
		YIAddr: dup4(yiaddr),
	}
}

func dup4(ip net.IP) net.IP {
	if v := ip.To4(); v != nil {
		return net.IP(append([]byte(nil), v...))
	}
	return net.IPv4zero.To4()
}

// leaser hands out addresses from a nat node's pool, round-robin, remembering
// the MAC->IP binding so a client that DISCOVERs then REQUESTs gets the same
// address. It is not persistent (a supervisor restart re-leases from the top);
// that is fine for a lab NAT. Safe for concurrent use.
type leaser struct {
	mu      sync.Mutex
	base    net.IP // pool start (172.31.<n>.100)
	end     net.IP // pool end (172.31.<n>.199)
	next    uint32 // next candidate as a host-order uint32
	byMAC   map[string]uint32
	baseU32 uint32
	endU32  uint32
}

// newLeaser builds a leaser over [start,end] inclusive.
func newLeaser(start, end net.IP) *leaser {
	bs := ipToU32(start)
	en := ipToU32(end)
	return &leaser{
		base:    start.To4(),
		end:     end.To4(),
		next:    bs,
		byMAC:   make(map[string]uint32),
		baseU32: bs,
		endU32:  en,
	}
}

// lease returns an address for a MAC, reusing an existing binding or advancing
// through the pool. It wraps to the pool start when it reaches the end (a lab
// never has enough nodes on one nat to overrun a /24 host range, and there is no
// conflict tracking by design).
func (l *leaser) lease(mac net.HardwareAddr) net.IP {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := mac.String()
	if u, ok := l.byMAC[key]; ok {
		return u32ToIP(u)
	}
	u := l.next
	l.byMAC[key] = u
	if l.next >= l.endU32 {
		l.next = l.baseU32
	} else {
		l.next++
	}
	return u32ToIP(u)
}

func ipToU32(ip net.IP) uint32 {
	v := ip.To4()
	if v == nil {
		return 0
	}
	return binary.BigEndian.Uint32(v)
}

func u32ToIP(u uint32) net.IP {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], u)
	return net.IP(b[:])
}
