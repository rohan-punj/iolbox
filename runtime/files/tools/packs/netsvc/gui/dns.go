package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	dnsTypeA     uint16 = 1
	dnsTypeNS    uint16 = 2
	dnsTypeCNAME uint16 = 5
	dnsTypePTR   uint16 = 12
	dnsTypeAAAA  uint16 = 28
	dnsClassIN   uint16 = 1
	dnsQR        uint16 = 0x8000
	dnsAA        uint16 = 0x0400
	dnsTC        uint16 = 0x0200
	dnsRD        uint16 = 0x0100
)

type DNSHeader struct{ ID, Flags, QDCount, ANCount, NSCount, ARCount uint16 }
type DNSQuestion struct {
	Name        string
	Type, Class uint16
}
type DNSRR struct {
	Name        string
	Type, Class uint16
	TTL         uint32
	Data        []byte
	Text        string
}
type DNSMessage struct {
	Header                            DNSHeader
	Questions                         []DNSQuestion
	Answers, Authorities, Additionals []DNSRR
}

func dnsFlags(qr bool, opcode uint16, aa, tc, rd, ra bool, rcode uint16) uint16 {
	var f uint16
	if qr {
		f |= dnsQR
	}
	f |= (opcode & 0xf) << 11
	if aa {
		f |= dnsAA
	}
	if tc {
		f |= dnsTC
	}
	if rd {
		f |= dnsRD
	}
	if ra {
		f |= 0
	}
	f |= rcode & 0xf
	return f
}

func ParseDNSMessage(b []byte) (DNSMessage, error) {
	var msg DNSMessage
	if len(b) < 12 {
		return msg, errors.New("DNS message shorter than header")
	}
	msg.Header = DNSHeader{binary.BigEndian.Uint16(b[0:2]), binary.BigEndian.Uint16(b[2:4]), binary.BigEndian.Uint16(b[4:6]), binary.BigEndian.Uint16(b[6:8]), binary.BigEndian.Uint16(b[8:10]), binary.BigEndian.Uint16(b[10:12])}
	off := 12
	for i := 0; i < int(msg.Header.QDCount); i++ {
		name, next, err := parseDNSName(b, off)
		if err != nil {
			return msg, err
		}
		off = next
		if off+4 > len(b) {
			return msg, errors.New("DNS question truncated")
		}
		msg.Questions = append(msg.Questions, DNSQuestion{Name: name, Type: binary.BigEndian.Uint16(b[off : off+2]), Class: binary.BigEndian.Uint16(b[off+2 : off+4])})
		off += 4
	}
	readRRs := func(count uint16) ([]DNSRR, error) {
		out := make([]DNSRR, 0, count)
		for i := 0; i < int(count); i++ {
			rr, next, err := parseDNSRR(b, off)
			if err != nil {
				return nil, err
			}
			off = next
			out = append(out, rr)
		}
		return out, nil
	}
	var err error
	if msg.Answers, err = readRRs(msg.Header.ANCount); err != nil {
		return msg, err
	}
	if msg.Authorities, err = readRRs(msg.Header.NSCount); err != nil {
		return msg, err
	}
	if msg.Additionals, err = readRRs(msg.Header.ARCount); err != nil {
		return msg, err
	}
	return msg, nil
}

