package relay

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestStripIOLHeader(t *testing.T) {
	frame := []byte{0xAA, 0xBB, 0xCC}
	datagram := append(make([]byte, IOLHeaderSize), frame...)
	got := StripIOLHeader(datagram)
	if !bytes.Equal(got, frame) {
		t.Fatalf("strip: %v", got)
	}
	if StripIOLHeader([]byte{1, 2, 3}) != nil {
		t.Fatal("short datagram should return nil")
	}
}

func TestPcapngStructure(t *testing.T) {
	var buf bytes.Buffer
	w := NewPcapngWriter(&buf)
	frame := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99} // 10 bytes -> needs 2 pad
	if err := w.WriteFrame(frame, 1234567890); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	le := binary.LittleEndian

	// --- SHB ---
	if le.Uint32(b[0:]) != 0x0A0D0D0A {
		t.Fatalf("SHB type = 0x%08X", le.Uint32(b[0:]))
	}
	shbLen := le.Uint32(b[4:])
	if le.Uint32(b[8:]) != 0x1A2B3C4D {
		t.Fatalf("byte-order magic = 0x%08X", le.Uint32(b[8:]))
	}
	if le.Uint32(b[int(shbLen)-4:]) != shbLen {
		t.Fatalf("SHB trailing length mismatch")
	}

	// --- IDB ---
	off := int(shbLen)
	if le.Uint32(b[off:]) != 0x00000001 {
		t.Fatalf("IDB type = 0x%08X", le.Uint32(b[off:]))
	}
	idbLen := le.Uint32(b[off+4:])
	if le.Uint16(b[off+8:]) != LinkTypeEthernet {
		t.Fatalf("linktype = %d", le.Uint16(b[off+8:]))
	}
	if le.Uint32(b[off+int(idbLen)-4:]) != idbLen {
		t.Fatalf("IDB trailing length mismatch")
	}

	// --- EPB ---
	off += int(idbLen)
	if le.Uint32(b[off:]) != 0x00000006 {
		t.Fatalf("EPB type = 0x%08X", le.Uint32(b[off:]))
	}
	epbLen := le.Uint32(b[off+4:])
	capLen := le.Uint32(b[off+20:])
	origLen := le.Uint32(b[off+24:])
	if capLen != uint32(len(frame)) || origLen != uint32(len(frame)) {
		t.Fatalf("caplen=%d origlen=%d want %d", capLen, origLen, len(frame))
	}
	if !bytes.Equal(b[off+28:off+28+len(frame)], frame) {
		t.Fatal("frame bytes not preserved")
	}
	// EPB length must be 32-bit aligned and match trailer.
	if epbLen%4 != 0 {
		t.Fatalf("EPB length %d not 4-aligned", epbLen)
	}
	if le.Uint32(b[off+int(epbLen)-4:]) != epbLen {
		t.Fatalf("EPB trailing length mismatch")
	}
	// Total consumed equals buffer length (no trailing garbage).
	if off+int(epbLen) != len(b) {
		t.Fatalf("consumed %d != buffer %d", off+int(epbLen), len(b))
	}
}

func TestPcapngHeaderIdempotent(t *testing.T) {
	var buf bytes.Buffer
	w := NewPcapngWriter(&buf)
	_ = w.WriteHeader()
	n1 := buf.Len()
	_ = w.WriteHeader()
	if buf.Len() != n1 {
		t.Fatal("WriteHeader not idempotent")
	}
}
