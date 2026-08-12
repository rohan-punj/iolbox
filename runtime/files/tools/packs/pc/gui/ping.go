package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"
)

func icmpChecksum(data []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}
	if len(data)%2 != 0 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func pingHost(host string, count, intervalMS, size, ttl int) string {
	ip, err := net.ResolveIPAddr("ip4", host)
	if err != nil {
		return "% ping: " + err.Error()
	}
	conn, err := openICMP()
	if err != nil {
		return "% ping: " + err.Error()
	}
	defer conn.Close()
	lines := []string{fmt.Sprintf("PING %s (%s)", host, ip.IP)}
	received := 0
	var min, max, total time.Duration
	for seq := 0; seq < count; seq++ {
		payload := make([]byte, size)
		if len(payload) >= 8 {
			binary.BigEndian.PutUint64(payload, uint64(time.Now().UnixNano()))
		}
		packet := append([]byte{8, 0, 0, 0, 0, 0, byte(seq >> 8), byte(seq)}, payload...)
		binary.BigEndian.PutUint16(packet[2:4], icmpChecksum(packet))
		started := time.Now()
		_ = conn.SetTTL(ttl)
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, err := conn.WriteTo(packet, ip); err != nil {
			lines = append(lines, "% "+err.Error())
			continue
		}
		buf := make([]byte, 1500)
		n, _, err := conn.ReadFrom(buf)
		if err == nil && n >= 8 {
			rtt := time.Since(started)
			received++
			total += rtt
			if min == 0 || rtt < min {
				min = rtt
			}
			if rtt > max {
				max = rtt
			}
			lines = append(lines, fmt.Sprintf("Reply from %s: time=%.1fms", ip.IP, float64(rtt.Microseconds())/1000))
		} else {
			lines = append(lines, "Request timeout")
		}
		if seq+1 < count && intervalMS > 0 {
			time.Sleep(time.Duration(intervalMS) * time.Millisecond)
		}
	}
	if received > 0 {
		lines = append(lines, fmt.Sprintf("%d packets transmitted, %d received, loss %.0f%%, min/avg/max %.1f/%.1f/%.1f ms", count, received, float64(count-received)*100/float64(count), float64(min.Microseconds())/1000, float64(total.Microseconds())/1000/float64(received), float64(max.Microseconds())/1000))
	} else {
		lines = append(lines, fmt.Sprintf("%d packets transmitted, 0 received, loss 100%%", count))
	}
	return strings.Join(lines, "\n")
}
