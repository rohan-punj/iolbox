package main

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"fmt"
	"os"
	"time"

	"github.com/rohanpunj/iolbox/tools/secbench-attacks-go/internal/attackcommon"
)

const (
	dhcpServerPort = 67
	dhcpClientPort = 68
	etherTypeIPv4  = 0x0800
)

func randomXID() (uint32, error) {
	var bytes [4]byte
	if _, err := cryptorand.Read(bytes[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(bytes[:]), nil
}

func randomDHCPChaddr() ([6]byte, error) {
	var chaddr [6]byte
	chaddr[0] = 0x02
	if _, err := cryptorand.Read(chaddr[1:]); err != nil {
		return chaddr, err
	}
	return chaddr, nil
}

func run() int {
	fs, common := attackcommon.BaseParser("Broadcast DHCPDISCOVER and report responding servers")
	duration := fs.Int("duration", 10, "seconds to listen for OFFERs")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if err := attackcommon.EnforceLabIface(common.Iface); err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 2
	}

	srcMAC := attackcommon.InterfaceMAC(common.Iface)
	if common.Selftest {
		chaddr, err := randomDHCPChaddr()
		if err != nil {
			attackcommon.Status("FATAL", fmt.Sprintf("selftest packet build failed: %v", err))
			return 1
		}
		frame := attackcommon.BuildDHCPDiscover(1234, chaddr, srcMAC)
		attackcommon.SelftestOK("dhcp_discover", fmt.Sprintf("DHCPDISCOVER len=%d", len(frame)))
		return 0
	}

	_, err := attackcommon.RunLoop(common.Count, common.Interval, func(n int) error {
		xid, err := randomXID()
		if err != nil {
			return fmt.Errorf("generate DHCP transaction ID: %w", err)
		}
		if xid == 0 {
			xid = 1
		}
		chaddr, err := randomDHCPChaddr()
		if err != nil {
			return fmt.Errorf("generate DHCP client address: %w", err)
		}

		// Bind before sending so a low-latency rogue server cannot beat receiver setup.
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

		attackcommon.Status("INFO", fmt.Sprintf("round #%d: broadcasting DHCPDISCOVER (xid=%d) on %s", n+1, xid, common.Iface))
		if err := sender.Send(attackcommon.BuildDHCPDiscover(xid, chaddr, srcMAC)); err != nil {
			return fmt.Errorf("send DHCPDISCOVER: %w", err)
		}

		seen := make(map[string]bool)
		deadline := time.Now().Add(time.Duration(*duration) * time.Second)
		if err := receiver.SetReadDeadline(deadline); err != nil {
			return err
		}
		for {
			frame, err := receiver.ReadFrame()
			if err != nil {
				// The receiver deadline is the normal end of the listen window.
				break
			}
			_, _, ethertype, _, payload := attackcommon.ParseEthernet(frame)
			if ethertype != etherTypeIPv4 {
				continue
			}
			serverIP, _, sport, dport, udpPayload, ok := attackcommon.ParseIPv4UDP(payload)
			if !ok || sport != dhcpServerPort || dport != dhcpClientPort {
				continue
			}
			_, _, offeredIP, msgType, ok := attackcommon.ParseBOOTP(udpPayload)
			if !ok || msgType != 2 {
				continue
			}
			serverText := serverIP.String()
			if seen[serverText] {
				continue
			}
			seen[serverText] = true
			attackcommon.Status("SERVER", fmt.Sprintf("dhcp-server=%s offered=%s", serverText, offeredIP))
		}
		attackcommon.Status("INFO", fmt.Sprintf("round #%d complete: %d server(s) answered", n+1, len(seen)))
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
