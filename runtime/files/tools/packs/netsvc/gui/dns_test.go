package main

import (
	"bytes"
	"testing"
)

func TestParseHandWrittenDNSCNAMEChainCompression(t *testing.T) {
	packet := []byte{
		0x12, 0x34, 0x84, 0x00, 0x00, 0x01, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00,
		0x05, 'a', 'l', 'i', 'a', 's', 0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 0x00, 0x00, 0x01, 0x00, 0x01,
		0xc0, 0x0c, 0x00, 0x05, 0x00, 0x01, 0x00, 0x00, 0x01, 0x2c, 0x00, 0x09, 0x06, 't', 'a', 'r', 'g', 'e', 't', 0xc0, 0x12,
		0xc0, 0x0c, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x3c, 0x00, 0x04, 0xc0, 0x00, 0x02, 0x0a,
	}
	msg, err := ParseDNSMessage(packet)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Answers) != 2 || msg.Answers[0].Text != "target.example." || msg.Answers[1].Type != dnsTypeA {
		t.Fatalf("answers = %+v", msg.Answers)
	}
	if !bytes.Equal(packet[31:33], []byte{0xc0, 0x0c}) || !bytes.Equal(packet[50:52], []byte{0xc0, 0x12}) {
		t.Fatal("hand-written compression pointers moved")
	}
}

func TestEncodeDNSCNAMEChainUsesCompressionPointers(t *testing.T) {
	msg := DNSMessage{Header: DNSHeader{ID: 0x1234, Flags: dnsQR | dnsAA}, Questions: []DNSQuestion{{Name: "alias.example.", Type: dnsTypeA, Class: dnsClassIN}}, Answers: []DNSRR{{Name: "alias.example.", Type: dnsTypeCNAME, Class: dnsClassIN, TTL: 300, Text: "target.example."}, {Name: "alias.example.", Type: dnsTypeA, Class: dnsClassIN, TTL: 60, Data: []byte{192, 0, 2, 10}}}}
	packet := EncodeDNSMessage(msg)
	if !bytes.Equal(packet[31:33], []byte{0xc0, 0x0c}) {
		t.Fatalf("owner name pointer = %x", packet[31:33])
	}
	// CNAME RDATA is label "target" followed by a pointer to the question's
	// example label at offset 18 (0x12), not a self-generated uncompressed name.
	if !bytes.Equal(packet[50:52], []byte{0xc0, 0x12}) {
		t.Fatalf("CNAME suffix pointer = %x", packet[50:52])
	}
	parsed, err := ParseDNSMessage(packet)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Answers) != 2 || parsed.Answers[0].Text != "target.example." {
		t.Fatalf("round trip answers = %+v", parsed.Answers)
	}
}

func TestDNSCompressionPointerLoopAndForwardPointerRejected(t *testing.T) {
	loop := []byte{0, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0xc0, 0x0c, 0, 1, 0, 1}
	if _, err := ParseDNSMessage(loop); err == nil {
		t.Fatal("self-referential compression pointer was accepted")
	}
	forward := []byte{0, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0xc0, 0x0e, 0, 1, 0, 1, 0}
	if _, err := ParseDNSMessage(forward); err == nil {
		t.Fatal("forward compression pointer was accepted")
	}
}

func TestDNSFlagsKeepThreeReservedZBitsAndEchoRD(t *testing.T) {
	store := NewStore("")
	cfg := store.Snapshot()
	cfg.DNS.Zone = "example."
	cfg.DNS.Records = []DNSRecord{{Name: "alias.example.", Type: "CNAME", Value: "target.example.", TTL: 300}, {Name: "target.example.", Type: "A", Value: "192.0.2.10", TTL: 60}}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	s := NewDNSServer(store)
	query := DNSMessage{Header: DNSHeader{ID: 1, Flags: dnsRD}, Questions: []DNSQuestion{{Name: "alias.example.", Type: dnsTypeA, Class: dnsClassIN}}}
	response, row := s.Answer(query, "10.0.0.1:1234")
	if row.RCode != "NOERROR" || len(response.Answers) != 2 {
		t.Fatalf("response=%+v row=%+v", response, row)
	}
	if response.Header.Flags&dnsRD == 0 || response.Header.Flags&0x0080 != 0 || response.Header.Flags&0x0070 != 0 {
		t.Fatalf("flags=%04x; RD should echo, RA/Z must be zero", response.Header.Flags)
	}
	unknown := query
	unknown.Questions = []DNSQuestion{{Name: "google.com.", Type: dnsTypeA, Class: dnsClassIN}}
	nxdomain, nrow := s.Answer(unknown, "10.0.0.1:1234")
	if nrow.RCode != "NXDOMAIN" || nxdomain.Header.Flags&0xf != 3 || nxdomain.Header.Flags&dnsAA == 0 {
		t.Fatalf("NXDOMAIN response=%+v row=%+v", nxdomain, nrow)
	}
}
