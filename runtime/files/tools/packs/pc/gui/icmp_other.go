//go:build !linux

package main

import (
	"net"
	"time"
)

type icmpConn struct{ conn *net.IPConn }

func openICMP() (*icmpConn, error) {
	conn, err := net.DialIP("ip4:icmp", nil, &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}
	return &icmpConn{conn: conn}, nil
}
func (c *icmpConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	return c.conn.WriteToIP(b, addr.(*net.IPAddr))
}
func (c *icmpConn) ReadFrom(b []byte) (int, net.Addr, error) { return c.conn.ReadFromIP(b) }
func (c *icmpConn) SetTTL(_ int) error                       { return nil }
func (c *icmpConn) SetDF(_ bool) error                       { return nil }
func (c *icmpConn) SetReadDeadline(deadline time.Time) error { return c.conn.SetReadDeadline(deadline) }
func (c *icmpConn) Close() error                             { return c.conn.Close() }
