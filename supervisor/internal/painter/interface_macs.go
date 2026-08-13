package painter

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

// InterfaceMAC is one IOS interface whose current hardware address was
// present in the command output.
type InterfaceMAC struct {
	Interface     string
	InterfaceNorm string
	MAC           string // lowercase colon-separated
}

var (
	// IOS interface headers are unindented. Keep the interface token as the
	// first capture and leave the operational state deliberately uninterpreted.
	interfaceMACHeaderRE = regexp.MustCompile(`(?i)^([A-Za-z][A-Za-z0-9.-]*(/[A-Za-z0-9.-]+)*)\s+is\s+.+$`)
	interfaceMACFieldRE  = regexp.MustCompile(`(?i)^\s*Hardware is .*,\s*address is\s+([0-9a-f.:-]+)(\s+\(bia\s+[0-9a-f.:-]+\))?.*$`)
)

type interfaceMACBlock struct {
	interfaceName string
	lines         []string
}

// splitInterfaceMACBlocks separates full or IOS-filtered `show interfaces`
// output before field parsing, so a partial hardware stanza can never leak
// its address into a neighboring interface.
func splitInterfaceMACBlocks(out string) []interfaceMACBlock {
	var blocks []interfaceMACBlock
	var current *interfaceMACBlock
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, "\r")
		if match := interfaceMACHeaderRE.FindStringSubmatch(line); len(match) > 1 {
			blocks = append(blocks, interfaceMACBlock{interfaceName: match[1]})
			current = &blocks[len(blocks)-1]
			continue
		}
		if current != nil {
			current.lines = append(current.lines, line)
		}
	}
	return blocks
}

// normalizeInterfaceMAC accepts IOS dotted notation and common colon/hyphen
// notation, then enforces a valid six-byte address.
func normalizeInterfaceMAC(raw string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	var compact string
	switch {
	case strings.Contains(s, "."):
		parts := strings.Split(s, ".")
		if len(parts) != 3 || len(parts[0]) != 4 || len(parts[1]) != 4 || len(parts[2]) != 4 {
			return "", false
		}
		compact = strings.Join(parts, "")
	case strings.Contains(s, ":") || strings.Contains(s, "-"):
		if strings.Contains(s, ":") && strings.Contains(s, "-") {
			return "", false
		}
		separator := ":"
		if strings.Contains(s, "-") {
			separator = "-"
		}
		parts := strings.Split(s, separator)
		if len(parts) != 6 {
			return "", false
		}
		for _, part := range parts {
			if len(part) != 2 {
				return "", false
			}
		}
		compact = strings.Join(parts, "")
	default:
		compact = s
	}
	if len(compact) != 12 {
		return "", false
	}
	mac, err := hex.DecodeString(compact)
	if err != nil || len(mac) != 6 {
		return "", false
	}
	allZero := true
	allBroadcast := true
	for _, b := range mac {
		if b != 0 {
			allZero = false
		}
		if b != 0xff {
			allBroadcast = false
		}
	}
	if allZero || allBroadcast {
		return "", false
	}
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5]), true
}

// ParseInterfaceMACs parses full or header/address-filtered `show interfaces`
// output from IOS/IOL and returns interfaces that contain a valid current
// "Hardware is ..., address is ..." value.
func ParseInterfaceMACs(out string) []InterfaceMAC {
	blocks := splitInterfaceMACBlocks(out)
	macs := make([]InterfaceMAC, 0, len(blocks))
	for _, block := range blocks {
		for _, line := range block.lines {
			match := interfaceMACFieldRE.FindStringSubmatch(line)
			if len(match) < 2 {
				continue
			}
			mac, ok := normalizeInterfaceMAC(match[1])
			if !ok {
				continue
			}
			macs = append(macs, InterfaceMAC{
				Interface:     block.interfaceName,
				InterfaceNorm: normIface(block.interfaceName),
				MAC:           mac,
			})
			break
		}
	}
	return macs
}
