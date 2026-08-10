package main

import (
	"crypto/md5" // TACACS+ RFC 8907 specifies this legacy obfuscation.
	"encoding/binary"
	"errors"
	"fmt"
)

// TACACS+ in this lab pack deliberately has one shared key for all clients.
// Config.Clients remains available for the existing RADIUS MVP, but RFC 8907
// deployments should use a per-client key lookup keyed by source address.

const (
	tacacsVersionMajor = 0xc
	tacacsVersion      = 0xc0

	tacacsTypeAuthen = 0x01
	tacacsTypeAuthor = 0x02
	tacacsTypeAcct   = 0x03

	tacacsFlagUnencrypted   = 0x01
	tacacsFlagSingleConnect = 0x04

	tacacsAuthenLogin    = 0x01
	tacacsAuthenChpass   = 0x02
	tacacsAuthenSendauth = 0x04

	tacacsAuthenASCII    = 0x01
	tacacsAuthenPAP      = 0x02
	tacacsAuthenCHAP     = 0x03
	tacacsAuthenMSCHAP   = 0x05
	tacacsAuthenMSCHAPV2 = 0x06

	tacacsAuthenMethodTACACSPlus = 0x06

	tacacsAuthenPass    = 0x01
	tacacsAuthenFail    = 0x02
	tacacsAuthenGetData = 0x03
	tacacsAuthenGetUser = 0x04
	tacacsAuthenGetPass = 0x05
	tacacsAuthenRestart = 0x06
	tacacsAuthenError   = 0x07
	tacacsAuthenFollow  = 0x21
	tacacsAuthenNoEcho  = 0x01

	tacacsAuthorPassAdd  = 0x01
	tacacsAuthorPassRepl = 0x02
	tacacsAuthorFail     = 0x10
	tacacsAuthorError    = 0x11
	tacacsAuthorFollow   = 0x21

	tacacsAcctStart    = 0x02
	tacacsAcctStop     = 0x04
	tacacsAcctWatchdog = 0x08
	tacacsAcctSuccess  = 0x01
	tacacsAcctError    = 0x02
	tacacsAcctFollow   = 0x21
	tacacsAcctAbort    = 0x01
)

type tacacsHeader struct {
	Version   byte
	Type      byte
	SeqNo     byte
	Flags     byte
	SessionID uint32
	Length    uint32
}

type tacacsPacket struct {
	Header tacacsHeader
	Body   []byte
}

func encodeTacacsHeader(h tacacsHeader) ([]byte, error) {
	if h.Version>>4 != tacacsVersionMajor || h.Version&0x0f > 1 {
		return nil, errors.New("unsupported TACACS+ version")
	}
	if h.Type < tacacsTypeAuthen || h.Type > tacacsTypeAcct {
		return nil, errors.New("unsupported TACACS+ packet type")
	}
	if h.SeqNo == 0 || h.SeqNo == 255 {
		return nil, errors.New("invalid TACACS+ sequence number")
	}
	if h.Flags & ^byte(tacacsFlagUnencrypted|tacacsFlagSingleConnect) != 0 {
		return nil, errors.New("invalid TACACS+ flags")
	}
	if h.Length > 65535 {
		return nil, errors.New("TACACS+ body is too large")
	}
	header := make([]byte, 12)
	header[0] = h.Version
	header[1] = h.Type
	header[2] = h.SeqNo
	header[3] = h.Flags
	binary.BigEndian.PutUint32(header[4:8], h.SessionID)
	binary.BigEndian.PutUint32(header[8:12], h.Length)
	return header, nil
}

func decodeTacacsHeader(data []byte) (tacacsHeader, error) {
	var h tacacsHeader
	if len(data) < 12 {
		return h, errors.New("TACACS+ header is shorter than 12 bytes")
	}
	h = tacacsHeader{
		Version: data[0], Type: data[1], SeqNo: data[2], Flags: data[3],
		SessionID: binary.BigEndian.Uint32(data[4:8]),
		Length:    binary.BigEndian.Uint32(data[8:12]),
	}
	if h.Version>>4 != tacacsVersionMajor {
		return h, fmt.Errorf("unsupported TACACS+ major version 0x%x", h.Version>>4)
	}
	if h.Version&0x0f > 1 {
		return h, fmt.Errorf("unsupported TACACS+ minor version %d", h.Version&0x0f)
	}
	if h.Type < tacacsTypeAuthen || h.Type > tacacsTypeAcct {
		return h, fmt.Errorf("unsupported TACACS+ packet type 0x%x", h.Type)
	}
	if h.SeqNo == 0 || h.SeqNo == 255 {
		return h, errors.New("invalid TACACS+ sequence number")
	}
	if h.Flags & ^byte(tacacsFlagUnencrypted|tacacsFlagSingleConnect) != 0 {
		return h, errors.New("invalid TACACS+ flags")
	}
	if h.Length > 65535 {
		return h, errors.New("TACACS+ body is too large")
	}
	return h, nil
}

