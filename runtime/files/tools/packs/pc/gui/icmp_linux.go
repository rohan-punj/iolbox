//go:build linux

package main

import (
	"net"
	"os"
	"syscall"
	"time"
)

type icmpConn struct {
	pc net.PacketConn
	fd int
}

func openICMP() (*icmpConn, error) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, syscall.IPPROTO_ICMP)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "netprobe-icmp")
	pc, err := net.FilePacketConn(file)
	_ = file.Close()
	if err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	return &icmpConn{pc: pc, fd: fd}, nil
}

func (c *icmpConn) WriteTo(b []byte, addr net.Addr) (int, error) { return c.pc.WriteTo(b, addr) }
func (c *icmpConn) ReadFrom(b []byte) (int, net.Addr, error)     { return c.pc.ReadFrom(b) }
func (c *icmpConn) SetTTL(ttl int) error {
	return syscall.SetsockoptInt(c.fd, syscall.IPPROTO_IP, syscall.IP_TTL, ttl)
}
func (c *icmpConn) SetReadDeadline(deadline time.Time) error { return c.pc.SetReadDeadline(deadline) }
func (c *icmpConn) Close() error                             { return c.pc.Close() }