func parseDNSName(b []byte, off int) (string, int, error) {
	if off < 0 || off >= len(b) {
		return "", off, errors.New("DNS name offset out of range")
	}
	labels := make([]string, 0, 4)
	pos, next, jumps := off, off, 0
	jumped := false
	for {
		if pos >= len(b) {
			return "", next, errors.New("DNS name truncated")
		}
		length := b[pos]
		switch length & 0xc0 {
		case 0xc0:
			if pos+1 >= len(b) {
				return "", next, errors.New("DNS compression pointer truncated")
			}
			ptr := int(length&0x3f)<<8 | int(b[pos+1])
			if ptr >= pos {
				return "", next, errors.New("DNS compression pointer points forward or to itself")
			}
			if !jumped {
				next = pos + 2
				jumped = true
			}
			pos = ptr
			jumps++
			if jumps > 16 {
				return "", next, errors.New("DNS compression pointer loop")
			}
			continue
		case 0x00:
			if length == 0 {
				if !jumped {
					next = pos + 1
				}
				return strings.Join(labels, ".") + ".", next, nil
			}
			if length > 63 {
				return "", next, errors.New("DNS label too long")
			}
			pos++
			if pos+int(length) > len(b) {
				return "", next, errors.New("DNS label truncated")
			}
			labels = append(labels, string(b[pos:pos+int(length)]))
			if len(strings.Join(labels, ".")) > 253 {
				return "", next, errors.New("DNS name too long")
			}
			pos += int(length)
		default:
			return "", next, errors.New("invalid DNS label type")
		}
	}
}

func parseDNSRR(b []byte, off int) (DNSRR, int, error) {
	var rr DNSRR
	name, next, err := parseDNSName(b, off)
	if err != nil {
		return rr, off, err
	}
	off = next
	if off+10 > len(b) {
		return rr, off, errors.New("DNS RR header truncated")
	}
	rr.Name, rr.Type, rr.Class, rr.TTL = name, binary.BigEndian.Uint16(b[off:off+2]), binary.BigEndian.Uint16(b[off+2:off+4]), binary.BigEndian.Uint32(b[off+4:off+8])
	n := int(binary.BigEndian.Uint16(b[off+8 : off+10]))
	off += 10
	if off+n > len(b) {
		return rr, off, errors.New("DNS RR data truncated")
	}
	rr.Data = append([]byte(nil), b[off:off+n]...)
	if rr.Type == dnsTypeCNAME || rr.Type == dnsTypePTR || rr.Type == dnsTypeNS {
		text, _, err := parseDNSName(b, off)
		if err != nil {
			return rr, off, err
		}
		rr.Text = text
	}
	return rr, off + n, nil
}

func EncodeDNSMessage(msg DNSMessage) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[0:2], msg.Header.ID)
	binary.BigEndian.PutUint16(b[2:4], msg.Header.Flags)
	binary.BigEndian.PutUint16(b[4:6], uint16(len(msg.Questions)))
	binary.BigEndian.PutUint16(b[6:8], uint16(len(msg.Answers)))
	binary.BigEndian.PutUint16(b[8:10], uint16(len(msg.Authorities)))
	binary.BigEndian.PutUint16(b[10:12], uint16(len(msg.Additionals)))
	compression := map[string]int{}
	for _, q := range msg.Questions {
		b = writeDNSName(b, q.Name, compression)
		var fixed [4]byte
		binary.BigEndian.PutUint16(fixed[0:2], q.Type)
		binary.BigEndian.PutUint16(fixed[2:4], q.Class)
		b = append(b, fixed[:]...)
	}
	for _, rr := range append(append(append([]DNSRR{}, msg.Answers...), msg.Authorities...), msg.Additionals...) {
		b = writeDNSName(b, rr.Name, compression)
		var fixed [10]byte
		binary.BigEndian.PutUint16(fixed[0:2], rr.Type)
		binary.BigEndian.PutUint16(fixed[2:4], rr.Class)
		binary.BigEndian.PutUint32(fixed[4:8], rr.TTL)
		rdlen := len(rr.Data)
		if rr.Type == dnsTypeCNAME || rr.Type == dnsTypePTR || rr.Type == dnsTypeNS {
			rdlen = 0
		}
		binary.BigEndian.PutUint16(fixed[8:10], uint16(rdlen))
		rdlenPos := len(b) + 8
		b = append(b, fixed[:]...)
		if rr.Type == dnsTypeCNAME || rr.Type == dnsTypePTR || rr.Type == dnsTypeNS {
			start := len(b)
			b = writeDNSName(b, rr.Text, compression)
			binary.BigEndian.PutUint16(b[rdlenPos:rdlenPos+2], uint16(len(b)-start))
		} else {
			b = append(b, rr.Data...)
		}
	}
	return b
}

