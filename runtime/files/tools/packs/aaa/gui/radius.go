package main

import (
	"crypto/hmac"
	"crypto/md5" // RADIUS PAP and Message-Authenticator are specified with MD5.
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	radiusAccessRequest = 1
	radiusAccessAccept  = 2
	radiusAccessReject  = 3

	attrUserName          = 1
	attrUserPassword      = 2
	attrServiceType       = 6
	attrReplyMessage      = 18
	attrVendorSpecific    = 26
	attrMessageAuth       = 80
	ciscoVendorID         = 9
	ciscoAVPairVendorType = 1
)

type radiusAttribute struct {
	typ   byte
	value []byte
}

type radiusPacket struct {
	code          byte
	identifier    byte
	authenticator [16]byte
	attributes    []radiusAttribute
}

type AuthAttempt struct {
	At      time.Time
	Remote  string
	User    string
	Result  string
	Message string
}

type attemptRing struct {
	mu    sync.RWMutex
	items []AuthAttempt
	max   int
}

func newRing(max int) *attemptRing {
	if max < 1 {
		max = 100
	}
	return &attemptRing{max: max}
}

func (r *attemptRing) Add(item AuthAttempt) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, item)
	if len(r.items) > r.max {
		r.items = append([]AuthAttempt(nil), r.items[len(r.items)-r.max:]...)
	}
}

func (r *attemptRing) List() []AuthAttempt {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]AuthAttempt(nil), r.items...)
}

type RadiusServer struct {
	store *Store
	ring  *attemptRing

	mu   sync.RWMutex
	conn *net.UDPConn
}

func NewRadiusServer(store *Store) *RadiusServer {
	return &RadiusServer{store: store, ring: newRing(100)}
}

func (s *RadiusServer) Attempts() []AuthAttempt { return s.ring.List() }

func (s *RadiusServer) Serve(addr string) error {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.conn == conn {
			s.conn = nil
		}
		s.mu.Unlock()
	}()

	buf := make([]byte, 4096)
	for {
		n, remote, readErr := conn.ReadFromUDP(buf)
		if readErr != nil {
			if errors.Is(readErr, net.ErrClosed) {
				return nil
			}
			return readErr
		}
		response := s.handlePacket(buf[:n], remote.String())
		if len(response) > 0 {
			if _, err := conn.WriteToUDP(response, remote); err != nil && !errors.Is(err, net.ErrClosed) {
				log.Printf("aaa: write RADIUS response: %v", err)
			}
		}
	}
}

func (s *RadiusServer) Close() error {
	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
}

func (s *RadiusServer) handlePacket(data []byte, remote string) []byte {
	cfg := s.store.Snapshot()
	response, attempt := evaluateAccessRequest(data, cfg)
	attempt.At = time.Now()
	attempt.Remote = remote
	s.ring.Add(attempt)
	return response
}

func evaluateAccessRequest(data []byte, cfg Config) ([]byte, AuthAttempt) {
	attempt := AuthAttempt{Result: "reject"}
	packet, err := parseRadiusPacket(data)
	if err != nil {
		attempt.Message = "malformed packet: " + err.Error()
		return nil, attempt
	}
	attempt.User = string(attributeValue(packet.attributes, attrUserName))
	if packet.code != radiusAccessRequest {
		attempt.Message = "unsupported RADIUS code"
		return nil, attempt
	}
	secret := []byte(cfg.SharedSecret)
	if len(secret) == 0 {
		attempt.Message = "shared secret is not configured"
		return responseFor(&packet, radiusAccessReject, nil, secret), attempt
	}
	if messageAuth := attribute(packet.attributes, attrMessageAuth); messageAuth != nil {
		if len(messageAuth.value) != 16 || !verifyMessageAuthenticator(data, secret) {
			attempt.Message = "invalid Message-Authenticator"
			return responseFor(&packet, radiusAccessReject, nil, secret), attempt
		}
	} else {
		attempt.Message = "warning: request has no Message-Authenticator"
	}

	passwordBytes := attributeValue(packet.attributes, attrUserPassword)
	password, err := decodeUserPassword(passwordBytes, packet.authenticator, secret)
	if err != nil {
		attempt.Message = "invalid User-Password: " + err.Error()
		return responseFor(&packet, radiusAccessReject, nil, secret), attempt
	}
	for _, user := range cfg.Users {
		if user.Username != attempt.User || user.Password != password {
			continue
		}
		attempt.Result = "accept"
		attempt.Message = "Access-Accept"
		return responseFor(&packet, radiusAccessAccept, acceptAttributes(user), secret), attempt
	}
	attempt.Message = "Access-Reject: invalid credentials"
	return responseFor(&packet, radiusAccessReject, nil, secret), attempt
}

