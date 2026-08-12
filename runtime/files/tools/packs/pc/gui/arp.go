package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func commandARP(args []string) string {
	if len(args) != 1 {
		return malformed("arp show | arp clear")
	}
	switch args[0] {
	case "show":
		out, err := exec.Command("ip", "neigh", "show", "dev", "eth1").CombinedOutput()
		if err != nil {
			return "% arp: " + err.Error()
		}
		if strings.TrimSpace(string(out)) == "" {
			return "ARP table empty."
		}
		return strings.TrimSpace(string(out))
	case "clear":
		if err := runIP("neigh", "flush", "dev", "eth1"); err != nil {
			return "% arp: " + err.Error()
		}
		return "ARP table flushed."
	default:
		return malformed("arp show | arp clear")
	}
}

var _ = fmt.Sprint