func writeDNSName(b []byte, name string, compression map[string]int) []byte {
	labels := dnsLabels(name)
	if len(labels) == 0 {
		return append(b, 0)
	}
	for i := 0; i < len(labels); i++ {
		suffix := strings.Join(labels[i:], ".") + "."
		if off, ok := compression[suffix]; ok && off < 0x4000 {
			return append(b, byte(0xc0|off>>8), byte(off))
		}
		compression[suffix] = len(b)
		label := []byte(labels[i])
		if len(label) > 63 {
			label = label[:63]
		}
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	return append(b, 0)
}

func dnsLabels(name string) []string {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	if name == "" {
		return nil
	}
	return strings.Split(name, ".")
}
func canonicalDNSName(name string) string {
	labels := dnsLabels(name)
	if len(labels) == 0 {
		return "."
	}
	return strings.Join(labels, ".") + "."
}

type DNSLog struct {
	At                        time.Time
	Name, Type, Source, RCode string
	Latency                   time.Duration
	Answers                   []DNSRR
	OutsideZone               bool
}

type DNSServer struct {
	store *Store
	mu    sync.RWMutex
	logs  []DNSLog
}

func NewDNSServer(store *Store) *DNSServer { return &DNSServer{store: store} }
func (s *DNSServer) Logs() []DNSLog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]DNSLog(nil), s.logs...)
}

func (s *DNSServer) handlePacket(conn *net.UDPConn, data []byte, addr *net.UDPAddr) {
	start := time.Now()
	query, err := ParseDNSMessage(data)
	if err != nil || len(query.Questions) == 0 {
		return
	}
	response, logRow := s.Answer(query, addr.String())
	logRow.At, logRow.Latency = time.Now(), time.Since(start)
	s.mu.Lock()
	s.logs = append([]DNSLog{logRow}, s.logs...)
	if len(s.logs) > 100 {
		s.logs = s.logs[:100]
	}
	s.mu.Unlock()
	encoded := EncodeDNSMessage(response)
	if len(encoded) > 512 {
		response.Header.Flags |= dnsTC
		for len(EncodeDNSMessage(response)) > 512 && len(response.Answers) > 0 {
			response.Answers = response.Answers[:len(response.Answers)-1]
		}
		encoded = EncodeDNSMessage(response)
	}
	_, _ = conn.WriteToUDP(encoded, addr)
}

func (s *DNSServer) Answer(query DNSMessage, source string) (DNSMessage, DNSLog) {
	q := query.Questions[0]
	q.Name = canonicalDNSName(q.Name)
	cfg := s.store.Snapshot().DNS
	zone := canonicalDNSName(cfg.Zone)
	outside := !dnsInZone(q.Name, zone) && !(q.Type == dnsTypePTR && strings.HasSuffix(q.Name, ".in-addr.arpa."))
	rd := query.Header.Flags&dnsRD != 0
	response := DNSMessage{Header: DNSHeader{ID: query.Header.ID, Flags: dnsFlags(true, (query.Header.Flags>>11)&0xf, true, false, rd, false, 0)}, Questions: []DNSQuestion{q}}
	row := DNSLog{Name: q.Name, Type: dnsTypeName(q.Type), Source: source}
	if outside {
		response.Header.Flags = dnsFlags(true, (query.Header.Flags>>11)&0xf, true, false, rd, false, 3)
		row.RCode = "NXDOMAIN"
		row.OutsideZone = true
		return response, row
	}
	answers := answerRecords(q, cfg.Records)
	if len(answers) == 0 {
		response.Header.Flags = dnsFlags(true, (query.Header.Flags>>11)&0xf, true, false, rd, false, 3)
		row.RCode = "NXDOMAIN"
		return response, row
	}
	response.Answers = answers
	row.RCode = "NOERROR"
	row.Answers = answers
	return response, row
}

