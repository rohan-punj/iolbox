package main

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testTacacsKey = "tacacs-secret"

func testTacacsStore(users ...User) *Store {
	store := NewStore("")
	store.Set(Config{SharedSecret: "radius-secret", TacacsKey: testTacacsKey, Protocol: "both", Users: users})
	return store
}

func mustTacacsBody(t *testing.T, body []byte, err error) []byte {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func mustTacacsPacket(t *testing.T, header tacacsHeader, body []byte, key string) []byte {
	t.Helper()
	packet, err := encodeTacacsPacket(header, body, []byte(key))
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func readTacacsPacket(t *testing.T, conn net.Conn, key string) tacacsPacket {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	headerBytes := make([]byte, 12)
	if _, err := io.ReadFull(conn, headerBytes); err != nil {
		t.Fatal(err)
	}
	header, err := decodeTacacsHeader(headerBytes)
	if err != nil {
		t.Fatal(err)
	}
	body := make([]byte, int(header.Length))
	if _, err := io.ReadFull(conn, body); err != nil {
		t.Fatal(err)
	}
	packet, err := decodeTacacsPacket(append(headerBytes, body...), []byte(key))
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func runTacacsPackets(t *testing.T, server *TacacsServer, packets [][]byte, key string) []tacacsPacket {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	go server.handleConn(serverConn)
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})
	responses := make([]tacacsPacket, 0, len(packets))
	for _, packet := range packets {
		if _, err := clientConn.Write(packet); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, readTacacsPacket(t, clientConn, key))
	}
	return responses
}

func authenStartPacket(t *testing.T, sessionID uint32, seq, version, authenType byte, username, key string) []byte {
	bodyBytes, bodyErr := encodeAuthenStart(tacacsAuthenStart{Action: tacacsAuthenLogin, PrivLvl: 1, AuthenType: authenType, AuthenService: 1, User: username, Port: "tty", RemoteAddr: "192.0.2.10"})
	body := mustTacacsBody(t, bodyBytes, bodyErr)
	return mustTacacsPacket(t, tacacsHeader{Version: version, Type: tacacsTypeAuthen, SeqNo: seq, SessionID: sessionID}, body, key)
}

func authenContinuePacket(t *testing.T, sessionID uint32, seq byte, message, key string) []byte {
	bodyBytes, bodyErr := encodeAuthenContinue(tacacsAuthenContinue{UserMsg: message})
	body := mustTacacsBody(t, bodyBytes, bodyErr)
	return mustTacacsPacket(t, tacacsHeader{Version: tacacsVersion, Type: tacacsTypeAuthen, SeqNo: seq, SessionID: sessionID}, body, key)
}

