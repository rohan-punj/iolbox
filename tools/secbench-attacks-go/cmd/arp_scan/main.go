package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/rohanpunj/iolbox/tools/secbench-attacks-go/internal/attackcommon"
)

const (
	etherTypeARP = 0x0806
	arpOpRequest = 1
	arpOpReply   = 2
)

var broadcastMAC = net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}

// buildARPRequest is the byte-level equivalent of
// Ether(src=..., dst=broadcast)/ARP(op=1, hwsrc=..., psrc=..., pdst=...).
func buildARPRequest(srcMAC net.HardwareAddr, srcIP, dstIP net.IP) ([]byte, error) {
	if len(srcMAC) != 6 {
		return nil, fmt.Errorf("source MAC must contain six octets")
	}
	srcIPv4 := srcIP.To4()
	if srcIPv4 == nil {
		return nil, fmt.Errorf("invalid source IPv4 address %q", srcIP)
	}
	dstIPv4 := dstIP.To4()
	if dstIPv4 == nil {
		return nil, fmt.Errorf("invalid destination IPv4 address %q", dstIP)
	}

	frame := make([]byte, 14+28)
	copy(frame[0:6], broadcastMAC)
	copy(frame[6:12], srcMAC)
	binary.BigEndian.PutUint16(frame[12:14], etherTypeARP)

	arp := frame[14:]
	binary.BigEndian.PutUint16(arp[0:2], 1)            // Ethernet hardware type
	binary.BigEndian.PutUint16(arp[2:4], 0x0800)       // IPv4 protocol type
	arp[4] = 6                                         // hardware address length
	arp[5] = 4                                         // protocol address length
	binary.BigEndian.PutUint16(arp[6:8], arpOpRequest) // who-has / request
	copy(arp[8:14], srcMAC)                            // hwsrc
	copy(arp[14:18], srcIPv4)                          // psrc
	// arp[18:24] is the all-zero hwdst from Scapy's explicit value.
	copy(arp[24:28], dstIPv4) // pdst
	return frame, nil
}

func run() int {
	fs, common := attackcommon.BaseParser("ARP-scan a subnet for live hosts")
	subnet := fs.String("subnet", "", "CIDR to scan, e.g. 192.168.1.0/24")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if err := attackcommon.EnforceLabIface(common.Iface); err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 2
	}

	srcMAC := attackcommon.InterfaceMAC(common.Iface)
	srcIP := attackcommon.InterfaceIPv4(common.Iface)
	if common.Selftest {
		frame, err := buildARPRequest(srcMAC, srcIP, net.IPv4(192, 168, 1, 1))
		if err != nil {
			attackcommon.Status("FATAL", fmt.Sprintf("selftest packet build failed: %v", err))
			return 1
		}
		attackcommon.SelftestOK("arp_scan", fmt.Sprintf("ARP request len=%d hwsrc=%s psrc=%s", len(frame), srcMAC, srcIP))
		return 0
	}
	if *subnet == "" {
		attackcommon.Status("FATAL", "no --subnet given")
		return 1
	}

	hosts, err := attackcommon.HostsInCIDR(*subnet)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}

	_, err = attackcommon.RunLoop(common.Count, common.Interval, func(n int) error {
		// Bind before sending so a fast responder cannot beat receiver setup.
		receiver, err := attackcommon.OpenRawReceiver(common.Iface)
		if err != nil {
			return err
		}
		defer receiver.Close()

		sender, err := attackcommon.OpenRawSender(common.Iface)
		if err != nil {
			return err
		}
		defer sender.Close()

		attackcommon.Status("INFO", fmt.Sprintf("ARP sweep #%d of %s on %s (hwsrc=%s psrc=%s)", n+1, *subnet, common.Iface, srcMAC, srcIP))
		for _, host := range hosts {
			frame, err := buildARPRequest(srcMAC, srcIP, host)
			if err != nil {
				return err
			}
			if err := sender.Send(frame); err != nil {
				return fmt.Errorf("send ARP request for %s: %w", host, err)
			}
		}

		seen := make(map[string]bool)
		deadline := time.Now().Add(3 * time.Second)
		if err := receiver.SetReadDeadline(deadline); err != nil {
			return err
		}
		for {
			frame, err := receiver.ReadFrame()
			if err != nil {
				// The receiver deadline is the normal end of a sweep.
				break
			}
			_, _, ethertype, _, payload := attackcommon.ParseEthernet(frame)
			if ethertype != etherTypeARP {
				continue
			}
			op, responderMAC, responderIP, _, ok := attackcommon.ParseARP(payload)
			if !ok || op != arpOpReply {
				continue
			}
			ipText := responderIP.String()
			if seen[ipText] {
				continue
			}
			seen[ipText] = true
			attackcommon.Status("HOST", fmt.Sprintf("ip=%s mac=%s", ipText, responderMAC))
		}
		attackcommon.Status("INFO", fmt.Sprintf("sweep #%d complete: %d host(s)", n+1, len(seen)))
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
