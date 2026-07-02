package iouyap

import (
	"bytes"
	"testing"
)

func TestHeaderRoundTrip(t *testing.T) {
	cases := []Header{
		{},
		{DstID: 1, SrcID: 2, DstPort: 0, SrcPort: 1, MsgType: MsgTypeData, Unused: 0},
		{DstID: 0xFFFF, SrcID: 0xFFFF, DstPort: 0xFF, SrcPort: 0xFF, MsgType: 0xFF, Unused: 0xFF},
		{DstID: 258, SrcID: 4660, DstPort: 3, SrcPort: 5, MsgType: MsgTypeData, Unused: 7},
	}
	for i, want := range cases {
		buf := BuildHeader(want)
		if len(buf) != HeaderSize {
			t.Fatalf("case %d: BuildHeader length = %d, want %d", i, len(buf), HeaderSize)
		}
		got, ok := ParseHeader(buf)
		if !ok {
			t.Fatalf("case %d: ParseHeader reported not-ok for a full-size header", i)
		}
		if got != want {
			t.Fatalf("case %d: round trip mismatch: got %+v, want %+v", i, got, want)
		}
	}
}

func TestHeaderFieldOffsets(t *testing.T) {
	// Pin the exact byte layout so a future accidental reordering is caught:
	// big-endian dst_ids[2], src_ids[2], dst_port[1], src_port[1],
	// msg_type[1], unused[1] -- matching relay.IOLHeaderSize's documented
	// layout for the same 8-byte framing.
	h := Header{DstID: 0x0102, SrcID: 0x0304, DstPort: 0x05, SrcPort: 0x06, MsgType: 0x07, Unused: 0x08}
	buf := BuildHeader(h)
	want := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	if !bytes.Equal(buf, want) {
		t.Fatalf("BuildHeader byte layout = % x, want % x", buf, want)
	}
}

func TestParseHeaderShort(t *testing.T) {
	for n := 0; n < HeaderSize; n++ {
		if _, ok := ParseHeader(make([]byte, n)); ok {
			t.Fatalf("ParseHeader accepted a %d-byte datagram, want ok=false (HeaderSize=%d)", n, HeaderSize)
		}
	}
}

func TestPayload(t *testing.T) {
	h := Header{DstID: 1, SrcID: 2, MsgType: MsgTypeData}
	frame := []byte{0xAA, 0xBB, 0xCC, 0xDD}
	datagram := WithPayload(h, frame)

	if len(datagram) != HeaderSize+len(frame) {
		t.Fatalf("WithPayload length = %d, want %d", len(datagram), HeaderSize+len(frame))
	}
	got := Payload(datagram)
	if !bytes.Equal(got, frame) {
		t.Fatalf("Payload = % x, want % x", got, frame)
	}

	gotHdr, ok := ParseHeader(datagram)
	if !ok || gotHdr != h {
		t.Fatalf("ParseHeader(WithPayload(h, frame)) = %+v, %v, want %+v, true", gotHdr, ok, h)
	}
}

func TestPayloadShort(t *testing.T) {
	if p := Payload(make([]byte, HeaderSize-1)); p != nil {
		t.Fatalf("Payload of a short datagram = %v, want nil", p)
	}
}

func TestWithPayloadEmptyFrame(t *testing.T) {
	h := Header{DstID: 9, SrcID: 9}
	datagram := WithPayload(h, nil)
	if len(datagram) != HeaderSize {
		t.Fatalf("WithPayload with empty frame length = %d, want %d", len(datagram), HeaderSize)
	}
	if p := Payload(datagram); len(p) != 0 {
		t.Fatalf("Payload of empty-frame datagram = % x, want empty", p)
	}
}
