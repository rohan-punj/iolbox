package main

import (
	"strings"
	"testing"
)

func testApp(t *testing.T) *App { t.Helper(); return NewApp(NewStore(t.TempDir() + "/options.json")) }

func TestCLIValidCoreGrammar(t *testing.T) {
	app := testApp(t)
	for _, line := range []string{"ip 10.0.0.1/24 10.0.0.254", "ip dhcp -r", "show ip", "arp show", "tcp connect invalid.invalid 9", "tcp listen 18080 -e", "tcp close 18080", "udp send 127.0.0.1 9 hello", "udp listen 18081 -e", "udp close 18081", "flow show", "flow start 127.0.0.1 1 1 -p udp -d 9", "flow stop", "save", "reset", "?", "? show ip", "help ping"} {
		if got := dispatchLine(app, line); strings.HasPrefix(got, "% Usage:") || strings.HasPrefix(got, `% Unknown`) {
			t.Fatalf("%q rejected: %q", line, got)
		}
	}
}

func TestCLIMalformedOutput(t *testing.T) {
	app := testApp(t)
	cases := map[string]string{
		"ip nope":           "% Usage: ip <addr>/<prefix> [<gateway>]",
		"ip dhcp -x":        "% Usage: ip dhcp [-r]",
		"show nope":         "% Usage: show ip",
		"ping 127.0.0.1 -c": "% Usage: ping <host> [-c <n>] [-i <ms>] [-s <bytes>] [-t <ttl>]",
		"trace":             "% Usage: trace <host> [-m <maxttl>] [-q <probes>]",
		"dns":               "% Usage: dns <name> [A|AAAA|CNAME|PTR] [@<server>]",
		"tcp nope":          "% Usage: tcp connect <host> <port> [-m <msg>] | tcp listen <port> [-e] | tcp close <port>",
		"udp nope":          "% Usage: udp send <host> <port> <msg> | udp listen <port> [-e] | udp close <port>",
		"flow nope":         "% Usage: flow start <dst> <pps> <bytes> [-p udp|tcp] [-d <port>] | flow stop [<id>] | flow show",
		"arp nope":          "% Usage: arp show | arp clear",
		"save now":          "% Usage: save",
		"reset now":         "% Usage: reset",
	}
	for line, want := range cases {
		if got := dispatchLine(app, line); got != want {
			t.Errorf("%q = %q, want %q", line, got, want)
		}
	}
	if got := dispatchLine(app, "wat"); got != `% Unknown command "wat". Type ? for a list.` {
		t.Fatalf("unknown output = %q", got)
	}
}

func TestCLIHelpIsComplete(t *testing.T) {
	help := helpText("")
	groups := map[string]bool{}
	for _, command := range cliCommands {
		if !strings.Contains(help, command.Name) || !strings.Contains(help, command.Description) {
			t.Errorf("help omits %q", command.Name)
		}
		groups[command.Group] = true
		if got := helpText(command.Name); !strings.Contains(got, command.Usage) || !strings.Contains(got, command.Description) {
			t.Errorf("topic help omits %q", command.Name)
		}
	}
	for _, group := range []string{"Addressing", "Diagnostics", "Services", "Config"} {
		if !groups[group] || !strings.Contains(help, group+":") {
			t.Errorf("help omits group %q", group)
		}
	}
	// Keep the dispatch surface and help table in lockstep, including the
	// multi-word show ip command.
	dispatchKeys := []string{"ip", "show ip", "ping", "trace", "save", "reset", "dns", "tcp", "udp", "flow", "arp"}
	for _, key := range dispatchKeys {
		found := false
		for _, command := range cliCommands {
			if command.Name == key {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("dispatcher command %q has no help entry", key)
		}
	}
}
