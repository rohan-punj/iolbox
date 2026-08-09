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

func getenv(key string) string {
	// Kept as a small seam for tests and to make the interface check easy to
	// reuse in the dashboard without pulling environment handling into it.
	return envLookup(key)
}
