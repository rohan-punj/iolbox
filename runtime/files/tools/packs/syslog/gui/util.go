package main

import "net"

func hasLabIface() bool {
	name := "eth1"
	if configured := getenv("IOLBOX_TOOL_IFACE"); configured != "" {
		name = configured
	}
	_, err := net.InterfaceByName(name)
	return err == nil
}

func getenv(key string) string { return envLookup(key) }