func TestTacacsSessionTable(t *testing.T) {
	cases := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "valid user PASS",
			check: func(t *testing.T) {
				server := NewTacacsServer(testTacacsStore(User{Username: "alice", Password: "correct", Service: "login", TacacsService: "shell", PrivLvl: 15}))
				responses := runTacacsPackets(t, server, [][]byte{
					authenStartPacket(t, 1, 1, tacacsVersion, tacacsAuthenASCII, "alice", testTacacsKey),
					authenContinuePacket(t, 1, 3, "correct", testTacacsKey),
				}, testTacacsKey)
				first, err := decodeAuthenReply(responses[0].Body)
				if err != nil || first.Status != tacacsAuthenGetPass || first.Flags != tacacsAuthenNoEcho {
					t.Fatalf("first reply = %+v, err %v", first, err)
				}
				last, err := decodeAuthenReply(responses[1].Body)
				if err != nil || last.Status != tacacsAuthenPass {
					t.Fatalf("last reply = %+v, err %v", last, err)
				}
				if responses[0].Header.SeqNo != 2 || responses[1].Header.SeqNo != 4 {
					t.Fatalf("reply sequence numbers = %d, %d; want 2, 4", responses[0].Header.SeqNo, responses[1].Header.SeqNo)
				}
			},
		},
		{
			name: "bad password FAIL",
			check: func(t *testing.T) {
				server := NewTacacsServer(testTacacsStore(User{Username: "alice", Password: "correct", TacacsService: "shell"}))
				responses := runTacacsPackets(t, server, [][]byte{authenStartPacket(t, 2, 1, tacacsVersion, tacacsAuthenASCII, "alice", testTacacsKey), authenContinuePacket(t, 2, 3, "wrong", testTacacsKey)}, testTacacsKey)
				reply, err := decodeAuthenReply(responses[1].Body)
				if err != nil || reply.Status != tacacsAuthenFail {
					t.Fatalf("reply = %+v, err %v", reply, err)
				}
			},
		},
		{
			name: "unknown user FAIL",
			check: func(t *testing.T) {
				server := NewTacacsServer(testTacacsStore(User{Username: "alice", Password: "correct", TacacsService: "shell"}))
				responses := runTacacsPackets(t, server, [][]byte{authenStartPacket(t, 3, 1, tacacsVersion, tacacsAuthenASCII, "nobody", testTacacsKey), authenContinuePacket(t, 3, 3, "correct", testTacacsKey)}, testTacacsKey)
				reply, err := decodeAuthenReply(responses[1].Body)
				if err != nil || reply.Status != tacacsAuthenFail {
					t.Fatalf("reply = %+v, err %v", reply, err)
				}
			},
		},
		{
			name: "wrong key is caught by structural validation",
			check: func(t *testing.T) {
				server := NewTacacsServer(testTacacsStore(User{Username: "alice", Password: "correct", TacacsService: "shell"}))
				responses := runTacacsPackets(t, server, [][]byte{authenStartPacket(t, 4, 1, tacacsVersion, tacacsAuthenASCII, "alice", "different-key")}, testTacacsKey)
				reply, err := decodeAuthenReply(responses[0].Body)
				if err != nil || reply.Status != tacacsAuthenError {
					t.Fatalf("wrong-key reply = %+v, err %v", reply, err)
				}
			},
		},
		{
			name: "PAP is a clean FAIL",
			check: func(t *testing.T) {
				server := NewTacacsServer(testTacacsStore(User{Username: "alice", Password: "correct", TacacsService: "shell"}))
				responses := runTacacsPackets(t, server, [][]byte{authenStartPacket(t, 5, 1, tacacsVersion, tacacsAuthenPAP, "alice", testTacacsKey)}, testTacacsKey)
				reply, err := decodeAuthenReply(responses[0].Body)
				if err != nil || reply.Status != tacacsAuthenFail || !strings.Contains(reply.ServerMsg, "ASCII") {
					t.Fatalf("PAP reply = %+v, err %v", reply, err)
				}
			},
		},
		{
			name: "AUTHOR matches TacacsService, not RADIUS Service",
			check: func(t *testing.T) {
				server := NewTacacsServer(testTacacsStore(User{Username: "alice", Password: "correct", Service: "shell", TacacsService: "ppp", PrivLvl: 15}))
				bodyBytes, bodyErr := encodeAuthorRequest(tacacsAuthorRequest{AuthenMethod: tacacsAuthenMethodTACACSPlus, PrivLvl: 1, AuthenType: tacacsAuthenASCII, AuthenService: 1, User: "alice", Args: []string{"service=ppp"}})
				body := mustTacacsBody(t, bodyBytes, bodyErr)
				decodedRequest, decodeErr := decodeAuthorRequest(body)
				if decodeErr != nil {
					t.Fatalf("AUTHOR request decode = %+v, err %v, len %d, body %x", decodedRequest, decodeErr, len(body), body)
				}
				responses := runTacacsPackets(t, server, [][]byte{mustTacacsPacket(t, tacacsHeader{Version: tacacsVersion, Type: tacacsTypeAuthor, SeqNo: 1, SessionID: 6}, body, testTacacsKey)}, testTacacsKey)
				reply, err := decodeAuthorReply(responses[0].Body)
				if err != nil || reply.Status != tacacsAuthorPassAdd || len(reply.Args) != 1 || reply.Args[0] != "priv-lvl=15" {
					t.Fatalf("AUTHOR reply = %+v, err %v", reply, err)
				}
			},
		},
		{
			name: "ACCT START succeeds and logs one row",
			check: func(t *testing.T) {
				server := NewTacacsServer(testTacacsStore())
				bodyBytes, bodyErr := encodeAcctRequest(tacacsAcctRequest{Flags: tacacsAcctStart, AuthenMethod: tacacsAuthenMethodTACACSPlus, PrivLvl: 15, AuthenType: tacacsAuthenASCII, AuthenService: 1, User: "alice", Args: []string{"task_id=7"}})
				body := mustTacacsBody(t, bodyBytes, bodyErr)
				decodedRequest, decodeErr := decodeAcctRequest(body)
				if decodeErr != nil {
					t.Fatalf("ACCT request decode = %+v, err %v, body %x", decodedRequest, decodeErr, body)
				}
				responses := runTacacsPackets(t, server, [][]byte{mustTacacsPacket(t, tacacsHeader{Version: tacacsVersion, Type: tacacsTypeAcct, SeqNo: 1, SessionID: 7}, body, testTacacsKey)}, testTacacsKey)
				reply, err := decodeAcctReply(responses[0].Body)
				if err != nil || reply.Status != tacacsAcctSuccess {
					t.Fatalf("ACCT reply = %+v, err %v", reply, err)
				}
				entries := server.Attempts()
				if len(entries) != 1 || entries[0].Result != "accept" || !strings.Contains(entries[0].Message, "start") {
					t.Fatalf("ACCT entries = %+v", entries)
				}
			},
		},
		{
			name: "ACCT invalid flag combination is ERROR",
			check: func(t *testing.T) {
				server := NewTacacsServer(testTacacsStore())
				bodyBytes, bodyErr := encodeAcctRequest(tacacsAcctRequest{Flags: tacacsAcctStart | tacacsAcctStop, AuthenMethod: tacacsAuthenMethodTACACSPlus, AuthenType: tacacsAuthenASCII, User: "alice"})
				body := mustTacacsBody(t, bodyBytes, bodyErr)
				responses := runTacacsPackets(t, server, [][]byte{mustTacacsPacket(t, tacacsHeader{Version: tacacsVersion, Type: tacacsTypeAcct, SeqNo: 1, SessionID: 8}, body, testTacacsKey)}, testTacacsKey)
				reply, err := decodeAcctReply(responses[0].Body)
				if err != nil || reply.Status != tacacsAcctError {
					t.Fatalf("invalid ACCT reply = %+v, err %v", reply, err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.check)
	}
}

func TestTacacsMinorVersionAndUnencryptedErrors(t *testing.T) {
	server := NewTacacsServer(testTacacsStore())
	authorBytes, authorErr := encodeAuthorRequest(tacacsAuthorRequest{AuthenMethod: tacacsAuthenMethodTACACSPlus, AuthenType: tacacsAuthenASCII, User: "alice", Args: []string{"service=shell"}})
	authorBody := mustTacacsBody(t, authorBytes, authorErr)
	acctBytes, acctErr := encodeAcctRequest(tacacsAcctRequest{Flags: tacacsAcctStart, AuthenMethod: tacacsAuthenMethodTACACSPlus, AuthenType: tacacsAuthenASCII, User: "alice"})
	acctBody := mustTacacsBody(t, acctBytes, acctErr)
	for _, tc := range []struct {
		name    string
		typ     byte
		session uint32
		body    []byte
	}{
		{"AUTHOR minor version 1", tacacsTypeAuthor, 9, authorBody},
		{"ACCT minor version 1", tacacsTypeAcct, 10, acctBody},
	} {
		t.Run(tc.name, func(t *testing.T) {
			packet := mustTacacsPacket(t, tacacsHeader{Version: 0xc1, Type: tc.typ, SeqNo: 1, SessionID: tc.session}, tc.body, testTacacsKey)
			responses := runTacacsPackets(t, server, [][]byte{packet}, testTacacsKey)
			if tc.typ == tacacsTypeAuthor {
				reply, err := decodeAuthorReply(responses[0].Body)
				if err != nil || reply.Status != tacacsAuthorError {
					t.Fatalf("minor-version AUTHOR reply = %+v, err %v", reply, err)
				}
			} else {
				reply, err := decodeAcctReply(responses[0].Body)
				if err != nil || reply.Status != tacacsAcctError {
					t.Fatalf("minor-version ACCT reply = %+v, err %v", reply, err)
				}
			}
		})
	}

	unencryptedBytes, unencryptedErr := encodeAuthenStart(tacacsAuthenStart{Action: tacacsAuthenLogin, AuthenType: tacacsAuthenASCII, User: "alice"})
	unencryptedBody := mustTacacsBody(t, unencryptedBytes, unencryptedErr)
	unencrypted := mustTacacsPacket(t, tacacsHeader{Version: tacacsVersion, Type: tacacsTypeAuthen, SeqNo: 1, Flags: tacacsFlagUnencrypted, SessionID: 11}, unencryptedBody, testTacacsKey)
	unencrypted[3] = tacacsFlagUnencrypted
	responses := runTacacsPackets(t, server, [][]byte{unencrypted}, testTacacsKey)
	reply, err := decodeAuthenReply(responses[0].Body)
	if err != nil || reply.Status != tacacsAuthenError {
		t.Fatalf("unencrypted reply = %+v, err %v", reply, err)
	}
}

func TestTacacsEmptyKeyRefusesToServe(t *testing.T) {
	store := NewStore("")
	store.Set(Config{Protocol: "both"})
	server := NewTacacsServer(store)
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve("127.0.0.1:0") }()
	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "no TACACS+ key configured") {
			t.Fatalf("Serve error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("empty-key Serve did not refuse promptly")
	}

	app := NewApp(store)
	go app.serveTacacs("127.0.0.1:0")
	waitForTacacsError(t, app)
	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(recorder.Body.String(), "TACACS") || !strings.Contains(recorder.Body.String(), "no TACACS") || !strings.Contains(recorder.Body.String(), "key configured") {
		t.Fatalf("empty-key dashboard = %q", recorder.Body.String())
	}
}

