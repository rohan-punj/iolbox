package main

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
)

func dnsQuery(name, recordType, server string) string {
	var resolver net.Resolver
	if server != "" {
		resolver = net.Resolver{PreferGo: true, Dial: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("udp", net.JoinHostPort(server, "53"))
		}}
	}
	ctx := context.Background()
	var addrs []string
	var err error
	switch recordType {
	case "A":
		var ips []net.IP
		ips, err = resolver.LookupIP(ctx, "ip4", name)
		for _, ip := range ips {
			addrs = append(addrs, ip.String())
		}
	case "AAAA":
		var ips []net.IP
		ips, err = resolver.LookupIP(ctx, "ip6", name)
		for _, ip := range ips {
			addrs = append(addrs, ip.String())
		}
	case "CNAME":
		var cname string
		cname, err = resolver.LookupCNAME(ctx, name)
		if cname != "" {
			addrs = []string{cname}
		}
	case "PTR":
		names, lookupErr := resolver.LookupAddr(ctx, name)
		err = lookupErr
		addrs = names
	default:
		err = fmt.Errorf("unsupported record type %s", recordType)
	}
	if err != nil {
		return "% dns: " + err.Error()
	}
	sort.Strings(addrs)
	return fmt.Sprintf("%s %s: %s", recordType, name, strings.Join(addrs, ", "))
}
