// Package nvram encodes and decodes IOL NVRAM images carrying an IOS
// startup-config, matching the GNS3 iou_import/iou_export codec.
//
// NVRAM layout (all multi-byte fields big-endian):
//
//	Startup-config header (36 bytes):
//	  off 0  uint16  magic          0xABCD
//	  off 2  uint16  format         1 (raw/uncompressed) | 2 (LZC, not produced here)
//	  off 4  uint16  checksum       ones-complement 16-bit sum over the section
//	  off 6  uint16  ios            IOS version hint (e.g. 0x0F04 = 15.4)
//	  off 8  uint32  startAddr      BASE_ADDRESS + 36
//	  off 12 uint32  endAddr        BASE_ADDRESS + 36 + len(config)
//	  off 16 uint32  length         len(config)
//	  off 20..35     zero
//	  then <length> bytes of startup-config text
//	  then padding to a 4-byte boundary
//	Private-config header (16 bytes, optional):
//	  off 0  uint16  magic          0xFEDC
//	  off 2  uint16  format         1
//	  off 4  uint32  startAddr
//	  off 8  uint32  endAddr
//	  off 12 uint32  length
//	  then <length> bytes of private config
//
// The checksum is computed with the checksum field zeroed, summing 16-bit
// big-endian words over the whole NVRAM section, folding carries, then XOR
// 0xFFFF (standard internet-checksum ones complement).
//
// Assumptions to verify during the P0 spike (see supervisor README): the exact
// BASE_ADDRESS, whether a real IOL rejects a zero private-config section, and
// the IOS version field's tolerance. These are named constants below.
package nvram

import (
	"encoding/binary"
	"fmt"
)

const (
	// StartupHeaderLen is the fixed startup-config header size.
	StartupHeaderLen = 36
	// PrivateHeaderLen is the fixed private-config header size.
	PrivateHeaderLen = 16

	magicStartup = 0xABCD
	magicPrivate = 0xFEDC
	formatRaw    = 1
	formatLZC    = 2

	// BaseAddress is the NVRAM base address used by the GNS3 codec. IOL only
	// uses the addresses for internal bookkeeping; the value is fixed here.
	BaseAddress = 0x10000000

	// DefaultIOSVersion is a benign IOS version hint (15.4). IOL tolerates a
	// wide range; P0 must confirm no image is picky.
	DefaultIOSVersion = 0x0F04
)

// Options tunes Encode. Zero value is fine (raw format, default IOS version, no
// private config, NVRAM sized to fit).
type Options struct {
	// IOSVersion sets the header hint; 0 uses DefaultIOSVersion.
	IOSVersion uint16
	// Private is optional private-config text appended after the startup config.
	Private string
	// Size, if > 0, pads the whole NVRAM image out to this many bytes (IOL
	// nvram files are usually a fixed size, e.g. 8/16/64 KiB). If the encoded
	// content exceeds Size, Size is ignored.
	Size int
}

// Encode builds an NVRAM image embedding config as the startup-config.
func Encode(config string, opts Options) ([]byte, error) {
	ios := opts.IOSVersion
	if ios == 0 {
		ios = DefaultIOSVersion
	}

	startup := []byte(config)
	buf := make([]byte, StartupHeaderLen+len(startup))
	binary.BigEndian.PutUint16(buf[0:], magicStartup)
	binary.BigEndian.PutUint16(buf[2:], formatRaw)
	// checksum (off 4) filled in after the section is complete
	binary.BigEndian.PutUint16(buf[6:], ios)
	binary.BigEndian.PutUint32(buf[8:], BaseAddress+StartupHeaderLen)
	binary.BigEndian.PutUint32(buf[12:], BaseAddress+StartupHeaderLen+uint32(len(startup)))
	binary.BigEndian.PutUint32(buf[16:], uint32(len(startup)))
	copy(buf[StartupHeaderLen:], startup)

	// Pad the startup section to a 4-byte boundary before any private header.
	buf = pad4(buf)

	// Optional private-config section.
	if opts.Private != "" {
		priv := []byte(opts.Private)
		ph := make([]byte, PrivateHeaderLen+len(priv))
		binary.BigEndian.PutUint16(ph[0:], magicPrivate)
		binary.BigEndian.PutUint16(ph[2:], formatRaw)
		binary.BigEndian.PutUint32(ph[4:], BaseAddress+StartupHeaderLen)
		binary.BigEndian.PutUint32(ph[8:], BaseAddress+StartupHeaderLen+uint32(len(priv)))
		binary.BigEndian.PutUint32(ph[12:], uint32(len(priv)))
		copy(ph[PrivateHeaderLen:], priv)
		buf = append(buf, ph...)
	}

	// Checksum covers the startup header + startup text (the section whose
	// header holds the checksum field). Compute with the field zeroed.
	setChecksum(buf, 0, StartupHeaderLen+len(startup))

	// Optionally pad the whole image out to a fixed size.
	if opts.Size > len(buf) {
		padded := make([]byte, opts.Size)
		copy(padded, buf)
		buf = padded
	}
	return buf, nil
}

// Decode extracts the startup-config text from an NVRAM image.
func Decode(nv []byte) (string, error) {
	if len(nv) < StartupHeaderLen {
		return "", fmt.Errorf("nvram: too short (%d bytes)", len(nv))
	}
	if binary.BigEndian.Uint16(nv[0:]) != magicStartup {
		return "", fmt.Errorf("nvram: bad startup magic 0x%04X", binary.BigEndian.Uint16(nv[0:]))
	}
	format := binary.BigEndian.Uint16(nv[2:])
	if format != formatRaw {
		if format == formatLZC {
			return "", fmt.Errorf("nvram: LZC-compressed config not supported")
		}
		return "", fmt.Errorf("nvram: unknown format %d", format)
	}
	length := binary.BigEndian.Uint32(nv[16:])
	start := StartupHeaderLen
	end := start + int(length)
	if end > len(nv) || end < start {
		return "", fmt.Errorf("nvram: config length %d overflows image (%d bytes)", length, len(nv))
	}
	return string(nv[start:end]), nil
}

// pad4 grows b so its length is a multiple of 4, filling with zeros.
func pad4(b []byte) []byte {
	if r := len(b) % 4; r != 0 {
		b = append(b, make([]byte, 4-r)...)
	}
	return b
}

// setChecksum zeroes the checksum field at start+4, computes the internet
// checksum over [start,end), and writes it back.
func setChecksum(data []byte, start, end int) {
	binary.BigEndian.PutUint16(data[start+4:], 0)
	var sum uint32
	i := start
	for i < end-1 {
		sum += uint32(binary.BigEndian.Uint16(data[i:]))
		i += 2
	}
	if i < end {
		sum += uint32(data[i]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	binary.BigEndian.PutUint16(data[start+4:], uint16(sum^0xFFFF))
}
