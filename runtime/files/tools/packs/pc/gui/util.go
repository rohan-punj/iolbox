package main

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

func hasLabIface() bool {
	iface, err := net.InterfaceByName("eth1")
	return err == nil && iface.Flags&net.FlagUp != 0
}

func runIP(args ...string) error {
	cmd := exec.Command("ip", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ip %s: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func printableASCII(s string) bool {
	for _, b := range []byte(s) {
		if b < 0x20 || b > 0x7e {
			return false
		}
	}
	return true
}

func parseIPv4CIDR(value string) (string, int, error) {
	ip, network, err := net.ParseCIDR(value)
	if err != nil || ip.To4() == nil || network == nil {
		return "", 0, fmt.Errorf("expected IPv4 address/prefix")
	}
	prefix, bits := network.Mask.Size()
	if bits != 32 || prefix < 1 || prefix > 32 {
		return "", 0, fmt.Errorf("prefix must be 1..32")
	}
	return ip.To4().String(), prefix, nil
}

func parsePort(s string) (int, error) {
	p, err := strconv.Atoi(s)
	if err != nil || p < 1 || p > 65535 {
		return 0, fmt.Errorf("port must be 1..65535")
	}
	return p, nil
}
