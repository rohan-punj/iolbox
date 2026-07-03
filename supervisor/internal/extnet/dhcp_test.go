package extnet

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

// buildDiscover crafts a raw DHCP DISCOVER datagram (the UDP payload) for a
// client MAC, with the broadcast flag set and a transaction id, so the codec can
// be exercised without a real client.
func buildDiscover(xid uint32, mac net.HardwareAddr, msgType byte, reqIP net.IP) []byte {
	b := make([]byte, dhcpMinLen)
	b[0] = opBootRequest
	b[1] = htypeEthernet
	b[2] = hlenEthernet
	binary.BigEndian.PutUint32(b[4:8], xid)
	binary.BigEndian.PutUint16(b[10:12], flagBroadcast)
	copy(b[28:34], mac)
	binary.BigEndian.PutUint32(b[236:240], dhcpMagic)
	opts := []byte{optMessageType, 1, msgType}
	if reqIP != nil {
		opts = append(opts, optRequestedIP, 4)
		opts = append(opts, reqIP.To4()...)
	}
	opts = append(opts, optEnd)
	return append(b, opts...)
}

// findOpt returns the value of a DHCP option in an encoded datagram, or nil.
func findOpt(b []byte, code byte) []byte {
	if len(b) < dhcpMinLen {
		return nil
	}
	opts := b[dhcpMinLen:]
	for i := 0; i < len(opts); {
		if opts[i] == optEnd {
			break
		}
		if opts[i] == optPad {
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
		if opts[i] == code {
			return opts[i+2 : i+2+length]
		}
		i += 2 + length
	}
	return nil
}

// TestDHCPDiscoverToOffer is the headline codec test: craft a DISCOVER, decode
// it, run the server policy, encode the OFFER, and assert every field a client
// needs — yiaddr from the pool, msg-type OFFER, and router/dns/server/mask all
// the gateway/mask.
func TestDHCPDiscoverToOffer(t *testing.T) {
	gw := net.ParseIP("172.31.3.1")
	mask := net.IPv4(255, 255, 255, 0)
	mac := net.HardwareAddr{0x52, 0x54, 0x00, 0xaa, 0xbb, 0xcc}

	raw := buildDiscover(0x12345678, mac, msgDiscover, nil)
	req, err := DecodePacket(raw)
	if err != nil {
		t.Fatalf("decode discover: %v", err)
	}
	if req.MsgType != msgDiscover {
		t.Fatalf("decoded msg type = %d, want DISCOVER", req.MsgType)
	}
	if req.XID != 0x12345678 {
		t.Fatalf("xid = %#x", req.XID)
	}
	if !bytes.Equal(req.CHAddr, mac) {
		t.Fatalf("chaddr = %s, want %s", req.CHAddr, mac)
	}

	offer := net.ParseIP("172.31.3.100")
	replyType, reply, ok := req.Handle(offer)
	if !ok || replyType != msgOffer {
		t.Fatalf("Handle(discover) = type %d ok %v, want OFFER", replyType, ok)
	}
	if !reply.YIAddr.Equal(offer) {
		t.Fatalf("offered yiaddr = %s, want %s", reply.YIAddr, offer)
	}
	if reply.XID != req.XID {
		t.Fatalf("reply xid must echo request: %#x vs %#x", reply.XID, req.XID)
	}

	enc := reply.Encode(msgOffer, gw, mask)
	dec, err := DecodePacket(enc)
	if err != nil {
		t.Fatalf("decode offer: %v", err)
	}
	if dec.Op != opBootReply {
		t.Fatalf("encoded op = %d, want BOOTREPLY", dec.Op)
	}
	if !dec.YIAddr.Equal(offer) {
		t.Fatalf("round-trip yiaddr = %s, want %s", dec.YIAddr, offer)
	}
	// Option 53 = OFFER.
	if v := findOpt(enc, optMessageType); len(v) != 1 || v[0] != msgOffer {
		t.Fatalf("option 53 = %v, want [OFFER]", v)
	}
	// router (3) + server-id (54) = the gateway; mask (1) = the mask.
	for _, tc := range []struct {
		code byte
		want net.IP
		name string
	}{
		{optRouter, gw, "router"},
		{optServerID, gw, "server-id"},
		{optSubnetMask, mask, "mask"},
	} {
		v := findOpt(enc, tc.code)
		if len(v) != 4 || !net.IP(v).Equal(tc.want) {
			t.Fatalf("option %s = %v, want %s", tc.name, v, tc.want)
		}
	}
	// DNS (6) = the public resolvers, NOT the gateway (which runs no resolver):
	// one option carrying 1.1.1.1 then 8.8.8.8.
	if v := findOpt(enc, optDNS); len(v) != 8 ||
		!net.IP(v[:4]).Equal(net.IPv4(1, 1, 1, 1)) ||
		!net.IP(v[4:]).Equal(net.IPv4(8, 8, 8, 8)) {
		t.Fatalf("option dns = %v, want 1.1.1.1 + 8.8.8.8", v)
	}
	// lease time (51) = 3600s.
	if v := findOpt(enc, optLeaseTime); len(v) != 4 || binary.BigEndian.Uint32(v) != leaseSeconds {
		t.Fatalf("lease time = %v, want %d", v, leaseSeconds)
	}
}

// TestDHCPRequestToACK confirms a REQUEST for the offered address ACKs, and a
// REQUEST for a different address NAKs (our pool is authoritative).
func TestDHCPRequestToACK(t *testing.T) {
	mac := net.HardwareAddr{0x52, 0x54, 0x00, 0x11, 0x22, 0x33}
	offer := net.ParseIP("172.31.3.100")

	req, err := DecodePacket(buildDiscover(1, mac, msgRequest, offer))
	if err != nil {
		t.Fatal(err)
	}
	replyType, _, ok := req.Handle(offer)
	if !ok || replyType != msgACK {
		t.Fatalf("REQUEST for offered addr = type %d ok %v, want ACK", replyType, ok)
	}

	wrong, err := DecodePacket(buildDiscover(2, mac, msgRequest, net.ParseIP("10.0.0.5")))
	if err != nil {
		t.Fatal(err)
	}
	replyType, _, ok = wrong.Handle(offer)
	if !ok || replyType != msgNAK {
		t.Fatalf("REQUEST for foreign addr = type %d ok %v, want NAK", replyType, ok)
	}
}

// TestDHCPDecodeRejectsGarbage confirms a too-short or cookie-less datagram is
// rejected rather than mis-parsed.
func TestDHCPDecodeRejectsGarbage(t *testing.T) {
	if _, err := DecodePacket([]byte{1, 2, 3}); err == nil {
		t.Fatal("short datagram must be rejected")
	}
	b := make([]byte, dhcpMinLen)
	// no magic cookie
	if _, err := DecodePacket(b); err == nil {
		t.Fatal("missing magic cookie must be rejected")
	}
}
