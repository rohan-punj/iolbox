package main

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxDatagramSize = 8192

type Entry struct {
	Received   time.Time
	SourceIP   string
	Facility   int
	Severity   int
	DeviceTime string
	Hostname   string
	Tag        string
	Message    string
	Raw        string

	priorityParsed bool
	structuredData string
	truncated      bool
}

type entryRing struct {
	mu    sync.RWMutex
	items []Entry
	max   int
}

func newEntryRing(max int) *entryRing {
	if max < 1 {
		max = defaultConfig().MaxEntries
	}
	return &entryRing{max: max}
}

func (r *entryRing) Add(item Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, item)
	if len(r.items) > r.max {
		r.items = append([]Entry(nil), r.items[len(r.items)-r.max:]...)
	}
}

func (r *entryRing) List() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Entry(nil), r.items...)
}

func (r *entryRing) Resize(max int) {
	if max < 1 {
		max = defaultConfig().MaxEntries
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.max = max
	if len(r.items) > r.max {
		r.items = append([]Entry(nil), r.items[len(r.items)-r.max:]...)
	}
}

func (r *entryRing) Clear() {
	r.mu.Lock()
	r.items = nil
	r.mu.Unlock()
}

type Receiver struct {
	mu      sync.RWMutex
	conn    *net.UDPConn
	ring    *entryRing
	lastErr error
}

func NewReceiver(maxEntries int) *Receiver {
	return &Receiver{ring: newEntryRing(maxEntries)}
}

func (r *Receiver) Start(port int) error {
	if err := validatePort(port); err != nil {
		r.setError(err)
		return err
	}
	r.mu.RLock()
	started := r.conn != nil
	r.mu.RUnlock()
	if started {
		return nil
	}
	return r.replaceListener(port)
}

func (r *Receiver) Restart(port int) error {
	if err := validatePort(port); err != nil {
		r.setError(err)
		return err
	}
	r.mu.RLock()
	current := r.conn
	currentPort := 0
	if current != nil {
		if addr, ok := current.LocalAddr().(*net.UDPAddr); ok {
			currentPort = addr.Port
		}
	}
	r.mu.RUnlock()
	if current != nil && port != 0 && currentPort == port {
		r.clearError()
		return nil
	}
	return r.replaceListener(port)
}

func (r *Receiver) replaceListener(port int) error {
	next, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: port})
	if err != nil {
		r.setError(err)
		return err
	}
	r.mu.Lock()
	old := r.conn
	r.conn = next
	r.lastErr = nil
	r.mu.Unlock()
	go r.readLoop(next)
	if old != nil {
		_ = old.Close()
	}
	return nil
}

func validatePort(port int) error {
	if port < 0 || port > 65535 {
		return fmt.Errorf("port must be between 0 and 65535")
	}
	return nil
}

func (r *Receiver) readLoop(conn *net.UDPConn) {
	buf := make([]byte, maxDatagramSize+1)
	for {
		n, _, flags, addr, err := conn.ReadMsgUDP(buf, nil)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			r.recordReadError(conn, err)
			return
		}
		truncated := n > maxDatagramSize || flags&msgTruncFlag() != 0
		if n > maxDatagramSize {
			n = maxDatagramSize
		}
		sourceIP := ""
		if addr != nil {
			sourceIP = addr.IP.String()
		}
		entry := parseSyslog(string(buf[:n]), sourceIP)
		entry.Received = time.Now()
		entry.truncated = truncated
		r.ring.Add(entry)
	}
}

func (r *Receiver) recordReadError(conn *net.UDPConn, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conn == conn {
		r.conn = nil
		r.lastErr = err
	}
}

func (r *Receiver) setError(err error) {
	r.mu.Lock()
	r.lastErr = err
	r.mu.Unlock()
}

func (r *Receiver) clearError() {
	r.mu.Lock()
	r.lastErr = nil
	r.mu.Unlock()
}

func (r *Receiver) LastError() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.lastErr == nil {
		return ""
	}
	return r.lastErr.Error()
}

func (r *Receiver) Addr() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.conn == nil {
		return ""
	}
	return r.conn.LocalAddr().String()
}

func (r *Receiver) Entries() []Entry { return r.ring.List() }

func (r *Receiver) Filter(query string, maxSeverity int) []Entry {
	query = strings.ToLower(query)
	items := r.ring.List()
	filtered := make([]Entry, 0, len(items))
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if query != "" && !strings.Contains(strings.ToLower(item.Raw+"\x00"+item.Hostname+"\x00"+item.Tag+"\x00"+item.Message), query) {
			continue
		}
		if maxSeverity >= 0 && (!item.priorityParsed || item.Severity > maxSeverity) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func (r *Receiver) Resize(maxEntries int) { r.ring.Resize(maxEntries) }

func (r *Receiver) Clear() { r.ring.Clear() }

func (r *Receiver) Close() error {
	r.mu.Lock()
	conn := r.conn
	r.conn = nil
	r.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.Close()
}

var syslogTimestamp = regexp.MustCompile(`^([*.]?[A-Z][a-z]{2} +[0-9]{1,2} +[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?)(: | +)`)

func parseSyslog(raw, sourceIP string) Entry {
	entry := Entry{SourceIP: sourceIP, Hostname: sourceIP, Message: raw, Raw: raw}
	rest, facility, severity, priorityOK := consumePRI(raw)
	if !priorityOK {
		return entry
	}
	entry.Facility = facility
	entry.Severity = severity
	entry.priorityParsed = true
	if parseRFC5424(rest, &entry) {
		return entry
	}
	if parseRFC3164OrCisco(rest, sourceIP, &entry) {
		return entry
	}
	return entry
}

func consumePRI(raw string) (rest string, facility, severity int, ok bool) {
	if len(raw) < 3 || raw[0] != '<' {
		return raw, 0, 0, false
	}
	end := strings.IndexByte(raw[1:], '>')
	if end < 1 || end > 3 {
		return raw, 0, 0, false
	}
	digits := raw[1 : end+1]
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return raw, 0, 0, false
		}
	}
	pri, err := strconv.Atoi(digits)
	if err != nil {
		return raw, 0, 0, false
	}
	return raw[end+2:], pri / 8, pri % 8, true
}

