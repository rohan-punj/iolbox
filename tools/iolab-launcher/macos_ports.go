package main

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultDarwinGUIPort = 4001
	darwinGUIGuestPort   = 4001
	darwinControlPort    = 4000
	darwinConsoleStart   = 9000
	darwinConsoleEnd     = 9049
	darwinCaptureStart   = 5500
	darwinCaptureEnd     = 5529
)

// darwinPortContract is the host-side Lima forwarding contract. The console
// and capture ranges are deliberately fixed: their allocators must agree with
// the guest and with the browser-visible port facts.
type darwinPortContract struct {
	GUIPort          int
	ConsoleHostStart int
	CaptureHostStart int
}

type darwinPortForwardRule map[string]string

func newDarwinPortContract(guiPort int) (darwinPortContract, error) {
	return newDarwinPortContractWithRanges(guiPort, darwinConsoleStart, darwinCaptureStart)
}

func newDarwinPortContractWithRanges(guiPort, consoleHostStart, captureHostStart int) (darwinPortContract, error) {
	if guiPort == 0 {
		guiPort = defaultDarwinGUIPort
	}
	if consoleHostStart == 0 {
		consoleHostStart = darwinConsoleStart
	}
	if captureHostStart == 0 {
		captureHostStart = darwinCaptureStart
	}
	contract := darwinPortContract{GUIPort: guiPort, ConsoleHostStart: consoleHostStart, CaptureHostStart: captureHostStart}
	if err := contract.validate(); err != nil {
		return darwinPortContract{}, err
	}
	return contract, nil
}

func (p darwinPortContract) validate() error {
	if p.GUIPort < 1 || p.GUIPort > 65535 {
		return fmt.Errorf("Darwin GUI port must be between 1 and 65535, got %d", p.GUIPort)
	}
	if p.GUIPort == darwinControlPort {
		return fmt.Errorf("Darwin GUI port %d is reserved for the guest-only control listener", darwinControlPort)
	}
	consoleEnd := p.ConsoleHostStart + darwinConsoleEnd - darwinConsoleStart
	captureEnd := p.CaptureHostStart + darwinCaptureEnd - darwinCaptureStart
	if p.ConsoleHostStart < 1 || consoleEnd > 65535 {
		return fmt.Errorf("Darwin console host range %d-%d is invalid", p.ConsoleHostStart, consoleEnd)
	}
	if p.CaptureHostStart < 1 || captureEnd > 65535 {
		return fmt.Errorf("Darwin capture host range %d-%d is invalid", p.CaptureHostStart, captureEnd)
	}
	if p.GUIPort >= p.ConsoleHostStart && p.GUIPort <= consoleEnd {
		return fmt.Errorf("Darwin GUI port %d overlaps the console range %d-%d", p.GUIPort, p.ConsoleHostStart, consoleEnd)
	}
	if p.GUIPort >= p.CaptureHostStart && p.GUIPort <= captureEnd {
		return fmt.Errorf("Darwin GUI port %d overlaps the capture range %d-%d", p.GUIPort, p.CaptureHostStart, captureEnd)
	}
	if p.ConsoleHostStart <= captureEnd && p.CaptureHostStart <= consoleEnd {
		return fmt.Errorf("Darwin console and capture host ranges overlap")
	}
	return nil
}

// requiredPorts returns the GUI port followed by every allocator port. It is
// intentionally not a sampled range: preflight and diagnostics must cover all
// 81 host ports in the contract.
func (p darwinPortContract) requiredPorts() []int {
	ports := []int{p.GUIPort}
	for port := p.ConsoleHostStart; port <= p.ConsoleHostStart+darwinConsoleEnd-darwinConsoleStart; port++ {
		ports = append(ports, port)
	}
	for port := p.CaptureHostStart; port <= p.CaptureHostStart+darwinCaptureEnd-darwinCaptureStart; port++ {
		ports = append(ports, port)
	}
	return ports
}

func (p darwinPortContract) yamlPortForwards() string {
	return fmt.Sprintf(`portForwards:
  - guestPort: %d
    hostPort: %d
    hostIP: "127.0.0.1"
    proto: "tcp"
  - guestPortRange: [%d, %d]
    hostPortRange: [%d, %d]
    hostIP: "127.0.0.1"
    proto: "tcp"
  - guestPortRange: [%d, %d]
    hostPortRange: [%d, %d]
    hostIP: "127.0.0.1"
    proto: "tcp"
  - guestIP: "127.0.0.1"
    guestPortRange: [1, 65535]
    proto: "any"
    ignore: true`,
		darwinGUIGuestPort, p.GUIPort,
		darwinConsoleStart, darwinConsoleEnd, p.ConsoleHostStart, p.ConsoleHostStart+darwinConsoleEnd-darwinConsoleStart,
		darwinCaptureStart, darwinCaptureEnd, p.CaptureHostStart, p.CaptureHostStart+darwinCaptureEnd-darwinCaptureStart)
}