func TestTacacsBindFailureVisibleAndHealthzStillOK(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	app := NewApp(testTacacsStore())
	go app.serveTacacs(occupied.Addr().String())
	waitForTacacsError(t, app)
	app.errMu.RLock()
	storedError := app.tacacsErr
	app.errMu.RUnlock()
	if storedError == "" {
		t.Fatal("listener goroutine did not set tacacsErr")
	}

	dashboard := httptest.NewRecorder()
	app.routes().ServeHTTP(dashboard, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(dashboard.Body.String(), "TACACS+ could not bind TCP 49") || !strings.Contains(dashboard.Body.String(), storedError) {
		t.Fatalf("dashboard omitted TACACS+ bind error: %q", dashboard.Body.String())
	}
	health := httptest.NewRecorder()
	app.routes().ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK || !strings.Contains(health.Body.String(), "ok") {
		t.Fatalf("healthz = %d %q", health.Code, health.Body.String())
	}
}

func waitForTacacsError(t *testing.T, app *App) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if app.listenerError("tacacs") != "" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("TACACS+ listener error was not recorded")
}

func TestTacacsTCPFramingSplitAndCoalesced(t *testing.T) {
	server := NewTacacsServer(testTacacsStore(User{Username: "alice", Password: "correct", TacacsService: "shell"}))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			accepted <- acceptErr
			return
		}
		server.handleConn(conn)
		accepted <- nil
	}()
	client, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	start := authenStartPacket(t, 12, 1, tacacsVersion, tacacsAuthenASCII, "", testTacacsKey)
	if _, err := client.Write(start[:15]); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write(start[15:]); err != nil {
		t.Fatal(err)
	}
	first := readTacacsPacket(t, client, testTacacsKey)
	firstReply, err := decodeAuthenReply(first.Body)
	if err != nil || firstReply.Status != tacacsAuthenGetUser {
		t.Fatalf("split START reply = %+v, err %v", firstReply, err)
	}

	username := authenContinuePacket(t, 12, 3, "alice", testTacacsKey)
	password := authenContinuePacket(t, 12, 5, "correct", testTacacsKey)
	coalesced := append(append([]byte(nil), username...), password...)
	if _, err := client.Write(coalesced); err != nil {
		t.Fatal(err)
	}
	second := readTacacsPacket(t, client, testTacacsKey)
	third := readTacacsPacket(t, client, testTacacsKey)
	secondReply, err := decodeAuthenReply(second.Body)
	if err != nil || secondReply.Status != tacacsAuthenGetPass || second.Header.SeqNo != 4 {
		t.Fatalf("coalesced username reply = %+v header %+v err %v", secondReply, second.Header, err)
	}
	thirdReply, err := decodeAuthenReply(third.Body)
	if err != nil || thirdReply.Status != tacacsAuthenPass || third.Header.SeqNo != 6 {
		t.Fatalf("coalesced password reply = %+v header %+v err %v", thirdReply, third.Header, err)
	}
	if acceptErr := <-accepted; acceptErr != nil {
		t.Fatal(acceptErr)
	}
}

