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

// handleCLIConnection is the ONLY console this pack (or any tool pack) types
// commands into — every other pack's console is "http" (a browser dashboard,
// no keystroke handling). Unlike an IOL node's console, which is a real pty
// and gets keystroke echo for free from the guest kernel's tty layer, this is
// a bare AF_UNIX socket wrapped in the same console hub IOL uses (see
// node.NewConsoleBridge) purely as a byte-stream multiplexer — nothing on
// that path echoes. bufio.Scanner-based line reading only ever wrote back
// the command's RESPONSE after a full line arrived, so every keystroke
// appeared to do nothing until Enter, and even then nothing showed the typed
// line — indistinguishable from a dead console. Reading byte-by-byte here
// and echoing as we go (with basic backspace handling) is what makes typing
// visible at all.
func handleCLIConnection(conn net.Conn, app *App) {
	defer conn.Close()
	_, _ = io.WriteString(conn, cliPrompt)
	reader := bufio.NewReader(conn)
	var line []byte
	// esc tracks a partially-read ANSI escape sequence for arrow keys: 0 = none,
	// 1 = saw ESC (0x1b), 2 = saw ESC '['. xterm/xterm.js send up as ESC [ A and
	// down as ESC [ B; every other CSI final byte is dropped once seen, same as
	// the always-dropped control bytes below.
	esc := 0
	// hist is a snapshot of app.state.History() taken lazily on the first arrow
	// press per line (oldest-first); pos counts back from len(hist) (not yet
	// navigating) down to 0 (oldest). pending holds what was typed before the
	// first up-arrow, restored when navigating back past the newest entry.
	var hist []string
	pos := -1
	var pending []byte
	// justDispatchedCR pairs a CRLF line ending across two ReadByte calls so
	// pasted Windows-style text doesn't dispatch an extra empty line for the
	// \n half — see the CRLF check in the main loop below.
	justDispatchedCR := false

	redraw := func(next []byte) {
		for range line {
			_, _ = io.WriteString(conn, "\b \b")
		}
		line = append(line[:0], next...)
		_, _ = conn.Write(line)
	}

	for {
		b, err := reader.ReadByte()
		if err != nil {
			return
		}
		if esc == 1 {
			esc = 0
			if b == '[' {
				esc = 2
				continue
			}
			continue // lone ESC or an unsupported ESC-prefixed sequence
		}
		if esc == 2 {
			esc = 0
			switch b {
			case 'A': // up — recall an older command
				if hist == nil {
					hist = app.state.History()
					pos = len(hist)
				}
				if pos > 0 {
					if pos == len(hist) {
						pending = append([]byte(nil), line...)
					}
					pos--
					redraw([]byte(hist[pos]))
				}
			case 'B': // down — return toward the in-progress line
				if hist != nil && pos < len(hist) {
					pos++
					if pos == len(hist) {
						redraw(pending)
					} else {
						redraw([]byte(hist[pos]))
					}
				}
			default:
				// Left/right/other CSI sequences: this CLI has no cursor
				// positioning within the line, so drop them rather than
				// corrupt the visible buffer.
			}
			continue
		}
		if b == '\n' && justDispatchedCR {
			// The other half of a CRLF line ending (paste of Windows-style
			// text, or a client that sends both bytes for Enter) — \r just
			// below already dispatched this line; treat this \n as inert
			// instead of dispatching a second, empty line.
			justDispatchedCR = false
			continue
		}
		justDispatchedCR = false
		switch {
		case b == 0x1b: // ESC — start of a possible arrow-key sequence
			esc = 1
		case b == '\r' || b == '\n':
			justDispatchedCR = b == '\r'
			_, _ = io.WriteString(conn, "\r\n"+dispatchLine(app, string(line))+"\r\n"+cliPrompt)
			line = line[:0]
			hist, pos, pending = nil, -1, nil
		case b == 0x7f || b == 0x08: // Backspace/DEL
			if len(line) > 0 {
				line = line[:len(line)-1]
				_, _ = io.WriteString(conn, "\b \b")
			}
		case b >= 0x20 && b < 0x7f: // printable ASCII
			line = append(line, b)
			_, _ = conn.Write([]byte{b})
		default:
			// Control bytes (Ctrl-C, telnet IAC stragglers, ...) are silently
			// dropped rather than fed into the line buffer — this CLI has no
			// line-editing beyond backspace and arrow-key recall, so echoing
			// them back would just corrupt the visible line.
		}
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
	host, count, interval, size, ttl, df, ok := parsePingArgs(args)
	if !ok {
		return malformed("ping <host> [-c <n>] [-i <ms>] [-s <bytes>] [-t <ttl>] [-D]")
	}
	return pingHost(host, count, interval, size, ttl, df)
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

func parsePingArgs(args []string) (string, int, int, int, int, bool, bool) {
	fail := func() (string, int, int, int, int, bool, bool) { return "", 0, 0, 0, 0, false, false }
	if len(args) < 1 {
		return fail()
	}
	host := args[0]
	count, interval, size, ttl, df := 5, 1000, 56, 64, false
	for i := 1; i < len(args); i++ {
		if args[i] == "-D" {
			df = true
			continue
		}
		if i+1 >= len(args) || (args[i] != "-c" && args[i] != "-i" && args[i] != "-s" && args[i] != "-t") {
			return fail()
		}
		n, err := strconv.Atoi(args[i+1])
		if err != nil || n < 1 || n > 65535 {
			return fail()
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
		i++
	}
	if count > 100 || size > 1500 || ttl > 255 {
		return fail()
	}
	return host, count, interval, size, ttl, df, true
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
