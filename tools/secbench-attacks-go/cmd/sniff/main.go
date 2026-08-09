package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rohanpunj/iolbox/tools/secbench-attacks-go/internal/attackcommon"
)

type talker struct {
	mac    string
	frames int
}

func isReceiveTimeout(err error) bool {
	return errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, os.ErrDeadlineExceeded)
}

func formatVLANs(vlanSet map[uint16]struct{}) string {
	if len(vlanSet) == 0 {
		return "(untagged only)"
	}

	vlans := make([]int, 0, len(vlanSet))
	for vlan := range vlanSet {
		vlans = append(vlans, int(vlan))
	}
	sort.Ints(vlans)
	parts := make([]string, len(vlans))
	for i, vlan := range vlans {
		parts[i] = strconv.Itoa(vlan)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func sniffRound(iface string, duration, round int) error {
	attackcommon.Status("INFO", fmt.Sprintf("round #%d: sniffing %s for %ds", round+1, iface, duration))

	receiver, err := attackcommon.OpenRawReceiver(iface)
	if err != nil {
		return err
	}
	defer receiver.Close()

	deadline := time.Now().Add(time.Duration(duration) * time.Second)
	macCounts := make(map[string]int)
	firstSeen := make([]string, 0)
	vlanSet := make(map[uint16]struct{})

	for {
		if !time.Now().Before(deadline) {
			break
		}
		if err := receiver.SetReadDeadline(deadline); err != nil {
			return err
		}
		frame, err := receiver.ReadFrame()
		if err != nil {
			if isReceiveTimeout(err) {
				break
			}
			return err
		}

		_, srcMAC, _, vlanTags, _ := attackcommon.ParseEthernet(frame)
		if len(srcMAC) != 6 {
			continue
		}
		mac := srcMAC.String()
		if _, exists := macCounts[mac]; !exists {
			firstSeen = append(firstSeen, mac)
		}
		macCounts[mac]++
		for _, vlan := range vlanTags {
			vlanSet[vlan] = struct{}{}
		}
	}

	candidates := make([]talker, 0, len(firstSeen))
	for _, mac := range firstSeen {
		candidates = append(candidates, talker{mac: mac, frames: macCounts[mac]})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].frames > candidates[j].frames
	})
	if len(candidates) > 10 {
		candidates = candidates[:10]
	}
	for _, candidate := range candidates {
		attackcommon.Status("TALKER", fmt.Sprintf("mac=%s frames=%d", candidate.mac, candidate.frames))
	}
	attackcommon.Status("INFO", fmt.Sprintf("round #%d: %d unique MAC(s), VLANs seen: %s", round+1, len(macCounts), formatVLANs(vlanSet)))
	return nil
}

func run() int {
	fs, common := attackcommon.BaseParser("Passively sniff eth1 and summarize what's on the wire")
	duration := fs.Int("duration", 20, "seconds to sniff per round")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if err := attackcommon.EnforceLabIface(common.Iface); err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 2
	}

	if common.Selftest {
		attackcommon.SelftestOK("sniff", "Ether/Dot1Q layers available, no packets sent")
		return 0
	}

	_, err := attackcommon.RunLoop(common.Count, common.Interval, func(n int) error {
		return sniffRound(common.Iface, *duration, n)
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
