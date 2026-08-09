package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	tacacsMaxConnections = 64
	tacacsReadTimeout    = 30 * time.Second

	authenStageStart        = 0
	authenStageNeedUsername = 1
	authenStageNeedPassword = 2
)

type TacacsServer struct {
	store *Store
	ring  *attemptRing

	mu       sync.RWMutex
	listener net.Listener
	conns    map[net.Conn]struct{}
}

func NewTacacsServer(store *Store) *TacacsServer {
	return &TacacsServer{store: store, ring: newRing(100), conns: make(map[net.Conn]struct{})}
}

func (s *TacacsServer) Attempts() []AuthAttempt { return s.ring.List() }

func (s *TacacsServer) Serve(addr string) error {
	if _, ok := tacacsKey(s.store.Snapshot()); !ok {
		return errors.New("no TACACS+ key configured; TACACS+ listener refused to start")
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.listener == listener {
			s.listener = nil
		}
		s.mu.Unlock()
	}()

	semaphore := make(chan struct{}, tacacsMaxConnections)
	for {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			if errors.Is(acceptErr, net.ErrClosed) {
				return nil
			}
			return acceptErr
		}
		select {
		case semaphore <- struct{}{}:
			s.trackConn(conn)
			go func() {
				defer func() {
					<-semaphore
					s.untrackConn(conn)
				}()
				s.handleConn(conn)
			}()
		default:
			log.Printf("aaa: TACACS+ connection limit reached; dropping %s", conn.RemoteAddr())
			_ = conn.Close()
		}
	}
}

func (s *TacacsServer) Addr() net.Addr {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

func (s *TacacsServer) Close() error {
	s.mu.Lock()
	listener := s.listener
	connections := make([]net.Conn, 0, len(s.conns))
	for conn := range s.conns {
		connections = append(connections, conn)
	}
	s.mu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}
	for _, conn := range connections {
		_ = conn.Close()
	}
	return nil
}

func (s *TacacsServer) trackConn(conn net.Conn) {
	s.mu.Lock()
	s.conns[conn] = struct{}{}
	s.mu.Unlock()
}

func (s *TacacsServer) untrackConn(conn net.Conn) {
	s.mu.Lock()
	delete(s.conns, conn)
	s.mu.Unlock()
}

type tacacsSession struct {
	initialized bool
	sessionID   uint32
	version     byte
	lastSeq     byte
	stage       int
	username    string
}

func (s *TacacsServer) handleConn(conn net.Conn) {
	defer conn.Close()
	remote := "unknown"
	if conn.RemoteAddr() != nil {
		remote = conn.RemoteAddr().String()
	}
	state := tacacsSession{}
	for {
		_ = conn.SetReadDeadline(time.Now().Add(tacacsReadTimeout))
	headerRead:
		headerBytes := make([]byte, 12)
		if _, err := io.ReadFull(conn, headerBytes); err != nil {
			if !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
				log.Printf("aaa: TACACS+ read from %s: %v", remote, err)
			}
			return
		}
		header, err := decodeTacacsHeader(headerBytes)
		if err != nil {
			return
		}
		bodyBytes := make([]byte, int(header.Length))
		if _, err := io.ReadFull(conn, bodyBytes); err != nil {
			log.Printf("aaa: TACACS+ body read from %s: %v", remote, err)
			return
		}
		packetBytes := append(headerBytes, bodyBytes...)
		if err := state.acceptHeader(header); err != nil {
			s.record(remote, "", "reject", "protocol error: "+err.Error())
			_ = s.writeError(conn, header, tacacsKeyOrEmpty(s.store.Snapshot()), err)
			return
		}
		cfg := s.store.Snapshot()
		key, configured := tacacsKey(cfg)
		if !configured {
			s.record(remote, "", "reject", "no TACACS+ key configured; refusing request")
			return
		}
		packet, err := decodeTacacsPacket(packetBytes, key)
		if err != nil {
			s.record(remote, "", "reject", err.Error())
			_ = s.writeError(conn, header, key, err)
			return
		}
		if err := validateTacacsRequestVersion(packet); err != nil {
			s.record(remote, "", "reject", err.Error())
			_ = s.writeError(conn, header, key, err)
			return
		}

		final, err := s.handlePacket(conn, packet, cfg, &state, remote)
		if err != nil {
			s.record(remote, state.username, "reject", err.Error())
			_ = s.writeError(conn, header, key, err)
			return
		}
		if final {
			return
		}
		// Keep the label to make it obvious that the next iteration must read a
		// fresh 12-byte header; TCP may split or coalesce packet writes.
		goto headerRead
	}
}

