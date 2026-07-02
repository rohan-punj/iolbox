// Package iouyap bridges one IOL netio unix-domain datagram socket to one UDP
// endpoint, mirroring the classic GNS3/IOU "iouyap" helper. IOL only speaks
// netio over a unix-domain socket for links declared in its NETMAP; the
// supervisor's UDP relay (internal/relay) needs a UDP peer to tee frames to
// pcapng and to forward to VPCS or a cross-host peer. This package is the
// glue: it owns the unix socket IOL connects to and pumps datagrams to/from a
// UDP socket bound to the relay's endpoint.
//
// See README.md in this directory for the full mechanism writeup and the P0
// verification checklist for the netio header assumptions below.
package iouyap

import "encoding/binary"

// HeaderSize is the number of bytes the netio framing prepends before the raw
// ethernet frame on every unix-domain datagram IOL sends or receives. The
// header exists ONLY on the netio (unix socket) side: this bridge strips it
// when forwarding to UDP and constructs a fresh one when delivering from UDP,
// so the UDP mesh (relay, VPCS, capture tee, cross-host tunnels) carries raw
// ethernet frames with no header at all.
//
// CONFIRMED against real IOL 17.18.02 wire bytes (docs/p0-spike.md "netio
// header layout", 2026-07-02): an instance-1 Ethernet0/1 interface wired to
// pseudo-instance 501 emits datagrams beginning
//
//	01 f5 00 01 00 10 01 00
//
// which decodes, big-endian, as:
//
//	offset 0: dst_id   uint16  destination instance id (0x01f5 = 501)
//	offset 2: src_id   uint16  source instance id (0x0001 = 1)
//	offset 4: dst_port uint8   destination interface, port*16+adapter (0/0 = 0x00)
//	offset 5: src_port uint8   source interface, port*16+adapter (Et0/1 = 0x10)
//	offset 6: msg_type uint8   1 = data frame (every observed frame)
//	offset 7: channel  uint8   0 on every observed frame
//
// Note two corrections vs the classic iouyap.c assumption this package
// originally carried: the port byte nibble order is port<<4|adapter (Et0/1 ->
// 0x10, Et1/0 -> 0x01, verified with both), and data frames carry msg_type 1,
// not 0.
const HeaderSize = 8

// Byte offsets within the 8-byte netio header.
const (
	offDstID   = 0 // uint16
	offSrcID   = 2 // uint16
	offDstPort = 4 // uint8
	offSrcPort = 5 // uint8
	offMsgType = 6 // uint8
	offUnused  = 7 // uint8
)

// MsgType values for offMsgType. Real IOL marks every data frame with 1
// (confirmed on the wire, see HeaderSize); other values were never observed
// and are preserved (not interpreted) when relaying.
const (
	// MsgTypeData marks an ordinary ethernet-frame payload.
	MsgTypeData uint8 = 1
)

// EncodePortByte packs an interface's adapter/port coordinates into the
// single netio header port byte. Confirmed nibble order on real IOL 17.18.02:
// port in the high nibble, adapter in the low nibble (Ethernet0/1 -> 0x10,
// Ethernet1/0 -> 0x01). This is the REVERSE of netmap.Iface.Index()'s flat
// adapter*16+port index; the two encodings must not be conflated.
func EncodePortByte(adapter, port int) uint8 {
	return uint8(port<<4 | adapter&0x0f)
}

// Header is the parsed form of the 8-byte netio/iouyap framing prefix. See
// the ASSUMPTION comment on HeaderSize for what each field means and its
// verification status.
type Header struct {
	DstID   uint16
	SrcID   uint16
	DstPort uint8
	SrcPort uint8
	MsgType uint8
	Unused  uint8
}

// ParseHeader reads the 8-byte netio header from the front of datagram. It
// returns false if datagram is shorter than HeaderSize.
func ParseHeader(datagram []byte) (Header, bool) {
	if len(datagram) < HeaderSize {
		return Header{}, false
	}
	return Header{
		DstID:   binary.BigEndian.Uint16(datagram[offDstID:]),
		SrcID:   binary.BigEndian.Uint16(datagram[offSrcID:]),
		DstPort: datagram[offDstPort],
		SrcPort: datagram[offSrcPort],
		MsgType: datagram[offMsgType],
		Unused:  datagram[offUnused],
	}, true
}

// BuildHeader serializes h into an 8-byte netio header.
func BuildHeader(h Header) []byte {
	buf := make([]byte, HeaderSize)
	binary.BigEndian.PutUint16(buf[offDstID:], h.DstID)
	binary.BigEndian.PutUint16(buf[offSrcID:], h.SrcID)
	buf[offDstPort] = h.DstPort
	buf[offSrcPort] = h.SrcPort
	buf[offMsgType] = h.MsgType
	buf[offUnused] = h.Unused
	return buf
}

// Payload returns the bytes of datagram after the netio header (the raw
// ethernet frame), or nil if datagram is shorter than HeaderSize.
func Payload(datagram []byte) []byte {
	if len(datagram) < HeaderSize {
		return nil
	}
	return datagram[HeaderSize:]
}

// WithPayload serializes h followed by payload into one datagram, ready to
// write to either the unix netio socket or the UDP socket.
func WithPayload(h Header, payload []byte) []byte {
	buf := make([]byte, HeaderSize+len(payload))
	copy(buf, BuildHeader(h))
	copy(buf[HeaderSize:], payload)
	return buf
}
