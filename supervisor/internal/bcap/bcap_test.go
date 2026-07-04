package bcap

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

// arpFrame is a minimal-but-real ARP request ethernet frame: broadcast dst,
// arbitrary src, ethertype 0x0806, then a bare ARP header (no full payload
// needed for parser correctness — parsePcapStream doesn't interpret frame
// contents at all).
func arpFrame() []byte {
	f := make([]byte, 42)
	for i := 0; i < 6; i++ {
		f[i] = 0xff // broadcast dst
	}
	copy(f[6:12], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}) // src mac
	f[12], f[13] = 0x08, 0x06                                 // ethertype ARP
	// arbitrary ARP body bytes to fill out a realistic length.
	for i := 14; i < len(f); i++ {
		f[i] = byte(i)
	}
	return f
}

// icmpFrame is a minimal-but-real IPv4/ICMP echo request ethernet frame.
func icmpFrame() []byte {
	f := make([]byte, 60)
	copy(f[0:6], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x02})  // dst mac
	copy(f[6:12], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}) // src mac
	f[12], f[13] = 0x08, 0x00                                 // ethertype IPv4
	l3 := 14
	f[l3] = 0x45 // version 4, IHL 5
	f[l3+9] = 1  // protocol ICMP
	f[l3+20] = 8 // ICMP type 8 = echo request
	return f
}

// writeGlobalHeader appends a 24-byte libpcap global header in the given
// byte order with the given magic (selecting us-vs-ns resolution).
func writeGlobalHeader(buf *bytes.Buffer, order binary.ByteOrder, magic uint32) {
	var hdr [24]byte
	order.PutUint32(hdr[0:4], magic)
	order.PutUint16(hdr[4:6], 2)            // version major
	order.PutUint16(hdr[6:8], 4)            // version minor
	order.PutUint32(hdr[8:12], 0)           // thiszone
	order.PutUint32(hdr[12:16], 0)          // sigfigs
	order.PutUint32(hdr[16:20], maxInclLen) // snaplen
	order.PutUint32(hdr[20:24], 1)          // network = LINKTYPE_ETHERNET
	buf.Write(hdr[:])
}

// writeRecord appends one 16-byte record header + frame body in the given
// byte order.
func writeRecord(buf *bytes.Buffer, order binary.ByteOrder, tsSec, tsFrac uint32, frame []byte) {
	var rhdr [16]byte
	order.PutUint32(rhdr[0:4], tsSec)
	order.PutUint32(rhdr[4:8], tsFrac)
	order.PutUint32(rhdr[8:12], uint32(len(frame)))
	order.PutUint32(rhdr[12:16], uint32(len(frame))) // orig_len == incl_len here
	buf.Write(rhdr[:])
	buf.Write(frame)
}

type gotFrame struct {
	data     []byte
	tsMicros uint64
}