// tacacsPad implements RFC 8907's MD5 chain. The session ID is serialized in
// the same big-endian four bytes that appear in the packet header.
func tacacsPad(sessionID uint32, key []byte, version, seqNo byte, length int) []byte {
	if length <= 0 {
		return nil
	}
	session := make([]byte, 4)
	binary.BigEndian.PutUint32(session, sessionID)
	seed := make([]byte, 0, 6+len(key))
	seed = append(seed, session...)
	seed = append(seed, key...)
	seed = append(seed, version, seqNo)

	pad := make([]byte, 0, length)
	var previous []byte
	for len(pad) < length {
		h := md5.New()
		_, _ = h.Write(seed)
		if previous != nil {
			_, _ = h.Write(previous)
		}
		previous = h.Sum(nil)
		pad = append(pad, previous...)
	}
	return pad[:length]
}

func encodeTacacsPacket(h tacacsHeader, plaintext, key []byte) ([]byte, error) {
	if len(plaintext) > 65535 {
		return nil, errors.New("TACACS+ body is too large")
	}
	h.Length = uint32(len(plaintext))
	header, err := encodeTacacsHeader(h)
	if err != nil {
		return nil, err
	}
	body := make([]byte, len(plaintext))
	pad := tacacsPad(h.SessionID, key, h.Version, h.SeqNo, len(body))
	for i := range body {
		body[i] = plaintext[i] ^ pad[i]
	}
	return append(header, body...), nil
}

func decodeTacacsPacket(data, key []byte) (tacacsPacket, error) {
	var packet tacacsPacket
	h, err := decodeTacacsHeader(data)
	packet.Header = h
	if err != nil {
		return packet, err
	}
	if len(data) != 12+int(h.Length) {
		return packet, errors.New("TACACS+ packet length does not match its header")
	}
	if h.Flags&tacacsFlagUnencrypted != 0 {
		return packet, errors.New("client sent unencrypted body — refusing")
	}
	if len(key) == 0 {
		return packet, errors.New("TACACS+ shared key is empty")
	}
	body := make([]byte, h.Length)
	pad := tacacsPad(h.SessionID, key, h.Version, h.SeqNo, len(body))
	for i := range body {
		body[i] = data[12+i] ^ pad[i]
	}
	packet.Body = body
	return packet, nil
}

func validateTacacsRequestVersion(packet tacacsPacket) error {
	if packet.Header.Version&0x0f == 0 {
		return nil
	}
	// RFC version 1 is reserved for one-shot non-ASCII AUTHEN START. This
	// server intentionally supports ASCII only, so every version-1 request is
	// a version negotiation error and receives ERROR rather than FAIL.
	return errors.New("TACACS+ minor version 1 is unsupported by the ASCII-only server")
}

type tacacsAuthenStart struct {
	Action        byte
	PrivLvl       byte
	AuthenType    byte
	AuthenService byte
	User          string
	Port          string
	RemoteAddr    string
	Data          string
}

type tacacsAuthenReply struct {
	Status    byte
	Flags     byte
	ServerMsg string
	Data      string
}

type tacacsAuthenContinue struct {
	UserMsg string
	Data    string
	Flags   byte
}

type tacacsAuthorRequest struct {
	AuthenMethod  byte
	PrivLvl       byte
	AuthenType    byte
	AuthenService byte
	User          string
	Port          string
	RemoteAddr    string
	Args          []string
}

type tacacsAuthorReply struct {
	Status    byte
	ServerMsg string
	Data      string
	Args      []string
}

type tacacsAcctRequest struct {
	Flags         byte
	AuthenMethod  byte
	PrivLvl       byte
	AuthenType    byte
	AuthenService byte
	User          string
	Port          string
	RemoteAddr    string
	Args          []string
}