func acceptAttributes(user User) []radiusAttribute {
	attrs := []radiusAttribute{{typ: attrServiceType, value: uint32Bytes(serviceType(user.Service))}}
	if user.PrivLvl > 0 {
		value := fmt.Sprintf("shell:priv-lvl=%d", user.PrivLvl)
		vsa := make([]byte, 6+len(value))
		binary.BigEndian.PutUint32(vsa, ciscoVendorID)
		vsa[4] = ciscoAVPairVendorType
		vsa[5] = byte(2 + len(value))
		copy(vsa[6:], value)
		attrs = append(attrs, radiusAttribute{typ: attrVendorSpecific, value: vsa})
	}
	return attrs
}

func serviceType(service string) uint32 {
	switch strings.ToLower(strings.TrimSpace(service)) {
	case "admin", "administrative":
		return 6
	case "nas-prompt":
		return 7
	default:
		return 1 // Login
	}
}

func uint32Bytes(value uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, value)
	return b
}

func responseFor(request *radiusPacket, code byte, attrs []radiusAttribute, secret []byte) []byte {
	if attrs == nil {
		attrs = []radiusAttribute{{typ: attrReplyMessage, value: []byte("Access-Reject")}}
	}
	encodedAttrs := encodeAttributes(attrs)
	length := 20 + len(encodedAttrs)
	packet := make([]byte, length)
	packet[0] = code
	packet[1] = request.identifier
	binary.BigEndian.PutUint16(packet[2:4], uint16(length))
	copy(packet[4:20], request.authenticator[:])
	copy(packet[20:], encodedAttrs)
	h := md5.New()
	_, _ = h.Write(packet[:4])
	_, _ = h.Write(request.authenticator[:])
	_, _ = h.Write(encodedAttrs)
	_, _ = h.Write(secret)
	copy(packet[4:20], h.Sum(nil))
	return packet
}

func parseRadiusPacket(data []byte) (radiusPacket, error) {
	var packet radiusPacket
	if len(data) < 20 {
		return packet, errors.New("packet shorter than RADIUS header")
	}
	length := int(binary.BigEndian.Uint16(data[2:4]))
	if length < 20 || length > len(data) {
		return packet, errors.New("invalid packet length")
	}
	packet.code = data[0]
	packet.identifier = data[1]
	copy(packet.authenticator[:], data[4:20])
	for offset := 20; offset < length; {
		if offset+2 > length {
			return packet, errors.New("truncated attribute header")
		}
		attrLen := int(data[offset+1])
		if attrLen < 2 || offset+attrLen > length {
			return packet, errors.New("invalid attribute length")
		}
		packet.attributes = append(packet.attributes, radiusAttribute{
			typ:   data[offset],
			value: append([]byte(nil), data[offset+2:offset+attrLen]...),
		})
		offset += attrLen
	}
	return packet, nil
}

func attribute(attrs []radiusAttribute, typ byte) *radiusAttribute {
	for i := range attrs {
		if attrs[i].typ == typ {
			return &attrs[i]
		}
	}
	return nil
}

func attributeValue(attrs []radiusAttribute, typ byte) []byte {
	if attr := attribute(attrs, typ); attr != nil {
		return attr.value
	}
	return nil
}

