package main

type cliCommand struct {
	Group       string
	Name        string
	Usage       string
	Description string
}

var cliCommands = []cliCommand{
	{Group: "Addressing", Name: "ip", Usage: "ip <addr>/<prefix> [<gateway>] | ip dhcp [-r]", Description: "Configure a static IPv4 address or request DHCP."},
	{Group: "Addressing", Name: "show ip", Usage: "show ip", Description: "Show address, gateway, MAC, MTU, DHCP, and lease details."},
	{Group: "Diagnostics", Name: "ping", Usage: "ping <host> [-c <n>] [-i <ms>] [-s <bytes>] [-t <ttl>] [-D]", Description: "Send ICMP echo requests. -s sets payload size in bytes, -D sets the don't-fragment bit."},
	{Group: "Diagnostics", Name: "trace", Usage: "trace <host> [-m <maxttl>] [-q <probes>]", Description: "Trace using ICMP probes and report the probe method."},
	{Group: "Diagnostics", Name: "arp", Usage: "arp show | arp clear", Description: "Inspect or clear the neighbour table."},
	{Group: "Services", Name: "dns", Usage: "dns <name> [A|AAAA|CNAME|PTR] [@<server>]", Description: "Query a DNS record."},
	{Group: "Services", Name: "tcp", Usage: "tcp connect <host> <port> [-m <msg>] | tcp listen <port> [-e] | tcp close <port>", Description: "Connect or listen over TCP."},
	{Group: "Services", Name: "udp", Usage: "udp send <host> <port> <msg> | udp listen <port> [-e] | udp close <port>", Description: "Send or listen over UDP."},
	{Group: "Services", Name: "flow", Usage: "flow start <dst> <pps> <bytes> [-p udp|tcp] [-d <port>] | flow stop [<id>] | flow show", Description: "Generate bounded TCP or UDP traffic."},
	{Group: "Config", Name: "save", Usage: "save", Description: "Persist PC state to this node."},
	{Group: "Config", Name: "reset", Usage: "reset", Description: "Clear runtime addressing, listeners, and flows."},
}

func helpText(topic string) string {
	if topic != "" && topic != "?" && topic != "help" {
		for _, command := range cliCommands {
			if command.Name == topic {
				return command.Usage + " — " + command.Description
			}
		}
		return `% Unknown command "` + topic + `". Type ? for a list.`
	}
	out := "Commands:"
	group := ""
	for _, command := range cliCommands {
		if command.Group != group {
			group = command.Group
			out += "\n" + group + ":"
		}
		out += "\n  " + command.Usage + " — " + command.Description
	}
	return out
}