func (state *tacacsSession) acceptHeader(header tacacsHeader) error {
	if header.SeqNo%2 == 0 {
		return errors.New("client sequence number must be odd")
	}
	if !state.initialized {
		if header.SeqNo != 1 {
			return fmt.Errorf("first client sequence number is %d, want 1", header.SeqNo)
		}
		state.initialized = true
		state.sessionID = header.SessionID
		state.version = header.Version
		state.lastSeq = header.SeqNo
		return nil
	}
	if header.SessionID != state.sessionID {
		return errors.New("session ID changed during a connection")
	}
	if header.Version != state.version {
		return errors.New("TACACS+ version changed during a connection")
	}
	if state.lastSeq >= 253 || header.SeqNo != state.lastSeq+2 {
		return fmt.Errorf("out-of-order client sequence number %d after %d", header.SeqNo, state.lastSeq)
	}
	state.lastSeq = header.SeqNo
	return nil
}

func (s *TacacsServer) handlePacket(conn net.Conn, packet tacacsPacket, cfg Config, state *tacacsSession, remote string) (bool, error) {
	switch packet.Header.Type {
	case tacacsTypeAuthen:
		return s.handleAuthen(conn, packet, cfg, state, remote)
	case tacacsTypeAuthor:
		return s.handleAuthor(conn, packet, cfg, state, remote)
	case tacacsTypeAcct:
		return s.handleAcct(conn, packet, cfg, state, remote)
	default:
		return true, errors.New("unsupported TACACS+ packet type")
	}
}

func (s *TacacsServer) handleAuthen(conn net.Conn, packet tacacsPacket, cfg Config, state *tacacsSession, remote string) (bool, error) {
	if state.stage == authenStageStart {
		start, err := decodeAuthenStart(packet.Body)
		if err != nil {
			return true, err
		}
		state.username = start.User
		if start.AuthenType != tacacsAuthenASCII {
			message := fmt.Sprintf("only ASCII login is supported (authen_type=0x%02x)", start.AuthenType)
			s.record(remote, start.User, "reject", message)
			return true, s.writeAuthen(conn, packet.Header, cfg, tacacsAuthenReply{Status: tacacsAuthenFail, ServerMsg: "only ASCII login is supported"})
		}
		if start.Action != tacacsAuthenLogin {
			message := "only ASCII LOGIN is supported"
			s.record(remote, start.User, "reject", message)
			return true, s.writeAuthen(conn, packet.Header, cfg, tacacsAuthenReply{Status: tacacsAuthenFail, ServerMsg: message})
		}
		if start.User == "" {
			state.stage = authenStageNeedUsername
			return false, s.writeAuthen(conn, packet.Header, cfg, tacacsAuthenReply{Status: tacacsAuthenGetUser, ServerMsg: "Username: "})
		}
		state.stage = authenStageNeedPassword
		return false, s.writeAuthen(conn, packet.Header, cfg, tacacsAuthenReply{Status: tacacsAuthenGetPass, Flags: tacacsAuthenNoEcho, ServerMsg: "Password: "})
	}

	continuePacket, err := decodeAuthenContinue(packet.Body)
	if err != nil {
		return true, err
	}
	if continuePacket.Flags&tacacsAcctAbort != 0 {
		s.record(remote, state.username, "reject", "AUTHEN aborted: "+continuePacket.Data)
		return true, nil
	}
	if state.stage == authenStageNeedUsername {
		state.username = continuePacket.UserMsg
		state.stage = authenStageNeedPassword
		return false, s.writeAuthen(conn, packet.Header, cfg, tacacsAuthenReply{Status: tacacsAuthenGetPass, Flags: tacacsAuthenNoEcho, ServerMsg: "Password: "})
	}

	password := continuePacket.UserMsg
	user := findUser(cfg.Users, state.username, password)
	if user == nil {
		message := "Authentication failed: invalid credentials"
		s.record(remote, state.username, "reject", message)
		return true, s.writeAuthen(conn, packet.Header, cfg, tacacsAuthenReply{Status: tacacsAuthenFail, ServerMsg: "Authentication failed"})
	}
	message := "Authentication successful"
	s.record(remote, state.username, "accept", message)
	return true, s.writeAuthen(conn, packet.Header, cfg, tacacsAuthenReply{Status: tacacsAuthenPass, ServerMsg: message})
}