type tacacsAcctReply struct {
	Status    byte
	ServerMsg string
	Data      string
}

func encodeAuthenStart(start tacacsAuthenStart) ([]byte, error) {
	strings := []string{start.User, start.Port, start.RemoteAddr, start.Data}
	lengths, err := byteLengths(strings)
	if err != nil {
		return nil, err
	}
	if start.Action != tacacsAuthenLogin && start.Action != tacacsAuthenChpass && start.Action != tacacsAuthenSendauth {
		return nil, errors.New("invalid AUTHEN action")
	}
	if !validAuthenType(start.AuthenType) {
		return nil, errors.New("invalid AUTHEN type")
	}
	body := []byte{start.Action, start.PrivLvl, start.AuthenType, start.AuthenService}
	body = append(body, lengths...)
	for _, value := range strings {
		body = append(body, []byte(value)...)
	}
	return body, nil
}

func decodeAuthenStart(body []byte) (tacacsAuthenStart, error) {
	var start tacacsAuthenStart
	if len(body) < 8 {
		return start, errors.New("AUTHEN START is shorter than 8 bytes")
	}
	start = tacacsAuthenStart{Action: body[0], PrivLvl: body[1], AuthenType: body[2], AuthenService: body[3]}
	if start.Action != tacacsAuthenLogin && start.Action != tacacsAuthenChpass && start.Action != tacacsAuthenSendauth {
		return start, errors.New("invalid AUTHEN action")
	}
	if !validAuthenType(start.AuthenType) {
		return start, errors.New("invalid AUTHEN type")
	}
	values, err := decodeFixedLengthStrings(body, 4, 1, 4, 8)
	if err != nil {
		return start, err
	}
	start.User, start.Port, start.RemoteAddr, start.Data = values[0], values[1], values[2], values[3]
	return start, nil
}

func encodeAuthenReply(reply tacacsAuthenReply) ([]byte, error) {
	if !validAuthenStatus(reply.Status) {
		return nil, errors.New("invalid AUTHEN reply status")
	}
	if reply.Flags&^byte(tacacsAuthenNoEcho) != 0 {
		return nil, errors.New("invalid AUTHEN reply flags")
	}
	if reply.Status == tacacsAuthenGetPass && reply.Flags&tacacsAuthenNoEcho == 0 {
		return nil, errors.New("GETPASS must set NOECHO")
	}
	if len(reply.ServerMsg) > 65535 || len(reply.Data) > 65535 {
		return nil, errors.New("AUTHEN reply string is too large")
	}
	body := make([]byte, 6, 6+len(reply.ServerMsg)+len(reply.Data))
	body[0], body[1] = reply.Status, reply.Flags
	binary.BigEndian.PutUint16(body[2:4], uint16(len(reply.ServerMsg)))
	binary.BigEndian.PutUint16(body[4:6], uint16(len(reply.Data)))
	body = append(body, reply.ServerMsg...)
	body = append(body, reply.Data...)
	return body, nil
}

func decodeAuthenReply(body []byte) (tacacsAuthenReply, error) {
	var reply tacacsAuthenReply
	if len(body) < 6 {
		return reply, errors.New("AUTHEN REPLY is shorter than 6 bytes")
	}
	reply.Status, reply.Flags = body[0], body[1]
	if !validAuthenStatus(reply.Status) || reply.Flags&^byte(tacacsAuthenNoEcho) != 0 {
		return reply, errors.New("invalid AUTHEN reply fields")
	}
	values, err := decodeFixedLengthStrings(body, 2, 2, 2, 6)
	if err != nil {
		return reply, err
	}
	reply.ServerMsg, reply.Data = values[0], values[1]
	if reply.Status == tacacsAuthenGetPass && reply.Flags&tacacsAuthenNoEcho == 0 {
		return reply, errors.New("GETPASS without NOECHO")
	}
	return reply, nil
}

