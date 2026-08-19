//go:build linux

package egress

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"net"
	"os"
	"strings"
)

// Detect classifies the runtime's egress by looking for the QEMU user-mode
// slirp signature (default route via 10.0.2.2, or a primary address in
// 10.0.2.0/24). It reads the routing table from /proc/net/route and interface
// addresses via net.Interfaces() — stdlib only, no external command. It is
// best-effort and never panics: on any read/parse error the inputs are simply
// empty, so classify falls back to Routed (the permissive default).
func Detect() string {
	return classify(defaultRouteGateway(), primaryAddrs())
}

// defaultRouteGateway returns the IPv4 gateway of the default route from
// /proc/net/route, or nil if there is none / it can't be read. The Gateway
// column is a little-endian hex-encoded IPv4 address.
func defaultRouteGateway() net.IP {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan() // header row
	for sc.Scan() {
		// Iface Destination Gateway Flags RefCnt Use Metric Mask ...
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		if fields[1] != "00000000" { // not the default route
			continue
		}
		if ip := parseHexLEIP(fields[2]); ip != nil {
			return ip
		}
	}
	return nil
}

// parseHexLEIP decodes an 8-hex-char little-endian IPv4 address as it appears in
// /proc/net/route (e.g. "0202000A" -> 10.0.2.2). Returns nil on any malformity.
func parseHexLEIP(s string) net.IP {
	if len(s) != 8 {
		return nil
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil
	}
	v := binary.LittleEndian.Uint32(b)
	return net.IPv4(byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// primaryAddrs returns the IPv4 unicast addresses of every up, non-loopback
// interface. Slirp only ever hands out one address, so scanning all real
// interfaces is both cheap and sufficient to spot 10.0.2.15.
func primaryAddrs() []net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip4 := ip.To4(); ip4 != nil {
				out = append(out, ip4)
			}
		}
	}
	return out
}
