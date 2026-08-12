//go:build linux

package main

import (
	"net"
	"syscall"
)

// DHCP clients commonly broadcast DISCOVER/OFFER before they have an address.
// The server therefore enables SO_BROADCAST on its dedicated UDP socket; this
// is the only platform-specific part of the otherwise stdlib DHCP server.
func prepareDHCPConn(conn *net.UDPConn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		controlErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
	}); err != nil {
		return err
	}
	return controlErr
}