func encodeAuthenContinue(continuePacket tacacsAuthenContinue) ([]byte, error) {
	if len(continuePacket.UserMsg) > 65535 || len(continuePacket.Data) > 65535 {
		return nil, errors.New("AUTHEN CONTINUE string is too large")
	}
	if continuePacket.Flags&^byte(tacacsAcctAbort) != 0 {
		return nil, errors.New("invalid AUTHEN CONTINUE flags")
	}
	body := make([]byte, 5, 5+len(continuePacket.UserMsg)+len(continuePacket.Data))
	binary.BigEndian.PutUint16(body[0:2], uint16(len(continuePacket.UserMsg)))
	binary.BigEndian.PutUint16(body[2:4], uint16(len(continuePacket.Data)))
	body[4] = continuePacket.Flags
	body = append(body, continuePacket.UserMsg...)
	body = append(body, continuePacket.Data...)
	return body, nil
}

func decodeAuthenContinue(body []byte) (tacacsAuthenContinue, error) {
	var continuePacket tacacsAuthenContinue
	if len(body) < 5 {
		return continuePacket, errors.New("AUTHEN CONTINUE is shorter than 5 bytes")
	}
	continuePacket.Flags = body[4]
	if continuePacket.Flags&^byte(tacacsAcctAbort) != 0 {
		return continuePacket, errors.New("invalid AUTHEN CONTINUE flags")
	}
	values, err := decodeFixedLengthStrings(body, 0, 2, 2, 5)
	if err != nil {
		return continuePacket, err
	}
	continuePacket.UserMsg, continuePacket.Data = values[0], values[1]
	return continuePacket, nil
}

func encodeAuthorRequest(request tacacsAuthorRequest) ([]byte, error) {
	if !validAuthenType(request.AuthenType) {
		return nil, errors.New("invalid AUTHOR authentication type")
	}
	if len(request.Args) > 255 {
		return nil, errors.New("too many AUTHOR arguments")
	}
	parts := append([]string{request.User, request.Port, request.RemoteAddr}, request.Args...)
	lengths, err := byteLengths(parts)
	if err != nil {
		return nil, err
	}
	body := []byte{request.AuthenMethod, request.PrivLvl, request.AuthenType, request.AuthenService, lengths[0], lengths[1], lengths[2], byte(len(request.Args))}
	body = append(body, lengths[3:]...)
	for _, value := range parts {
		body = append(body, []byte(value)...)
	}
	return body, nil
}

func decodeAuthorRequest(body []byte) (tacacsAuthorRequest, error) {
	var request tacacsAuthorRequest
	if len(body) < 8 {
		return request, errors.New("AUTHOR REQUEST is shorter than 8 bytes")
	}
	request = tacacsAuthorRequest{AuthenMethod: body[0], PrivLvl: body[1], AuthenType: body[2], AuthenService: body[3]}
	if !validAuthenType(request.AuthenType) {
		return request, errors.New("invalid AUTHOR authentication type")
	}
	argCount := int(body[7])
	values, err := decodeRequestParts(body, 4, 7, 8, 8+argCount)
	if err != nil {
		return request, err
	}
	request.User, request.Port, request.RemoteAddr = values[0], values[1], values[2]
	request.Args = append([]string(nil), values[3:]...)
	return request, nil
}

func encodeAuthorReply(reply tacacsAuthorReply) ([]byte, error) {
	if !validAuthorStatus(reply.Status) {
		return nil, errors.New("invalid AUTHOR reply status")
	}
	if len(reply.Args) > 255 || len(reply.ServerMsg) > 65535 || len(reply.Data) > 65535 {
		return nil, errors.New("AUTHOR reply is too large")
	}
	argLengths, err := byteLengths(reply.Args)
	if err != nil {
		return nil, err
	}
	body := []byte{reply.Status, byte(len(reply.Args)), 0, 0, 0, 0}
	binary.BigEndian.PutUint16(body[2:4], uint16(len(reply.ServerMsg)))
	binary.BigEndian.PutUint16(body[4:6], uint16(len(reply.Data)))
	body = append(body, argLengths...)
	body = append(body, reply.ServerMsg...)
	body = append(body, reply.Data...)
	for _, arg := range reply.Args {
		body = append(body, arg...)
	}
	return body, nil
}