// limaSetExpression is the single YAML value passed to limactl start --set
// when a stopped pre-M3 instance receives the explicit forwarding contract.
// Keeping it as one argv element is important for shells and for paths with
// spaces elsewhere in the launcher command line.
func (p darwinPortContract) limaSetExpression() string {
	return fmt.Sprintf(`.portForwards=[{"guestPort": %d, "hostPort": %d, "hostIP": "127.0.0.1", "proto": "tcp"},{"guestPortRange": [%d,%d], "hostPortRange": [%d,%d], "hostIP": "127.0.0.1", "proto": "tcp"},{"guestPortRange": [%d,%d], "hostPortRange": [%d,%d], "hostIP": "127.0.0.1", "proto": "tcp"},{"guestIP": "127.0.0.1", "guestPortRange": [1,65535], "proto": "any", "ignore": true}]`,
		darwinGUIGuestPort, p.GUIPort,
		darwinConsoleStart, darwinConsoleEnd, p.ConsoleHostStart, p.ConsoleHostStart+darwinConsoleEnd-darwinConsoleStart,
		darwinCaptureStart, darwinCaptureEnd, p.CaptureHostStart, p.CaptureHostStart+darwinCaptureEnd-darwinCaptureStart)
}

func (p darwinPortContract) limaStartSetArg() string {

	return "--set=" + p.limaSetExpression()
}

func expectedDarwinPortForwardRules(p darwinPortContract) []darwinPortForwardRule {
	if p.ConsoleHostStart == 0 {
		p.ConsoleHostStart = darwinConsoleStart
	}
	if p.CaptureHostStart == 0 {
		p.CaptureHostStart = darwinCaptureStart
	}
	return []darwinPortForwardRule{
		{"guestPort": strconv.Itoa(darwinGUIGuestPort), "hostPort": strconv.Itoa(p.GUIPort), "hostIP": "127.0.0.1", "proto": "tcp"},
		{"guestPortRange": "[9000,9049]", "hostPortRange": fmt.Sprintf("[%d,%d]", p.ConsoleHostStart, p.ConsoleHostStart+darwinConsoleEnd-darwinConsoleStart), "hostIP": "127.0.0.1", "proto": "tcp"},
		{"guestPortRange": "[5500,5529]", "hostPortRange": fmt.Sprintf("[%d,%d]", p.CaptureHostStart, p.CaptureHostStart+darwinCaptureEnd-darwinCaptureStart), "hostIP": "127.0.0.1", "proto": "tcp"},
		{"guestIP": "127.0.0.1", "guestPortRange": "[1,65535]", "proto": "any", "ignore": "true"},
	}
}

// parseDarwinPortForwardRules parses the scalar-only portForwards fragment
// Lima stores in an instance's lima.yaml. It is deliberately not a general
// YAML parser; this contract contains only scalar fields and integer ranges.
func parseDarwinPortForwardRules(text string) ([]darwinPortForwardRule, error) {
	var rules []darwinPortForwardRule
	var current darwinPortForwardRule
	blockIndent, ruleIndent := -1, -1
	inBlock := false
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for lineNo := 0; lineNo < len(lines); lineNo++ {
		line := lines[lineNo]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if !inBlock {
			if trimmed == "portForwards:" {
				blockIndent = indent
				inBlock = true
			}
			continue
		}
		if ruleIndent >= 0 && indent <= blockIndent && !strings.HasPrefix(trimmed, "-") {
			break
		}
		if ruleIndent < 0 {
			if strings.HasPrefix(trimmed, "-") {
				ruleIndent = indent
			} else if indent <= blockIndent {
				break
			} else {
				continue
			}
		}
		if strings.HasPrefix(trimmed, "-") {
			if current != nil {
				rules = append(rules, current)
			}
			current = make(darwinPortForwardRule)
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
			if trimmed == "" {
				continue
			}
		}
		if current == nil {
			return nil, fmt.Errorf("portForwards line %d is not under a list item", lineNo+1)
		}
		colon := strings.IndexByte(trimmed, ':')
		if colon <= 0 {
			return nil, fmt.Errorf("invalid portForwards field on line %d: %q", lineNo+1, trimmed)
		}
		key := strings.TrimSpace(trimmed[:colon])
		value := strings.TrimSpace(trimmed[colon+1:])
		if key == "" {
			return nil, fmt.Errorf("invalid portForwards field on line %d: %q", lineNo+1, trimmed)
		}
		if value == "" {
			// Block-sequence form, e.g.:
			//   guestPortRange:
			//   - 9000
			//   - 9049
			// instead of the flow form this launcher itself writes
			// (guestPortRange: [9000, 9049]). Lima's own YAML marshaler
			// re-emits block style whenever it resaves a config (observed
			// on real hardware after a forced-kill/restart cycle rewrote
			// the running VM's lima.yaml), so both forms must parse to the
			// same canonical value or a perfectly healthy VM becomes
			// unrecoverable by this launcher's own port-contract check.
			items, consumed, err := parseDarwinYAMLBlockSequence(lines, lineNo+1, ruleIndent)
			if err != nil {
				return nil, fmt.Errorf("invalid portForwards block on line %d: %w", lineNo+1, err)
			}
			value = "[" + strings.Join(items, ",") + "]"
			lineNo += consumed
		}
		current[key] = canonicalDarwinPortValue(value)
	}
	if current != nil {
		rules = append(rules, current)
	}
	return rules, nil
}

