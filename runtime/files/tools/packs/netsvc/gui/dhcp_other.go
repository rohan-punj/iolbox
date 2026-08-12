//go:build !linux

package main

import "net"

func prepareDHCPConn(conn *net.UDPConn) error { return nil }
