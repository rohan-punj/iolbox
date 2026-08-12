//go:build linux

package main

import (
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
)

type icmpConn struct {
	pc *net.UDPConn
}

func openICMP() (*icmpConn, error) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, syscall.IPPROTO_ICMP)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "netprobe-icmp")
	// net.FilePacketConn dup()s fd for its own runtime-managed descriptor, so
	// the original fd closed just below is no longer valid — SetTTL/SetDF
	// below must reach into the *live* descriptor via SyscallConn(), not
	// reuse this now-closed one.
	pc, err := net.FilePacketConn(file)
	_ = file.Close()
	if err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	udpConn, ok := pc.(*net.UDPConn)
	if !ok {
		_ = pc.Close()
		return nil, fmt.Errorf("unexpected packet conn type %T for a SOCK_DGRAM ping socket", pc)
	}
	return &icmpConn{pc: udpConn}, nil
}

func (c *icmpConn) setsockoptInt(level, opt, value int) error {
	raw, err := c.pc.SyscallConn()
	if err != nil {
		return err
	}
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		sockErr = syscall.SetsockoptInt(int(fd), level, opt, value)
	}); err != nil {
		return err
	}
	return sockErr
}

// WriteTo/ReadFrom present the same net.IPAddr-based contract as icmp_other.go's
// net.IPConn backend, even though the fd underneath is a SOCK_DGRAM "ping
// socket": Go's net.FilePacketConn wraps any SOCK_DGRAM fd as a *net.UDPConn
// (its family classifier keys off socket type, not IP protocol number), and
// *net.UDPConn.WriteTo/ReadFrom require *net.UDPAddr — passing the *net.IPAddr
// callers use fails UDPConn's internal type assertion and comes back as
// "write udp 0.0.0.0:0->host: invalid argument" (a zero-valued OpError, not a
// real network/permission failure). Convert at this boundary so ping.go and
// trace.go never need to know the underlying socket type.
func (c *icmpConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	ipAddr, ok := addr.(*net.IPAddr)
	if !ok {
		return 0, &net.AddrError{Err: "expected *net.IPAddr", Addr: addr.String()}
	}
	return c.pc.WriteTo(b, &net.UDPAddr{IP: ipAddr.IP})
}
func (c *icmpConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, addr, err := c.pc.ReadFrom(b)
	if udpAddr, ok := addr.(*net.UDPAddr); ok {
		return n, &net.IPAddr{IP: udpAddr.IP}, err
	}
	return n, addr, err
}
func (c *icmpConn) SetTTL(ttl int) error {
	return c.setsockoptInt(syscall.IPPROTO_IP, syscall.IP_TTL, ttl)
}

// SetDF toggles the IP "don't fragment" bit for packets this socket sends.
// IP_PMTUDISC_DO sets DF and reports back ICMP "fragmentation needed" as a
// write/read error instead of silently fragmenting; IP_PMTUDISC_WANT restores
// the kernel default (fragment as needed).
func (c *icmpConn) SetDF(df bool) error {
	mode := syscall.IP_PMTUDISC_WANT
	if df {
		mode = syscall.IP_PMTUDISC_DO
	}
	return c.setsockoptInt(syscall.IPPROTO_IP, syscall.IP_MTU_DISCOVER, mode)
}
func (c *icmpConn) SetReadDeadline(deadline time.Time) error { return c.pc.SetReadDeadline(deadline) }
func (c *icmpConn) Close() error                             { return c.pc.Close() }
