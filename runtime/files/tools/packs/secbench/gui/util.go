package main

import (
	"net"
	"regexp"
	"strings"
)

type ifaceInfo struct {
	Name string
	IP   string // "" when the interface has no IPv4 yet
	Lab  bool   // lab-docker convention: eth0 = docker0 mgmt, eth1+ = lab
	Up   bool
}

// listIfaces returns up, non-loopback interface addresses. eth0 is always
// docker0 mgmt (its default route is deleted at node start); eth1+ is the
// lab data path attacks are locked to (see runner.go labIface).
func listIfaces() []ifaceInfo {
	var out []ifaceInfo
	ifs, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		var ips []string
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				ips = append(ips, ipnet.IP.String())
			}
		}
		lab := ifc.Name != "eth0"
		up := ifc.Flags&net.FlagUp != 0
		if len(ips) == 0 {
			out = append(out, ifaceInfo{Name: ifc.Name, IP: "", Lab: lab, Up: up})
			continue
		}
		for _, ip := range ips {
			out = append(out, ifaceInfo{Name: ifc.Name, IP: ip, Lab: lab, Up: up})
		}
	}
	return out
}

// deriveReconSubnet finds eth1's IPv4/mask and returns it as a CIDR subnet,
// used to prefill the ARP Scan "subnet" field on a fresh node.
func deriveReconSubnet() string {
	ifc, err := net.InterfaceByName(labIface)
	if err != nil {
		return ""
	}
	addrs, err := ifc.Addrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
			return ipnet.String() // e.g. "192.168.1.5/24" — good enough as a scan hint
		}
	}
	return ""
}

var hostLineRE = regexp.MustCompile(`\[HOST\].*?ip=(\S+)\s+mac=(\S+)`)

// ReconHost is one row parsed from arp_scan.py's "[HOST] ip=.. mac=.." lines.
type ReconHost struct {
	IP  string
	MAC string
}

// parseReconHosts extracts host rows from the arp_scan runner's log tail,
// de-duplicated by IP (last-seen wins), so other tabs can prefill target
// fields (a <datalist>) from what recon already found.
func parseReconHosts(lines []string) []ReconHost {
	seen := map[string]string{}
	var order []string
	for _, l := range lines {
		m := hostLineRE.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		if _, ok := seen[m[1]]; !ok {
			order = append(order, m[1])
		}
		seen[m[1]] = m[2]
	}
	out := make([]ReconHost, 0, len(order))
	for _, ip := range order {
		out = append(out, ReconHost{IP: ip, MAC: seen[ip]})
	}
	return out
}

// classify tags a status line as ok/bad/"" for colouring in the log view.
func classify(l string) string {
	if strings.HasPrefix(l, "[FATAL]") || strings.HasPrefix(l, "[ERROR]") {
		return "bad"
	}
	if strings.HasPrefix(l, "[OK]") || strings.HasPrefix(l, "[SENT]") || strings.HasPrefix(l, "[HOST]") {
		return "ok"
	}
	if strings.HasPrefix(l, "[WARN]") {
		return "warn"
	}
	return ""
}