func decodeAuthorReply(body []byte) (tacacsAuthorReply, error) {
	var reply tacacsAuthorReply
	if len(body) < 6 {
		return reply, errors.New("AUTHOR REPLY is shorter than 6 bytes")
	}
	reply.Status = body[0]
	if !validAuthorStatus(reply.Status) {
		return reply, errors.New("invalid AUTHOR reply status")
	}
	argCount := int(body[1])
	serverMsgLen := int(binary.BigEndian.Uint16(body[2:4]))
	dataLen := int(binary.BigEndian.Uint16(body[4:6]))
	argsStart := 6 + argCount
	if argsStart > len(body) {
		return reply, errors.New("AUTHOR REPLY argument lengths are truncated")
	}
	offset := argsStart
	if serverMsgLen > len(body)-offset {
		return reply, errors.New("AUTHOR REPLY server message exceeds body")
	}
	reply.ServerMsg = string(body[offset : offset+serverMsgLen])
	offset += serverMsgLen
	if dataLen > len(body)-offset {
		return reply, errors.New("AUTHOR REPLY data exceeds body")
	}
	reply.Data = string(body[offset : offset+dataLen])
	offset += dataLen
	reply.Args = make([]string, argCount)
	for i := 0; i < argCount; i++ {
		length := int(body[6+i])
		if length > len(body)-offset {
			return reply, errors.New("AUTHOR REPLY argument exceeds body")
		}
		reply.Args[i] = string(body[offset : offset+length])
		offset += length
	}
	if offset != len(body) {
		return reply, errors.New("AUTHOR REPLY fields do not consume the body")
	}
	return reply, nil
}

func encodeAcctRequest(request tacacsAcctRequest) ([]byte, error) {
	if len(request.Args) > 255 {
		return nil, errors.New("too many ACCT arguments")
	}
	parts := append([]string{request.User, request.Port, request.RemoteAddr}, request.Args...)
	lengths, err := byteLengths(parts)
	if err != nil {
		return nil, err
	}
	body := []byte{request.Flags, request.AuthenMethod, request.PrivLvl, request.AuthenType, request.AuthenService, lengths[0], lengths[1], lengths[2], byte(len(request.Args))}
	body = append(body, lengths[3:]...)
	for _, value := range parts {
		body = append(body, []byte(value)...)
	}
	return body, nil
}

func decodeAcctRequest(body []byte) (tacacsAcctRequest, error) {
	var request tacacsAcctRequest
	if len(body) < 9 {
		return request, errors.New("ACCT REQUEST is shorter than 9 bytes")
	}
	request = tacacsAcctRequest{Flags: body[0], AuthenMethod: body[1], PrivLvl: body[2], AuthenType: body[3], AuthenService: body[4]}
	if !validAuthenType(request.AuthenType) {
		return request, errors.New("invalid ACCT authentication type")
	}
	argCount := int(body[8])
	values, err := decodeRequestParts(body, 5, 8, 9, 9+argCount)
	if err != nil {
		return request, err
	}
	request.User, request.Port, request.RemoteAddr = values[0], values[1], values[2]
	request.Args = append([]string(nil), values[3:]...)
	return request, nil
}

func encodeAcctReply(reply tacacsAcctReply) ([]byte, error) {
	if !validAcctStatus(reply.Status) {
		return nil, errors.New("invalid ACCT reply status")
	}
	if len(reply.ServerMsg) > 65535 || len(reply.Data) > 65535 {
		return nil, errors.New("ACCT reply is too large")
	}
	body := []byte{0, 0, 0, 0, reply.Status}
	binary.BigEndian.PutUint16(body[0:2], uint16(len(reply.ServerMsg)))
	binary.BigEndian.PutUint16(body[2:4], uint16(len(reply.Data)))
	body = append(body, reply.ServerMsg...)
	body = append(body, reply.Data...)
	return body, nil
}

func decodeAcctReply(body []byte) (tacacsAcctReply, error) {
	var reply tacacsAcctReply
	if len(body) < 5 {
		return reply, errors.New("ACCT REPLY is shorter than 5 bytes")
	}
	reply.Status = body[4]
	if !validAcctStatus(reply.Status) {
		return reply, errors.New("invalid ACCT reply status")
	}
	serverMsgLen := int(binary.BigEndian.Uint16(body[0:2]))
	dataLen := int(binary.BigEndian.Uint16(body[2:4]))
	offset := 5
	if serverMsgLen > len(body)-offset {
		return reply, errors.New("ACCT REPLY server message exceeds body")
	}
	reply.ServerMsg = string(body[offset : offset+serverMsgLen])
	offset += serverMsgLen
	if dataLen > len(body)-offset {
		return reply, errors.New("ACCT REPLY data exceeds body")
	}
	reply.Data = string(body[offset : offset+dataLen])
	offset += dataLen
	if offset != len(body) {
		return reply, errors.New("ACCT REPLY fields do not consume the body")
	}
	return reply, nil
}