func TestTacacsSequenceRejectsOutOfOrderAndWrap(t *testing.T) {
	state := tacacsSession{initialized: true, sessionID: 1, version: tacacsVersion, lastSeq: 1}
	if err := state.acceptHeader(tacacsHeader{SessionID: 1, Version: tacacsVersion, SeqNo: 3}); err != nil {
		t.Fatal(err)
	}
	if err := state.acceptHeader(tacacsHeader{SessionID: 1, Version: tacacsVersion, SeqNo: 7}); err == nil {
		t.Fatal("out-of-order sequence was accepted")
	}
	if err := state.acceptHeader(tacacsHeader{SessionID: 1, Version: tacacsVersion, SeqNo: 255}); err == nil {
		t.Fatal("wrapped sequence was accepted")
	}
	if _, err := decodeTacacsHeader([]byte{tacacsVersion, tacacsTypeAuthen, 0xff, 0, 0, 0, 0, 1, 0, 0, 0, 0}); err == nil {
		t.Fatal("header decoder accepted seq_no 255")
	}
}

func TestTacacsAcctFlagsTable(t *testing.T) {
	for _, tc := range []struct {
		flags byte
		want  bool
	}{
		{tacacsAcctStart, true},
		{tacacsAcctStop, true},
		{tacacsAcctWatchdog, true},
		{tacacsAcctStart | tacacsAcctWatchdog, true},
		{0, false},
		{tacacsAcctStart | tacacsAcctStop, false},
		{tacacsAcctStop | tacacsAcctWatchdog, false},
		{0x80 | tacacsAcctStart, false},
	} {
		if got := validAcctFlags(tc.flags); got != tc.want {
			t.Errorf("validAcctFlags(0x%02x) = %v, want %v", tc.flags, got, tc.want)
		}
	}
}

func TestTacacsPacketDecodeRejectsTrailingBytes(t *testing.T) {
	packet := mustTacacsPacket(t, tacacsHeader{Version: tacacsVersion, Type: tacacsTypeAuthen, SeqNo: 1, SessionID: 13}, []byte("x"), testTacacsKey)
	packet = append(packet, 0)
	if _, err := decodeTacacsPacket(packet, []byte(testTacacsKey)); err == nil {
		t.Fatal("packet decoder accepted trailing bytes")
	}
}

func TestTacacsKeyFallback(t *testing.T) {
	key, ok := tacacsKey(Config{SharedSecret: "shared"})
	if !ok || !bytes.Equal(key, []byte("shared")) {
		t.Fatalf("fallback key = %q, configured = %v", key, ok)
	}
	if _, ok := tacacsKey(Config{}); ok {
		t.Fatal("empty TACACS+ key was accepted")
	}
}
