package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

const cliPrompt = "PC> "

func malformed(usage string) string { return "% Usage: " + usage }

func handleCLIConnection(conn net.Conn, app *App) {
	defer conn.Close()
	_, _ = io.WriteString(conn, cliPrompt)
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 1024), 4096)
	for scanner.Scan() {
		_, _ = io.WriteString(conn, dispatchLine(app, scanner.Text())+"\r\n"+cliPrompt)
	}
}

func dispatchLine(app *App, line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	if fields[0] == "?" || fields[0] == "help" {
		if len(fields) > 1 {
			return helpText(strings.Join(fields[1:], " "))
		}
		return helpText("")
	}
	if fields[0] != "save" && fields[0] != "reset" {
		app.state.Remember(strings.TrimSpace(line))
	}
	switch fields[0] {
	case "ip":
		return commandIP(app, fields[1:])
	case "show":
		if len(fields) == 2 && fields[1] == "ip" {
			return app.state.ShowIP()
		}
		return malformed("show ip")
	case "ping":
		return commandPing(fields[1:])
	case "trace":
		return commandTrace(fields[1:])
	case "dns":
		return commandDNS(fields[1:])
	case "tcp":
		return commandTCP(app, fields[1:])
	case "udp":
		return commandUDP(app, fields[1:])
	case "flow":
		return commandFlow(app, fields[1:])
	case "arp":
		return commandARP(fields[1:])
	case "save":
		if len(fields) != 1 {
			return malformed("save")
		}
		if err := app.state.Save(); err != nil {
			return "% Save failed: " + err.Error()
		}
		return "Saved to this node. Use Lab > Save to store it in the lab file."
	case "reset":
		if len(fields) != 1 {
			return malformed("reset")
		}
		app.state.Reset()
		app.flows.StopAll()
		app.socks.CloseAll()
		return "Runtime address, listeners, flows, and lease reset."
	default:
		return `% Unknown command "` + fields[0] + `". Type ? for a list.`
	}
}

func commandIP(app *App, args []string) string {
	if len(args) >= 1 && args[0] == "dhcp" {
		release := false
		for _, arg := range args[1:] {
			if arg != "-r" || release {
				return malformed("ip dhcp [-r]")
			}
			release = true
		}
		cfg := app.store.Snapshot()
		cfg.PC.DHCP = !release
		if err := app.store.Save(cfg); err != nil {
			return "% Save failed: " + err.Error()
		}
		if release {
			_ = app.state.ClearAddress()
			app.state.ClearLease()
			return "DHCP lease released."
		}
		return runDHCP(app)
	}
	if len(args) < 1 || len(args) > 2 {
		return malformed("ip <addr>/<prefix> [<gateway>]")
	}
	ip, prefix, err := parseIPv4CIDR(args[0])
	if err != nil {
		return malformed("ip <addr>/<prefix> [<gateway>]")
	}
	gateway := ""
	if len(args) == 2 {
		gateway = args[1]
	}
	if err := app.state.SetAddress(ip, prefix, gateway); err != nil {
		return "% " + err.Error()
	}
	cfg := app.store.Snapshot()
	cfg.PC.DHCP = false
	if err := app.store.Save(cfg); err != nil {
		return "% Save failed: " + err.Error()
	}
	return "Address configured on eth1: " + ip + "/" + strconv.Itoa(prefix)
}

func commandPing(args []string) string {
	host, count, interval, size, ttl, ok := parsePingArgs(args)
	if !ok {
		return malformed("ping <host> [-c <n>] [-i <ms>] [-s <bytes>] [-t <ttl>]")
	}
	return pingHost(host, count, interval, size, ttl)
}

func commandTrace(args []string) string {
	if len(args) < 1 {
		return malformed("trace <host> [-m <maxttl>] [-q <probes>]")
	}
	hops, probes := 30, 3
	host := args[0]
	for i := 1; i < len(args); i += 2 {
		if i+1 >= len(args) || (args[i] != "-m" && args[i] != "-q") {
			return malformed("trace <host> [-m <maxttl>] [-q <probes>]")
		}
		n, err := strconv.Atoi(args[i+1])
		if err != nil || n < 1 || n > 64 {
			return malformed("trace <host> [-m <maxttl>] [-q <probes>]")
		}
		if args[i] == "-m" {
			hops = n
		} else {
			probes = n
		}
	}
	return traceHost(host, hops, probes)
}

func commandDNS(args []string) string {
	if len(args) < 1 || len(args) > 3 {
		return malformed("dns <name> [A|AAAA|CNAME|PTR] [@<server>]")
	}
	typeName, server := "A", ""
	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, "@") {
			server = strings.TrimPrefix(arg, "@")
			continue
		}
		upper := strings.ToUpper(arg)
		if upper != "A" && upper != "AAAA" && upper != "CNAME" && upper != "PTR" {
			return malformed("dns <name> [A|AAAA|CNAME|PTR] [@<server>]")
		}
		typeName = upper
	}
	return dnsQuery(args[0], typeName, server)
}

func parsePingArgs(args []string) (string, int, int, int, int, bool) {
	if len(args) < 1 {
		return "", 0, 0, 0, 0, false
	}
	host := args[0]
	count, interval, size, ttl := 5, 1000, 56, 64
	for i := 1; i < len(args); i += 2 {
		if i+1 >= len(args) || (args[i] != "-c" && args[i] != "-i" && args[i] != "-s" && args[i] != "-t") {
			return "", 0, 0, 0, 0, false
		}
		n, err := strconv.Atoi(args[i+1])
		if err != nil || n < 1 || n > 65535 {
			return "", 0, 0, 0, 0, false
		}
		switch args[i] {
		case "-c":
			count = n
		case "-i":
			interval = n
		case "-s":
			size = n
		case "-t":
			ttl = n
		}
	}
	if count > 100 || size > 1500 || ttl > 255 {
		return "", 0, 0, 0, 0, false
	}
	return host, count, interval, size, ttl, true
}

func splitSavedCommands(value string) []string {
	lines := strings.Split(value, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if line != "" && printableASCII(line) && len([]byte(line)) <= 200 {
			out = append(out, line)
		}
		if len(out) == 64 {
			break
		}
	}
	return out
}

func _unusedCLIFormatting() string { return fmt.Sprint("") }
