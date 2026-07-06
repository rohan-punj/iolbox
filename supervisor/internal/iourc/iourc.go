// Package iourc generates the Cisco IOU license file (~/.iourc) from the
// runtime's hostid and hostname.
//
// The algorithm below is the community-standard IOU keygen used by GNS3/EVE for
// years (commonly "CiscoIOUKeygen.py"). It contains no Cisco code: it is a small
// MD5 construction over two fixed pad constants and a key derived from the host
// identity. It is reimplemented here in Go from the public description.
//
// Assumption to verify during the P0 spike: a real IOL image must accept the
// key this produces for the runtime's actual hostid+hostname. See supervisor
// README "assumptions".
package iourc

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
)

// pad1 and pad2 are the fixed constants of the community IOU keygen.
var (
	pad1 = []byte{
		0x4b, 0x58, 0x21, 0x81, 0x56, 0x7b, 0x0d, 0xf3,
		0x21, 0x43, 0x9b, 0x7e, 0xac, 0x1d, 0xe6, 0x8a,
	}
	pad2 = func() []byte {
		b := make([]byte, 40)
		b[0] = 0x80
		return b
	}()
)

// Key computes the 16-hex-character IOU license key for a hostid (hex string,
// e.g. the 8-hex output of the `hostid` command) and a hostname.
//
//	key32 = int(hostid, 16) + sum(ord(c) for c in hostname)
//	license = md5(pad1 + pad2 + be32(key32) + pad1).hex()[:16]
func Key(hostid, hostname string) (string, error) {
	h, err := strconv.ParseUint(hostid, 16, 32)
	if err != nil {
		return "", fmt.Errorf("iourc: bad hostid %q: %w", hostid, err)
	}
	sum := uint32(h)
	for _, c := range []byte(hostname) {
		sum += uint32(c)
	}
	var key4 [4]byte
	binary.BigEndian.PutUint32(key4[:], sum)

	m := md5.New()
	m.Write(pad1)
	m.Write(pad2)
	m.Write(key4[:])
	m.Write(pad1)
	full := hex.EncodeToString(m.Sum(nil))
	return full[:16], nil
}

// File renders the full ~/.iourc file content for a hostid+hostname pair:
//
//	[license]
//	<hostname> = <key>;
func File(hostid, hostname string) (string, error) {
	key, err := Key(hostid, hostname)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("[license]\n%s = %s;\n", hostname, key), nil
}