func byteLengths(values []string) ([]byte, error) {
	lengths := make([]byte, len(values))
	for i, value := range values {
		if len(value) > 255 {
			return nil, errors.New("TACACS+ request field is too long")
		}
		lengths[i] = byte(len(value))
	}
	return lengths, nil
}

func decodeFixedLengthStrings(body []byte, lengthsOffset, width, count, dataOffset int) ([]string, error) {
	if lengthsOffset < 0 || width <= 0 || count <= 0 || dataOffset < 0 || lengthsOffset+count*width > len(body) {
		return nil, errors.New("TACACS+ length fields are truncated")
	}
	if lengthsOffset+count*width > dataOffset {
		return nil, errors.New("invalid TACACS+ length field region")
	}
	if dataOffset > len(body) {
		return nil, errors.New("TACACS+ data starts past body")
	}
	values := make([]string, count)
	offset := dataOffset
	for i := 0; i < count; i++ {
		var length int
		switch width {
		case 1:
			length = int(body[lengthsOffset+i])
		case 2:
			start := lengthsOffset + i*2
			if start+2 > len(body) {
				return nil, errors.New("TACACS+ length field is truncated")
			}
			length = int(binary.BigEndian.Uint16(body[start : start+2]))
		default:
			return nil, errors.New("unsupported TACACS+ length width")
		}
		if length > len(body)-offset {
			return nil, errors.New("TACACS+ field length exceeds body")
		}
		values[i] = string(body[offset : offset+length])
		offset += length
	}
	if offset != len(body) {
		return nil, errors.New("TACACS+ fields do not consume the body")
	}
	return values, nil
}

func decodeRequestParts(body []byte, baseLengthsOffset, argCountOffset, argLengthsOffset, dataOffset int) ([]string, error) {
	if argCountOffset < 0 || argCountOffset >= len(body) {
		return nil, errors.New("TACACS+ argument count is truncated")
	}
	argCount := int(body[argCountOffset])
	if baseLengthsOffset < 0 || baseLengthsOffset+3 > len(body) || argLengthsOffset+argCount > len(body) || dataOffset > len(body) {
		return nil, errors.New("TACACS+ request lengths are truncated")
	}
	lengths := make([]int, 0, 3+argCount)
	for i := 0; i < 3; i++ {
		lengths = append(lengths, int(body[baseLengthsOffset+i]))
	}
	for i := 0; i < argCount; i++ {
		lengths = append(lengths, int(body[argLengthsOffset+i]))
	}
	values := make([]string, len(lengths))
	offset := dataOffset
	for i, length := range lengths {
		if length > len(body)-offset {
			return nil, errors.New("TACACS+ field length exceeds body")
		}
		values[i] = string(body[offset : offset+length])
		offset += length
	}
	if offset != len(body) {
		return nil, errors.New("TACACS+ fields do not consume the body")
	}
	return values, nil
}

func validAuthenType(value byte) bool {
	return value == tacacsAuthenASCII || value == tacacsAuthenPAP || value == tacacsAuthenCHAP || value == tacacsAuthenMSCHAP || value == tacacsAuthenMSCHAPV2
}

func validAuthenStatus(value byte) bool {
	return value == tacacsAuthenPass || value == tacacsAuthenFail || value == tacacsAuthenGetData || value == tacacsAuthenGetUser || value == tacacsAuthenGetPass || value == tacacsAuthenRestart || value == tacacsAuthenError || value == tacacsAuthenFollow
}

func validAuthorStatus(value byte) bool {
	return value == tacacsAuthorPassAdd || value == tacacsAuthorPassRepl || value == tacacsAuthorFail || value == tacacsAuthorError || value == tacacsAuthorFollow
}

func validAcctStatus(value byte) bool {
	return value == tacacsAcctSuccess || value == tacacsAcctError || value == tacacsAcctFollow
}

func validAcctFlags(flags byte) bool {
	if flags&^byte(tacacsAcctStart|tacacsAcctStop|tacacsAcctWatchdog) != 0 {
		return false
	}
	switch flags {
	case tacacsAcctStart, tacacsAcctStop, tacacsAcctWatchdog, tacacsAcctStart | tacacsAcctWatchdog:
		return true
	default:
		return false
	}
}