func TestParsePcapStream_LittleEndianMicros(t *testing.T) {
	var buf bytes.Buffer
	writeGlobalHeader(&buf, binary.LittleEndian, magicUsLE)
	f1 := arpFrame()
	f2 := icmpFrame()
	writeRecord(&buf, binary.LittleEndian, 1000, 500000, f1)
	writeRecord(&buf, binary.LittleEndian, 1001, 250000, f2)

	var got []gotFrame
	err := parsePcapStream(&buf, func(frame []byte, ts uint64) {
		cp := append([]byte(nil), frame...)
		got = append(got, gotFrame{cp, ts})
	})
	if err != nil {
		t.Fatalf("parsePcapStream: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(got))
	}
	if !bytes.Equal(got[0].data, f1) {
		t.Errorf("frame 0 mismatch")
	}
	if !bytes.Equal(got[1].data, f2) {
		t.Errorf("frame 1 mismatch")
	}
	wantTS0 := uint64(1000)*1_000_000 + 500000
	wantTS1 := uint64(1001)*1_000_000 + 250000
	if got[0].tsMicros != wantTS0 {
		t.Errorf("ts0 = %d, want %d", got[0].tsMicros, wantTS0)
	}
	if got[1].tsMicros != wantTS1 {
		t.Errorf("ts1 = %d, want %d", got[1].tsMicros, wantTS1)
	}
}

func TestParsePcapStream_BigEndianMicros(t *testing.T) {
	var buf bytes.Buffer
	writeGlobalHeader(&buf, binary.BigEndian, magicUsLE) // magic written as native-order value; detector must swap
	f1 := arpFrame()
	writeRecord(&buf, binary.BigEndian, 42, 123456, f1)

	var got []gotFrame
	err := parsePcapStream(&buf, func(frame []byte, ts uint64) {
		cp := append([]byte(nil), frame...)
		got = append(got, gotFrame{cp, ts})
	})
	if err != nil {
		t.Fatalf("parsePcapStream: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(got))
	}
	if !bytes.Equal(got[0].data, f1) {
		t.Errorf("frame mismatch")
	}
	wantTS := uint64(42)*1_000_000 + 123456
	if got[0].tsMicros != wantTS {
		t.Errorf("ts = %d, want %d", got[0].tsMicros, wantTS)
	}
}

func TestParsePcapStream_NanosecondMagic(t *testing.T) {
	var buf bytes.Buffer
	writeGlobalHeader(&buf, binary.LittleEndian, magicNsLE)
	f1 := icmpFrame()
	// ts_frac in nanoseconds: 500,000,000 ns == 500,000 us.
	writeRecord(&buf, binary.LittleEndian, 7, 500_000_000, f1)

	var got []gotFrame
	err := parsePcapStream(&buf, func(frame []byte, ts uint64) {
		cp := append([]byte(nil), frame...)
		got = append(got, gotFrame{cp, ts})
	})
	if err != nil {
		t.Fatalf("parsePcapStream: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(got))
	}
	wantTS := uint64(7)*1_000_000 + 500000
	if got[0].tsMicros != wantTS {
		t.Errorf("ts = %d, want %d", got[0].tsMicros, wantTS)
	}
}

func TestParsePcapStream_TruncatedStreamIsClean(t *testing.T) {
	var buf bytes.Buffer
	writeGlobalHeader(&buf, binary.LittleEndian, magicUsLE)
	// Write a record header claiming more data than actually follows, then
	// stop (simulates tcpdump being killed mid-write).
	var rhdr [16]byte
	binary.LittleEndian.PutUint32(rhdr[8:12], 100)
	buf.Write(rhdr[:])
	buf.Write([]byte{1, 2, 3}) // far short of 100 bytes

	err := parsePcapStream(&buf, func(frame []byte, ts uint64) {
		t.Errorf("onFrame should not be called on truncated record")
	})
	if err != nil {
		t.Fatalf("expected clean nil error on truncation, got %v", err)
	}
}

func TestParsePcapStream_AbsurdInclLenRejected(t *testing.T) {
	var buf bytes.Buffer
	writeGlobalHeader(&buf, binary.LittleEndian, magicUsLE)
	var rhdr [16]byte
	binary.LittleEndian.PutUint32(rhdr[8:12], maxInclLen+1)
	buf.Write(rhdr[:])

	err := parsePcapStream(&buf, func(frame []byte, ts uint64) {
		t.Errorf("onFrame should not be called for an absurd incl_len")
	})
	if err == nil {
		t.Fatal("expected an error for implausible incl_len, got nil")
	}
}

func TestParsePcapStream_UnrecognizedMagic(t *testing.T) {
	var buf bytes.Buffer
	var hdr [24]byte
	binary.LittleEndian.PutUint32(hdr[0:4], 0xdeadbeef)
	buf.Write(hdr[:])

	err := parsePcapStream(&buf, func(frame []byte, ts uint64) {})
	if err == nil {
		t.Fatal("expected an error for unrecognized magic, got nil")
	}
}

// TestPcapngServer_ClientReceivesHeaderAndFrame connects a real TCP client to
// a pcapngServer, broadcasts a frame, and checks the client sees the pcapng
// Section Header Block (block type 0x0A0D0D0A, little-endian) immediately,
// followed eventually by packet-block bytes after Broadcast. We don't
// re-derive the pcapng wire format here (that's relay.PcapngWriter's job,
// already tested in internal/relay) — just confirm this server's plumbing
// delivers bytes end-to-end over a loopback TCP connection.
func TestPcapngServer_ClientReceivesHeaderAndFrame(t *testing.T) {
	srv, err := newPcapngServer("127.0.0.1", 0)
	if err != nil {
		t.Fatalf("newPcapngServer: %v", err)
	}
	defer srv.Close()

	conn, err := net.Dial("tcp", "127.0.0.1"+portSuffix(srv.Port()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Read the SHB: first 4 bytes must be the pcapng block type 0x0A0D0D0A,
	// little-endian on the wire.
	var shbType [4]byte
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	if _, err := io.ReadFull(conn, shbType[:]); err != nil {
		t.Fatalf("read SHB type: %v", err)
	}
	wantSHB := []byte{0x0a, 0x0d, 0x0d, 0x0a}
	if !bytes.Equal(shbType[:], wantSHB) {
		t.Fatalf("SHB type = % x, want % x", shbType, wantSHB)
	}

	// Drain the rest of the SHB + the IDB. We don't know the exact lengths
	// without re-deriving the writer's framing, so just read whatever is
	// buffered up to a short timeout, then broadcast a frame and confirm
	// more bytes arrive.
	drainAvailable(t, conn)

	frame := arpFrame()
	srv.Broadcast(frame, 123456)

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	var epbType [4]byte
	if _, err := io.ReadFull(conn, epbType[:]); err != nil {
		t.Fatalf("read EPB type: %v", err)
	}
	wantEPB := []byte{0x06, 0x00, 0x00, 0x00}
	if !bytes.Equal(epbType[:], wantEPB) {
		t.Fatalf("EPB type = % x, want % x", epbType, wantEPB)
	}
}

func drainAvailable(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	buf := make([]byte, 4096)
	for {
		_, err := conn.Read(buf)
		if err != nil {
			return
		}
	}
}

func portSuffix(port int) string {
	return ":" + strconv.Itoa(port)
}