func (s *TacacsServer) handleAuthor(conn net.Conn, packet tacacsPacket, cfg Config, state *tacacsSession, remote string) (bool, error) {
	request, err := decodeAuthorRequest(packet.Body)
	if err != nil {
		return true, err
	}
	state.username = request.User
	user := findUserByName(cfg.Users, request.User)
	if user == nil {
		message := "AUTHOR failed: unknown user"
		s.record(remote, request.User, "reject", message)
		return true, s.writeAuthor(conn, packet.Header, cfg, tacacsAuthorReply{Status: tacacsAuthorFail, ServerMsg: "Authorization failed"})
	}
	service, hasService := authorService(request.Args)
	if !hasService || !knownTacacsService(service) {
		// Unknown services are intentionally permissive in this lab collector:
		// accept the authorization request but add no attributes.
		message := "AUTHOR accepted with no attributes: unknown service"
		s.record(remote, request.User, "accept", message)
		return true, s.writeAuthor(conn, packet.Header, cfg, tacacsAuthorReply{Status: tacacsAuthorPassAdd})
	}
	if !strings.EqualFold(service, tacacsService(*user)) {
		message := "AUTHOR failed: service does not match the user's TACACS+ service"
		s.record(remote, request.User, "reject", message)
		return true, s.writeAuthor(conn, packet.Header, cfg, tacacsAuthorReply{Status: tacacsAuthorFail, ServerMsg: "Authorization failed"})
	}
	argument := fmt.Sprintf("priv-lvl=%d", user.PrivLvl)
	s.record(remote, request.User, "accept", "AUTHOR PASS_ADD: "+argument)
	return true, s.writeAuthor(conn, packet.Header, cfg, tacacsAuthorReply{Status: tacacsAuthorPassAdd, Args: []string{argument}})
}

func (s *TacacsServer) handleAcct(conn net.Conn, packet tacacsPacket, cfg Config, state *tacacsSession, remote string) (bool, error) {
	request, err := decodeAcctRequest(packet.Body)
	if err != nil {
		return true, err
	}
	state.username = request.User
	if !validAcctFlags(request.Flags) {
		message := fmt.Sprintf("ACCT rejected: invalid flags 0x%02x", request.Flags)
		s.record(remote, request.User, "reject", message)
		return true, s.writeAcct(conn, packet.Header, cfg, tacacsAcctReply{Status: tacacsAcctError})
	}
	parts := acctFlagNames(request.Flags)
	message := "ACCT " + strings.Join(parts, "+")
	if request.Flags != tacacsAcctWatchdog && len(request.Args) > 0 {
		message += ": " + strings.Join(request.Args, ", ")
	}
	s.record(remote, request.User, "accept", message)
	return true, s.writeAcct(conn, packet.Header, cfg, tacacsAcctReply{Status: tacacsAcctSuccess})
}

func (s *TacacsServer) writeAuthen(conn net.Conn, request tacacsHeader, cfg Config, reply tacacsAuthenReply) error {
	body, err := encodeAuthenReply(reply)
	if err != nil {
		return err
	}
	return s.writeReply(conn, request, cfg, body)
}

