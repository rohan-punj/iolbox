package main

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestDecodeHandWrittenDHCPOfferOptions(t *testing.T) {
	offer := append(make([]byte, 240), []byte{
		53, 1, 2,
		1, 4, 255, 255, 255, 0,
		3, 4, 10, 0, 0, 1,
		6, 8, 10, 0, 0, 53, 10, 0, 0, 54,
		42, 4, 10, 0, 0, 123,
		51, 4, 0, 0, 14, 16,
		54, 4, 10, 0, 0, 2,
		66, 10, 't', 'f', 't', 'p', '.', 'l', 'o', 'c', 'a', 'l',
		150, 8, 10, 0, 0, 2, 10, 0, 0, 3,
		255,
	}...)
	offer[0], offer[1], offer[2] = 2, 1, 6
	offer[236], offer[237], offer[238], offer[239] = 0x63, 0x82, 0x53, 0x63
	p, err := DecodeDHCP(offer)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Option(66); string(got) != "tftp.local" {
		t.Fatalf("option 66 = %q, want string tftp.local", got)
	}
	if got := p.Option(150); !bytes.Equal(got, []byte{10, 0, 0, 2, 10, 0, 0, 3}) {
		t.Fatalf("option 150 = %v", got)
	}
	if got := p.Option(6); len(got) != 8 {
		t.Fatalf("option 6 length = %d", len(got))
	}
	if got := p.Option(42); !bytes.Equal(got, []byte{10, 0, 0, 123}) {
		t.Fatalf("option 42 = %v", got)
	}
	retained := leaseOptions(p)
	for _, code := range []byte{1, 3, 6, 42, 51, 54, 66, 150} {
		found := false
		for _, option := range retained {
			if option.Code == code {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("offer option %d did not surface in lease details: %+v", code, retained)
		}
	}
}

func TestDecodeHandWrittenDHCPRequestParameterAndRelayOptions(t *testing.T) {
	request := append(make([]byte, 240), []byte{
		53, 1, 3,
		55, 3, 1, 3, 6,
		82, 9, 1, 3, 'G', 'i', '1', 2, 2, 'R', '1',
		61, 7, 1, 2, 3, 4, 5, 6, 7,
		12, 6, 'r', 'o', 'u', 't', 'e', 'r',
		60, 3, 'I', 'O', 'S',
		255,
	}...)
	request[0], request[1], request[2], request[3] = 1, 1, 6, 0
	request[236], request[237], request[238], request[239] = 0x63, 0x82, 0x53, 0x63
	p, err := DecodeDHCP(request)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Option(55); !bytes.Equal(got, []byte{1, 3, 6}) {
		t.Fatalf("PRL = %v", got)
	}
	info := parseRelayInfo(p.Option(82))
	if info.CircuitID != "Gi1" || info.RemoteID != "R1" {
		t.Fatalf("relay info = %+v", info)
	}
	if got := string(p.Option(12)); got != "router" {
		t.Fatalf("hostname = %q", got)
	}
	if got := p.Option(61); !bytes.Equal(got, []byte{1, 2, 3, 4, 5, 6, 7}) {
		t.Fatalf("client id = %v", got)
	}
}

func TestDHCPEncodeKeepsOption66StringAnd150AddressList(t *testing.T) {
	b := EncodeDHCP(DHCPPacket{Op: dhcpBootReply, Options: []DHCPOption{
		{Code: 66, Value: []byte("10.0.0.10")},
		{Code: 150, Value: []byte{10, 0, 0, 10, 10, 0, 0, 11}},
	}})
	if !bytes.Contains(b, []byte{66, 9, '1', '0', '.', '0', '.', '0', '.', '1', '0'}) {
		t.Fatalf("option 66 was not encoded as a string: %v", b[240:])
	}
	if !bytes.Contains(b, []byte{150, 8, 10, 0, 0, 10, 10, 0, 0, 11}) {
		t.Fatalf("option 150 was not encoded as 4-byte addresses: %v", b[240:])
	}
}

func TestDHCPRelayReplyDestination(t *testing.T) {
	p := DHCPPacket{GIAddr: net.IPv4(10, 1, 0, 1)}
	dest := dhcpReplyDestination(p, Lease{IP: "192.0.2.100"})
	if !dest.IP.Equal(net.IPv4(10, 1, 0, 1)) || dest.Port != 67 {
		t.Fatalf("relay destination = %v, want 10.1.0.1:67", dest)
	}
}

func TestDHCPHandleUsesRelayPoolAndRetainsLeaseDetails(t *testing.T) {
	store := NewStore("")
	cfg := store.Snapshot()
	cfg.DHCP.ServerIP = "10.1.0.2"
	cfg.DHCP.Pools = []DHCPPool{{Subnet: "10.1.0.0/24", RangeStart: "10.1.0.100", RangeEnd: "10.1.0.100", Router: "10.1.0.1"}}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	s := NewDHCPServer(store)
	p := DHCPPacket{HType: 1, HLen: 6, Hops: 1, XID: 7, GIAddr: net.IPv4(10, 1, 0, 1), Options: []DHCPOption{
		{Code: 53, Value: []byte{1}},
		{Code: 55, Value: []byte{1, 3, 6}},
		{Code: 82, Value: []byte{1, 3, 'G', 'i', '1'}},
	}}
	reply, lease, send, err := s.Handle(p, time.Unix(100, 0))
	if err != nil || !send {
		t.Fatalf("Handle: reply=%+v lease=%+v send=%v err=%v", reply, lease, send, err)
	}
	if lease.IP != "10.1.0.100" || lease.GIAddr != "10.1.0.1" || lease.CircuitID != "Gi1" {
		t.Fatalf("lease = %+v", lease)
	}
	if !reply.GIAddr.Equal(net.IPv4(10, 1, 0, 1)) || !reply.YIAddr.Equal(net.IPv4(10, 1, 0, 100)) {
		t.Fatalf("reply addresses = yiaddr %v giaddr %v", reply.YIAddr, reply.GIAddr)
	}
}
