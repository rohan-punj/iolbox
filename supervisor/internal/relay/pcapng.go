// Package relay is the UDP data plane: p2p forwards, segment hubs, and an
// optional capture tee that serves a pcapng byte stream for Wireshark.
//
// This file implements the platform-independent pieces: a minimal pcapng writer
// and the IOL UDP header stripper. Both are unit-tested on any OS. The actual
// UDP socket wiring lives in the //go:build linux files.
package relay

import (
	"encoding/binary"
	"io"
)

// LinkTypeEthernet is the pcapng LINKTYPE for Ethernet frames.
const LinkTypeEthernet = 1

// IOLHeaderSize is the number of bytes the IOL/iouyap UDP framing prepends
// before the ethernet frame. Per the iouyap header (dst_ids[2], src_ids[2],
// dst_port[1], src_port[1], msg_type[1], channel[1]) this is 8 bytes.
//
// ASSUMPTION (verify in P0 against a real IOL image): that IOL prepends exactly
// this 8-byte header on the wire and that the ethernet frame begins immediately
// after it. This is a named constant precisely so P0 can adjust it in one place.
const IOLHeaderSize = 8

// StripIOLHeader removes the IOL UDP header from a datagram, returning the clean
// ethernet frame. If the datagram is shorter than the header it returns nil.
func StripIOLHeader(datagram []byte) []byte {
	if len(datagram) < IOLHeaderSize {
		return nil
	}
	return datagram[IOLHeaderSize:]
}

// PcapngWriter writes a pcapng stream: a Section Header Block, one Interface
// Description Block (LINKTYPE_ETHERNET), then an Enhanced Packet Block per
// frame. All blocks are little-endian and 32-bit aligned per the spec.
type PcapngWriter struct {
	w        io.Writer
	wroteHdr bool
}

// NewPcapngWriter returns a writer over w. The SHB+IDB are emitted on the first
// WriteFrame (or explicitly via WriteHeader).
func NewPcapngWriter(w io.Writer) *PcapngWriter {
	return &PcapngWriter{w: w}
}

// Block type identifiers (pcapng spec).
const (
	blockTypeSHB = 0x0A0D0D0A
	blockTypeIDB = 0x00000001
	blockTypeEPB = 0x00000006

	// byteOrderMagic identifies little-endian sections.
	byteOrderMagic = 0x1A2B3C4D
	pcapngMajor    = 1
	pcapngMinor    = 0
	snapLen        = 262144
)

// WriteHeader emits the Section Header Block and Interface Description Block.
// It is idempotent; the first WriteFrame calls it automatically.
func (p *PcapngWriter) WriteHeader() error {
	if p.wroteHdr {
		return nil
	}
	if err := p.writeSHB(); err != nil {
		return err
	}
	if err := p.writeIDB(); err != nil {
		return err
	}
	p.wroteHdr = true
	return nil
}

func (p *PcapngWriter) writeSHB() error {
	// SHB body: byte-order magic(4) + major(2) + minor(2) + section length(8).
	const bodyLen = 4 + 2 + 2 + 8
	total := 12 + bodyLen // + block-type(4)+len(4)+trailing-len(4)
	buf := make([]byte, total)
	le := binary.LittleEndian
	le.PutUint32(buf[0:], blockTypeSHB)
	le.PutUint32(buf[4:], uint32(total))
	le.PutUint32(buf[8:], byteOrderMagic)
	le.PutUint16(buf[12:], pcapngMajor)
	le.PutUint16(buf[14:], pcapngMinor)
	// section length = -1 (unknown), int64.
	le.PutUint64(buf[16:], ^uint64(0))
	le.PutUint32(buf[total-4:], uint32(total))
	_, err := p.w.Write(buf)
	return err
}

func (p *PcapngWriter) writeIDB() error {
	// IDB body: linktype(2) + reserved(2) + snaplen(4).
	const bodyLen = 2 + 2 + 4
	total := 12 + bodyLen
	buf := make([]byte, total)
	le := binary.LittleEndian
	le.PutUint32(buf[0:], blockTypeIDB)
	le.PutUint32(buf[4:], uint32(total))
	le.PutUint16(buf[8:], LinkTypeEthernet)
	le.PutUint16(buf[10:], 0) // reserved
	le.PutUint32(buf[12:], snapLen)
	le.PutUint32(buf[total-4:], uint32(total))
	_, err := p.w.Write(buf)
	return err
}

// WriteFrame writes one Enhanced Packet Block for a clean ethernet frame,
// timestamped at tsMicros (microseconds since the Unix epoch). It emits the
// SHB+IDB first if not already written.
func (p *PcapngWriter) WriteFrame(frame []byte, tsMicros uint64) error {
	if err := p.WriteHeader(); err != nil {
		return err
	}
	capLen := len(frame)
	// EPB body: iface(4) + ts_high(4) + ts_low(4) + caplen(4) + origlen(4)
	//           + packet data + padding to 4.
	pad := (4 - (capLen % 4)) % 4
	bodyLen := 4 + 4 + 4 + 4 + 4 + capLen + pad
	total := 12 + bodyLen
	buf := make([]byte, total)
	le := binary.LittleEndian
	le.PutUint32(buf[0:], blockTypeEPB)
	le.PutUint32(buf[4:], uint32(total))
	le.PutUint32(buf[8:], 0) // interface id 0
	le.PutUint32(buf[12:], uint32(tsMicros>>32))
	le.PutUint32(buf[16:], uint32(tsMicros&0xFFFFFFFF))
	le.PutUint32(buf[20:], uint32(capLen))
	le.PutUint32(buf[24:], uint32(capLen))
	copy(buf[28:], frame)
	le.PutUint32(buf[total-4:], uint32(total))
	_, err := p.w.Write(buf)
	return err
}
