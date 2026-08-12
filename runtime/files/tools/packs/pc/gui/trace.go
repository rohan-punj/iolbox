package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"
)

func traceHost(host string, maxHops, probes int) string {
	ip, err := net.ResolveIPAddr("ip4", host)
	if err != nil {
		return "% trace: " + err.Error()
	}
	conn, err := openICMP()
	if err != nil {
		return "% trace: " + err.Error()
	}
	defer conn.Close()
	lines := []string{fmt.Sprintf("Tracing route to %s (%s), max %d hops, %d probes, method=ICMP datagram", host, ip.IP, maxHops, probes)}
	for ttl := 1; ttl <= maxHops; ttl++ {
		answered := false
		for probe := 0; probe < probes; probe++ {
			_ = conn.SetTTL(ttl)
			started := time.Now()
			_ = conn.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
			packet := []byte{8, 0, 0, 0, 0, 0, byte(ttl >> 8), byte(ttl)}
			binary.BigEndian.PutUint16(packet[2:4], icmpChecksum(packet))
			if _, err := conn.WriteTo(packet, ip); err != nil {
				continue
			}
			buf := make([]byte, 1500)
			_, _, err := conn.ReadFrom(buf)
			if err == nil {
				lines = append(lines, fmt.Sprintf("%2d  %s  %.1f ms", ttl, ip.IP, float64(time.Since(started).Microseconds())/1000))
				answered = true
				break
			}
		}
		if !answered {
			lines = append(lines, fmt.Sprintf("%2d  *", ttl))
		}
		if answered {
			break
		}
	}
	return strings.Join(lines, "\n")
}
