package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

func hasLabIface() bool {
	name := getenv("IOLBOX_TOOL_IFACE")
	if name == "" {
		name = "eth1"
	}
	_, err := net.InterfaceByName(name)
	return err == nil
}

func localIPv4(preferred string) net.IP {
	if ip := net.ParseIP(strings.TrimSpace(preferred)).To4(); ip != nil {
		return ip
	}
	name := getenv("IOLBOX_TOOL_IFACE")
	if name == "" {
		name = "eth1"
	}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil
	}
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip4 := ip.To4(); ip4 != nil {
			return ip4
		}
	}
	return nil
}

func uploadDir(optionsPath string) string {
	if optionsPath == "" {
		return filepath.Join(os.TempDir(), "iolbox-netsvc-uploads")
	}
	return filepath.Join(filepath.Dir(optionsPath), "uploads")
}

func formatErr(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprint(err)
}
