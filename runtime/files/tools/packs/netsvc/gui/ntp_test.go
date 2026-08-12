package main

import (
	"bytes"
	"testing"
	"time"
)

func TestNTPMode4LayoutAndOriginateEcho(t *testing.T) {
	request := make([]byte, 48)
	request[0] = 0x1b // LI=0, VN=3, Mode=3
	copy(request[40:48], []byte{0xde, 0xad, 0xbe, 0xef, 0x12, 0x34, 0x56, 0x78})
	receive := time.Unix(1_700_000_000, 250_000_000)
	transmit := receive.Add(2 * time.Millisecond)
	reply, err := encodeNTPResponse(request, receive, transmit, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(reply) != 48 {
		t.Fatalf("NTP reply length = %d", len(reply))
	}
	if reply[0] != 0x1c || reply[1] != 1 || string(reply[12:16]) != "LOCL" {
		t.Fatalf("header = %x", reply[:16])
	}
	if !bytes.Equal(reply[24:32], request[40:48]) {
		t.Fatalf("Originate = %x, want exact client transmit %x", reply[24:32], request[40:48])
	}
	if _, err := encodeNTPResponse(append([]byte{0x16}, request[1:]...), receive, transmit, 3, ""); err == nil {
		t.Fatal("mode 6 request was accepted")
	}
}

func TestNTPTimestampKnownLiteral(t *testing.T) {
	tm := time.Unix(946684800, 123456789).UTC() // 2000-01-01 00:00:00.123456789
	got := encodeNTPTimestamp(tm)
	want := []byte{0xbc, 0x17, 0xc2, 0x00, 0x1f, 0x9a, 0xdd, 0x37}
	if !bytes.Equal(got[:], want) {
		t.Fatalf("NTP timestamp = %x, want %x", got, want)
	}
	roundTrip, err := decodeNTPTimestamp(want)
	if err != nil {
		t.Fatal(err)
	}
	if roundTrip.Unix() != tm.Unix() || roundTrip.Nanosecond() != 123456788 {
		t.Fatalf("round trip = %s", roundTrip.Format(time.RFC3339Nano))
	}
}

func TestNTPRejectsModes6And7(t *testing.T) {
	for _, mode := range []byte{6, 7} {
		req := make([]byte, 48)
		req[0] = 0x18 | mode
		if _, err := encodeNTPResponse(req, time.Now(), time.Now(), 3, ""); err == nil {
			t.Fatalf("mode %d accepted", mode)
		}
	}
}