func parseRFC5424(rest string, entry *Entry) bool {
	if !strings.HasPrefix(rest, "1 ") {
		return false
	}
	version, rest, ok := takeField(rest)
	if !ok || version != "1" {
		return false
	}
	timestamp, rest, ok := takeField(rest)
	if !ok {
		return false
	}
	hostname, rest, ok := takeField(rest)
	if !ok {
		return false
	}
	appName, rest, ok := takeField(rest)
	if !ok {
		return false
	}
	_, rest, ok = takeField(rest) // PROCID
	if !ok {
		return false
	}
	_, rest, ok = takeField(rest) // MSGID
	if !ok {
		return false
	}
	structuredData, message, ok := takeStructuredData(rest)
	if !ok {
		return false
	}
	entry.DeviceTime = timestamp
	entry.Hostname = hostname
	entry.Tag = appName
	entry.Message = message
	entry.structuredData = structuredData
	return true
}

func takeField(input string) (field, rest string, ok bool) {
	space := strings.IndexByte(input, ' ')
	if space < 1 {
		return "", input, false
	}
	return input[:space], input[space+1:], true
}

func takeStructuredData(input string) (structuredData, message string, ok bool) {
	if input == "" {
		return "", "", false
	}
	if input[0] == '-' {
		if len(input) == 1 {
			return "-", "", true
		}
		if input[1] != ' ' {
			return "", "", false
		}
		return "-", strings.TrimLeft(input[2:], " "), true
	}
	if input[0] != '[' {
		return "", "", false
	}
	end := 0
	for end < len(input) && input[end] == '[' {
		blockEnd, found := scanStructuredElement(input[end:])
		if !found {
			return "", "", false
		}
		end += blockEnd
	}
	if end == 0 || (end < len(input) && input[end] != ' ') {
		return "", "", false
	}
	if end == len(input) {
		return input, "", true
	}
	return input[:end], strings.TrimLeft(input[end+1:], " "), true
}

func scanStructuredElement(input string) (end int, ok bool) {
	if input == "" || input[0] != '[' {
		return 0, false
	}
	depth := 0
	inQuote := false
	escaped := false
	for i := 0; i < len(input); i++ {
		ch := input[i]
		if inQuote {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inQuote = false
			}
			continue
		}
		switch ch {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		case '"':
			inQuote = true
		}
	}
	return 0, false
}

func parseRFC3164OrCisco(rest, sourceIP string, entry *Entry) bool {
	// Real IOS-XE output was found live to stack TWO leading numeric
	// sequence prefixes on a syslog-transmitted message (e.g. "96: 000091: "),
	// not just the one "service sequence-numbers" console prefix the RFC
	// 3164 spec anticipates — strip every leading "NNN: " run, not only the
	// first, or the timestamp match below silently fails and the whole line
	// falls through to the unparsed fallback.
	for {
		sequenceEnd := ciscoSequenceEnd(rest)
		if sequenceEnd == 0 {
			break
		}
		rest = rest[sequenceEnd:]
	}
	matches := syslogTimestamp.FindStringSubmatch(rest)
	if len(matches) != 3 {
		return false
	}
	entry.DeviceTime = matches[1]
	rest = rest[len(matches[0]):]
	if strings.HasPrefix(rest, "%") {
		colon := strings.IndexByte(rest, ':')
		if colon > 1 && !strings.ContainsAny(rest[1:colon], " \t") {
			entry.Hostname = sourceIP
			entry.Tag = rest[:colon]
			entry.Message = strings.TrimSpace(rest[colon+1:])
			return true
		}
	}
	space := strings.IndexByte(rest, ' ')
	if space <= 0 {
		return false
	}
	entry.Hostname = rest[:space]
	tagAndMessage := strings.TrimLeft(rest[space+1:], " ")
	colon := strings.IndexByte(tagAndMessage, ':')
	if colon <= 0 {
		return false
	}
	tag := tagAndMessage[:colon]
	if strings.ContainsAny(tag, " \t") {
		return false
	}
	if bracket := strings.IndexByte(tag, '['); bracket > 0 && strings.HasSuffix(tag, "]") {
		tag = tag[:bracket]
	}
	entry.Tag = tag
	entry.Message = strings.TrimSpace(tagAndMessage[colon+1:])
	return true
}

func ciscoSequenceEnd(input string) int {
	i := 0
	for i < len(input) && input[i] >= '0' && input[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(input) || input[i] != ':' || input[i+1] != ' ' {
		return 0
	}
	return i + 2
}

func severityName(e Entry) string {
	if !e.priorityParsed || e.Severity < 0 || e.Severity > 7 {
		return "unknown"
	}
	return [...]string{"emerg", "alert", "crit", "err", "warning", "notice", "info", "debug"}[e.Severity]
}

func severityClass(e Entry) string {
	if !e.priorityParsed {
		return "severity-normal"
	}
	switch e.Severity {
	case 0, 1, 2, 3:
		return "severity-red"
	case 4:
		return "severity-amber"
	default:
		return "severity-normal"
	}
}