// parseDarwinYAMLBlockSequence reads the "- item" lines of a YAML block
// sequence that starts immediately after a "key:" line with no inline
// value, returning the scalar items and how many lines were consumed.
//
// minIndent is the enclosing rule's indent (the "-" of "- guestPort: ..."),
// not the "key:" line's own indent — go-yaml's default block style renders
// a mapping key's nested sequence at the SAME indent as the key itself when
// that key is a continuation field inside a list item (observed verbatim on
// real hardware: "hostPortRange:" and its own "- 9000"/"- 9049" items sit at
// identical indent). Using the key's own indent as the floor would reject
// exactly that, the most common real-world shape. Sequence items are
// distinguished from the next sibling field purely by the "-" prefix, which
// is why minIndent only needs to exclude a return to the next top-level
// rule, not bound the sequence tightly against its key.
func parseDarwinYAMLBlockSequence(lines []string, start, minIndent int) (items []string, consumed int, err error) {
	for i := start; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			consumed = i - start + 1
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent <= minIndent || !strings.HasPrefix(trimmed, "-") {
			break
		}
		item := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		if item == "" {
			return nil, 0, fmt.Errorf("empty block sequence item on line %d", i+1)
		}
		items = append(items, item)
		consumed = i - start + 1
	}
	if len(items) == 0 {
		return nil, 0, fmt.Errorf("expected a block sequence after line %d", start+1)
	}
	return items, consumed, nil
}

func canonicalDarwinPortValue(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "\"'")
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		value = strings.ReplaceAll(value, " ", "")
		value = strings.ReplaceAll(value, "\t", "")
	}
	return value
}

func darwinPortForwardRulesEqual(got, want darwinPortForwardRule) bool {
	if len(got) != len(want) {
		return false
	}
	for key, value := range want {
		if got[key] != value {
			return false
		}
	}
	return true
}

func darwinPortContractMatchesYAML(data []byte, p darwinPortContract) (bool, error) {
	rules, err := parseDarwinPortForwardRules(string(data))
	if err != nil {
		return false, err
	}
	want := expectedDarwinPortForwardRules(p)
	if len(rules) < len(want) {
		return false, nil
	}
	for i := range want {
		if !darwinPortForwardRulesEqual(rules[i], want[i]) {
			return false, nil
		}
	}
	return true, nil
}

func darwinPortConflicts(p darwinPortContract, probe func(int) error) []int {
	ports := p.requiredPorts()
	conflicts := make([]int, 0)
	for _, port := range ports {
		if err := probe(port); err != nil {
			conflicts = append(conflicts, port)
		}
	}
	return conflicts
}

func probeDarwinTCPPort(port int) error {
	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	return listener.Close()
}

func preflightDarwinPorts(p darwinPortContract) error {
	if err := p.validate(); err != nil {
		return codedError(exitPreflight, "%v", err)
	}
	conflicts := darwinPortConflicts(p, probeDarwinTCPPort)
	if len(conflicts) == 0 {
		return nil
	}
	parts := make([]string, len(conflicts))
	for i, port := range conflicts {
		parts[i] = strconv.Itoa(port)
	}
	return codedError(exitPreflight, "required Darwin host port(s) are busy: %s", strings.Join(parts, ", "))
}

// verifyDarwinHostContract is the runtime negative check that catches a
// pre-M3 Lima instance which still installed automatic forwarding for the
// guest-loopback supervisor port. GUI readiness itself is performed against
// the loopback URL by the caller via waitHTTPReady.
func verifyDarwinHostContract(p darwinPortContract, timeout time.Duration) error {
	if p.GUIPort == darwinControlPort {
		return codedError(exitVerify, "Darwin GUI port cannot be the guest-only control port %d", darwinControlPort)
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(darwinControlPort)), timeout)
	if err == nil {
		_ = conn.Close()
		return codedError(exitVerify, "host 127.0.0.1:%d is reachable; stop and start the machine once to apply the Lima port contract", darwinControlPort)
	}
	return nil
}

func sortedDarwinPorts(p darwinPortContract) []int {
	ports := append([]int(nil), p.requiredPorts()...)
	sort.Ints(ports)
	return ports
}
