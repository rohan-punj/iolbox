package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"

	"github.com/rohanpunj/iolbox/tools/secbench-attacks-go/internal/attackcommon"
)

func buildFrame(spoofMAC net.HardwareAddr, srcIP, dstIP net.IP) []byte {
	frame := make([]byte, 14)
	copy(frame[0:6], net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	copy(frame[6:12], spoofMAC)
	binary.BigEndian.PutUint16(frame[12:14], 0x0800)
	ip := attackcommon.BuildIPv4(srcIP, dstIP, 1, attackcommon.BuildICMPEcho(0, 0, nil))
	return append(frame, ip...)
}

func run() int {
	fs, common := attackcommon.BaseParser("Send frames from a forged source MAC")
	spoofMACText := fs.String("spoof_mac", "02:00:00:aa:bb:cc", "source MAC to forge")
	targetIPText := fs.String("target_ip", "", "destination IP (optional; broadcast if blank)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if err := attackcommon.EnforceLabIface(common.Iface); err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 2
	}

	spoofMAC, err := attackcommon.ParseMAC(*spoofMACText)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	dstIP := net.IPv4bcast
	if *targetIPText != "" {
		dstIP = net.ParseIP(*targetIPText).To4()
		if dstIP == nil {
			attackcommon.Status("FATAL", fmt.Sprintf("invalid IPv4 address %q", *targetIPText))
			return 1
		}
	}
	srcIP := attackcommon.InterfaceIPv4(common.Iface)
	frame := buildFrame(spoofMAC, srcIP, dstIP)
	if common.Selftest {
		attackcommon.SelftestOK("mac_spoof", fmt.Sprintf("forged-src frame len=%d src=%s", len(frame), *spoofMACText))
		return 0
	}

	sender, err := attackcommon.OpenRawSender(common.Iface)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	defer sender.Close()

	_, err = attackcommon.RunLoop(common.Count, common.Interval, func(n int) error {
		if err := sender.Send(buildFrame(spoofMAC, srcIP, dstIP)); err != nil {
			return fmt.Errorf("send forged-src frame: %w", err)
		}
		attackcommon.Status("SENT", fmt.Sprintf("frame #%d src=%s dst=%s", n+1, *spoofMACText, func() string {
			if *targetIPText == "" {
				return "255.255.255.255"
			}
			return *targetIPText
		}()))
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