func (s *TacacsServer) writeAuthor(conn net.Conn, request tacacsHeader, cfg Config, reply tacacsAuthorReply) error {
	body, err := encodeAuthorReply(reply)
	if err != nil {
		return err
	}
	return s.writeReply(conn, request, cfg, body)
}

func (s *TacacsServer) writeAcct(conn net.Conn, request tacacsHeader, cfg Config, reply tacacsAcctReply) error {
	body, err := encodeAcctReply(reply)
	if err != nil {
		return err
	}
	return s.writeReply(conn, request, cfg, body)
}

func (s *TacacsServer) writeError(conn net.Conn, request tacacsHeader, key []byte, cause error) error {
	var body []byte
	var err error
	switch request.Type {
	case tacacsTypeAuthen:
		body, err = encodeAuthenReply(tacacsAuthenReply{Status: tacacsAuthenError, ServerMsg: "TACACS+ request rejected: " + cause.Error()})
	case tacacsTypeAuthor:
		body, err = encodeAuthorReply(tacacsAuthorReply{Status: tacacsAuthorError, ServerMsg: "TACACS+ request rejected"})
	case tacacsTypeAcct:
		body, err = encodeAcctReply(tacacsAcctReply{Status: tacacsAcctError})
	}
	if err != nil {
		return err
	}
	return s.writeReplyWithKey(conn, request, key, body)
}

func (s *TacacsServer) writeReply(conn net.Conn, request tacacsHeader, cfg Config, body []byte) error {
	key, ok := tacacsKey(cfg)
	if !ok {
		return errors.New("no TACACS+ key configured; refusing to send a reply")
	}
	return s.writeReplyWithKey(conn, request, key, body)
}

func (s *TacacsServer) writeReplyWithKey(conn net.Conn, request tacacsHeader, key, body []byte) error {
	if request.SeqNo >= 254 {
		return errors.New("TACACS+ sequence number would wrap")
	}
	replyHeader := request
	replyHeader.SeqNo++
	replyHeader.Flags = 0
	encoded, err := encodeTacacsPacket(replyHeader, body, key)
	if err != nil {
		return err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(tacacsReadTimeout))
	for len(encoded) > 0 {
		n, writeErr := conn.Write(encoded)
		if writeErr != nil {
			return writeErr
		}
		if n == 0 {
			return io.ErrUnexpectedEOF
		}
		encoded = encoded[n:]
	}
	return nil
}

func (s *TacacsServer) record(remote, user, result, message string) {
	s.ring.Add(AuthAttempt{At: time.Now(), Remote: remote, User: user, Result: result, Message: message})
}

func tacacsKey(cfg Config) ([]byte, bool) {
	key := cfg.TacacsKey
	if key == "" {
		key = cfg.SharedSecret
	}
	return []byte(key), key != ""
}

func tacacsKeyOrEmpty(cfg Config) []byte {
	key, _ := tacacsKey(cfg)
	return key
}

func findUser(users []User, username, password string) *User {
	for i := range users {
		if users[i].Username == username && users[i].Password == password {
			return &users[i]
		}
	}
	return nil
}

func findUserByName(users []User, username string) *User {
	for i := range users {
		if users[i].Username == username {
			return &users[i]
		}
	}
	return nil
}

func tacacsService(user User) string {
	if strings.TrimSpace(user.TacacsService) == "" {
		return "shell"
	}
	return strings.TrimSpace(user.TacacsService)
}

func authorService(args []string) (string, bool) {
	for _, arg := range args {
		if key, value, ok := strings.Cut(arg, "="); ok && strings.EqualFold(key, "service") {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}

func knownTacacsService(service string) bool {
	switch strings.ToLower(strings.TrimSpace(service)) {
	case "shell", "ppp", "arap", "slip", "rlogin", "tty", "connection", "none":
		return true
	default:
		return false
	}
}

func acctFlagNames(flags byte) []string {
	var names []string
	if flags&tacacsAcctStart != 0 {
		names = append(names, "start")
	}
	if flags&tacacsAcctStop != 0 {
		names = append(names, "stop")
	}
	if flags&tacacsAcctWatchdog != 0 {
		names = append(names, "watchdog")
	}
	return names
}
