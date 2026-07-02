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

// HeaderSize is the number of bytes the netio/iouyap framing prepends before
// the raw ethernet frame on every datagram, in both directions (IOL->iouyap
// and iouyap->IOL). It matches relay.IOLHeaderSize exactly: both packages
// frame the SAME datagram, just on opposite sides of the netio<->UDP hop, so
// the header must round-trip unchanged in size. This package does not import
// internal/relay (to avoid a dependency between sibling internal packages);
// keep this constant in sync with relay.IOLHeaderSize if either changes.
//
// ASSUMPTION (P0-VERIFY): the classic iouyap/IOU netio header is 8 bytes,
// big-endian, laid out as:
//
//	offset 0: dst_ids  uint16  destination "bridge" id (peer's netio channel id)
//	offset 2: src_ids  uint16  source "bridge" id (this side's netio channel id)
//	offset 4: dst_port uint8   destination port index within the peer's node
//	offset 5: src_port uint8   source port index within this node
//	offset 6: msg_type uint8   0 = data frame, other values reserved (keepalive/ctl)
//	offset 7: unused   uint8   padding/channel byte, historically unused by iouyap
//
// This is the field layout the original iouyap.c / dynamips netio_filter code
// uses; we adopt it here because it is the only documented framing that real
// IOL images are known to speak. It has NOT yet been confirmed against actual
// wire captures of an IOL process talking to a unix netio socket (P0 step 7
// exercises the UDP relay's pcapng tee, not this bridge directly). Confirm by
// capturing the unix-domain datagrams IOL sends to iouyap's socket and diffing
// against this layout; adjust the field offsets/constants here (and the
// mirrored relay.IOLHeaderSize) if real IOL differs.
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

// MsgType values for offMsgType. Only msgTypeData is currently produced or
// expected; other values are reserved per the ASSUMPTION above and are
// preserved (not interpreted) when relaying.
const (
	// MsgTypeData marks an ordinary ethernet-frame payload.
	MsgTypeData uint8 = 0
)

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
