package main

import (
	"net"
	"os"
)

func hasLabIface() bool {
	name := os.Getenv("IOLBOX_TOOL_IFACE")
	if name == "" {
		name = "eth1"
	}
	_, err := net.InterfaceByName(name)
	return err == nil
}