func encodeAttributes(attrs []radiusAttribute) []byte {
	var out []byte
	for _, attr := range attrs {
		if len(attr.value) > 253 {
			continue
		}
		out = append(out, attr.typ, byte(len(attr.value)+2))
		out = append(out, attr.value...)
	}
	return out
}

func verifyMessageAuthenticator(packet []byte, secret []byte) bool {
	if len(packet) < 20 {
		return false
	}
	length := int(binary.BigEndian.Uint16(packet[2:4]))
	if length < 20 || length > len(packet) {
		return false
	}
	copyPacket := append([]byte(nil), packet[:length]...)
	var expected []byte
	for offset := 20; offset < length; {
		attrLen := int(copyPacket[offset+1])
		if attrLen < 2 || offset+attrLen > length {
			return false
		}
		if copyPacket[offset] == attrMessageAuth {
			if attrLen != 18 {
				return false
			}
			expected = append([]byte(nil), copyPacket[offset+2:offset+18]...)
			for i := offset + 2; i < offset+18; i++ {
				copyPacket[i] = 0
			}
		}
		offset += attrLen
	}
	if len(expected) != 16 {
		return false
	}
	h := hmac.New(md5.New, secret)
	_, _ = h.Write(copyPacket)
	return hmac.Equal(expected, h.Sum(nil))
}

func decodeUserPassword(ciphertext []byte, requestAuthenticator [16]byte, secret []byte) (string, error) {
	if len(ciphertext) == 0 || len(ciphertext)%16 != 0 {
		return "", errors.New("password attribute is not a multiple of 16 bytes")
	}
	if len(ciphertext) > 128 {
		return "", errors.New("password is longer than 128 bytes")
	}
	plain := make([]byte, len(ciphertext))
	var previous []byte
	for offset := 0; offset < len(ciphertext); offset += 16 {
		h := md5.New()
		_, _ = h.Write(secret)
		if offset == 0 {
			_, _ = h.Write(requestAuthenticator[:])
		} else {
			_, _ = h.Write(previous)
		}
		xorBlock(plain[offset:offset+16], ciphertext[offset:offset+16], h)
		previous = ciphertext[offset : offset+16]
	}
	return string(trimZeroPadding(plain)), nil
}

func xorBlock(dst, src []byte, h hash.Hash) {
	mask := h.Sum(nil)
	for i := range src {
		dst[i] = src[i] ^ mask[i]
	}
}

func trimZeroPadding(value []byte) []byte {
	for len(value) > 0 && value[len(value)-1] == 0 {
		value = value[:len(value)-1]
	}
	return value
}

func encodeUserPassword(password string, authenticator [16]byte, secret []byte) []byte {
	plain := []byte(password)
	length := ((len(plain) / 16) + 1) * 16
	padded := make([]byte, length)
	copy(padded, plain)
	ciphertext := make([]byte, length)
	var previous []byte
	for offset := 0; offset < length; offset += 16 {
		h := md5.New()
		_, _ = h.Write(secret)
		if offset == 0 {
			_, _ = h.Write(authenticator[:])
		} else {
			_, _ = h.Write(previous)
		}
		xorBlock(ciphertext[offset:offset+16], padded[offset:offset+16], h)
		previous = ciphertext[offset : offset+16]
	}
	return ciphertext
}

func buildAccessRequest(identifier byte, authenticator [16]byte, username, password, secret string) []byte {
	if authenticator == [16]byte{} {
		_, _ = rand.Read(authenticator[:])
	}
	attrs := encodeAttributes([]radiusAttribute{
		{typ: attrUserName, value: []byte(username)},
		{typ: attrUserPassword, value: encodeUserPassword(password, authenticator, []byte(secret))},
	})
	packet := make([]byte, 20+len(attrs))
	packet[0] = radiusAccessRequest
	packet[1] = identifier
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	copy(packet[4:20], authenticator[:])
	copy(packet[20:], attrs)
	return packet
}