func answerRecords(q DNSQuestion, records []DNSRecord) []DNSRR {
	canonical := canonicalDNSName(q.Name)
	out := []DNSRR{}
	byName := map[string][]DNSRecord{}
	for _, rec := range records {
		rec.Name = canonicalDNSName(rec.Name)
		rec.Type = strings.ToUpper(strings.TrimSpace(rec.Type))
		if rec.TTL == 0 {
			rec.TTL = 300
		}
		byName[rec.Name] = append(byName[rec.Name], rec)
	}
	if q.Type == dnsTypePTR {
		for _, rec := range records {
			if strings.EqualFold(rec.Type, "A") && reverseIPv4(rec.Value) == canonical {
				out = append(out, DNSRR{Name: canonical, Type: dnsTypePTR, Class: dnsClassIN, TTL: rec.TTL, Text: canonicalDNSName(rec.Name)})
			}
		}
		return out
	}
	seen := map[string]bool{}
	var add func(string, uint16)
	add = func(name string, typ uint16) {
		name = canonicalDNSName(name)
		if seen[name+fmt.Sprint(typ)] {
			return
		}
		seen[name+fmt.Sprint(typ)] = true
		for _, rec := range byName[name] {
			rt := dnsTypeNumber(rec.Type)
			if rt == typ {
				if rt == dnsTypeNS {
					out = append(out, DNSRR{Name: name, Type: rt, Class: dnsClassIN, TTL: rec.TTL, Text: canonicalDNSName(rec.Value)})
					continue
				}
				if data, ok := dnsRData(rt, rec.Value); ok {
					out = append(out, DNSRR{Name: name, Type: rt, Class: dnsClassIN, TTL: rec.TTL, Data: data})
				}
			}
		}
	}
	for _, rec := range byName[canonical] {
		rt := dnsTypeNumber(rec.Type)
		if rt == dnsTypeCNAME && (q.Type == dnsTypeCNAME || q.Type == dnsTypeA || q.Type == dnsTypeAAAA) {
			out = append(out, DNSRR{Name: canonical, Type: dnsTypeCNAME, Class: dnsClassIN, TTL: rec.TTL, Text: canonicalDNSName(rec.Value)})
			if q.Type != dnsTypeCNAME {
				add(rec.Value, q.Type)
			}
		}
	}
	if len(out) == 0 {
		add(canonical, q.Type)
	}
	return out
}

func dnsInZone(name, zone string) bool {
	zone = canonicalDNSName(zone)
	return name == zone || strings.HasSuffix(name, "."+strings.TrimSuffix(zone, ".")) || strings.HasSuffix(name, zone)
}
func reverseIPv4(value string) string {
	ip := net.ParseIP(strings.TrimSpace(value)).To4()
	if ip == nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa.", ip[3], ip[2], ip[1], ip[0])
}
func dnsTypeNumber(text string) uint16 {
	switch strings.ToUpper(text) {
	case "A":
		return dnsTypeA
	case "AAAA":
		return dnsTypeAAAA
	case "CNAME":
		return dnsTypeCNAME
	case "PTR":
		return dnsTypePTR
	case "NS":
		return dnsTypeNS
	}
	return 0
}
func dnsTypeName(typ uint16) string {
	switch typ {
	case dnsTypeA:
		return "A"
	case dnsTypeAAAA:
		return "AAAA"
	case dnsTypeCNAME:
		return "CNAME"
	case dnsTypePTR:
		return "PTR"
	case dnsTypeNS:
		return "NS"
	}
	return fmt.Sprintf("TYPE%d", typ)
}
func dnsRData(typ uint16, value string) ([]byte, bool) {
	switch typ {
	case dnsTypeA:
		ip := net.ParseIP(value).To4()
		return append([]byte(nil), ip...), ip != nil
	case dnsTypeAAAA:
		ip := net.ParseIP(value).To16()
		return append([]byte(nil), ip...), ip != nil
	case dnsTypeNS:
		// Name-valued RDATA is encoded separately by DNSRR.Text. This branch
		// is kept false so callers do not accidentally put presentation text
		// directly on the wire.
		return nil, false
	default:
		return nil, false
	}
}
