package main

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"testing"
)

func TestTacacsPadVectorIndependentThreeBlockChain(t *testing.T) {
	sessionID := uint32(0x01020304)
	key := []byte("vector-key")
	version := byte(0xc0)
	seqNo := byte(3)
	length := 48

	// This is intentionally written independently of tacacsPad. The three
	// explicit digests catch a wrong session-ID byte order, omitted version or
	// sequence fields, and a truncated previous digest.
	seed := append([]byte{0x01, 0x02, 0x03, 0x04}, key...)
	seed = append(seed, version, seqNo)
	digest1 := md5.Sum(seed)
	secondInput := append(append([]byte(nil), seed...), digest1[:]...)
	digest2 := md5.Sum(secondInput)
	thirdInput := append(append([]byte(nil), seed...), digest2[:]...)
	digest3 := md5.Sum(thirdInput)
	want := append(append(append([]byte(nil), digest1[:]...), digest2[:]...), digest3[:]...)

	got := tacacsPad(sessionID, key, version, seqNo, length)
	if !bytes.Equal(got, want) {
		t.Fatalf("TACACS+ pad = %x, want %x", got, want)
	}
}

func TestTacacsHeaderLayoutLiteral(t *testing.T) {
	h := tacacsHeader{Version: 0xc0, Type: tacacsTypeAuthen, SeqNo: 1, Flags: 0, SessionID: 0x01020304, Length: 0x20}
	want := []byte{0xc0, 0x01, 0x01, 0x00, 0x01, 0x02, 0x03, 0x04, 0x00, 0x00, 0x00, 0x20}
	got, err := encodeTacacsHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("TACACS+ header = %x, want %x", got, want)
	}
	decoded, err := decodeTacacsHeader(want)
	if err != nil || decoded != h {
		t.Fatalf("decoded header = %+v, err %v, want %+v", decoded, err, h)
	}
}

func TestTacacsBodyLayoutLiterals(t *testing.T) {
	tests := []struct {
		name string
		got  func() ([]byte, error)
		want []byte
	}{
		{
			name: "AUTHEN START uses one-byte lengths",
			got: func() ([]byte, error) {
				return encodeAuthenStart(tacacsAuthenStart{Action: tacacsAuthenLogin, PrivLvl: 15, AuthenType: tacacsAuthenASCII, AuthenService: 1, User: "bob", Port: "tty", RemoteAddr: "1.2.3.4", Data: "x"})
			},
			want: []byte{0x01, 0x0f, 0x01, 0x01, 0x03, 0x03, 0x07, 0x01, 'b', 'o', 'b', 't', 't', 'y', '1', '.', '2', '.', '3', '.', '4', 'x'},
		},
		{
			name: "AUTHEN REPLY uses two-byte lengths",
			got: func() ([]byte, error) {
				return encodeAuthenReply(tacacsAuthenReply{Status: tacacsAuthenPass, ServerMsg: "OK", Data: "D"})
			},
			want: []byte{0x01, 0x00, 0x00, 0x02, 0x00, 0x01, 'O', 'K', 'D'},
		},
		{
			name: "AUTHEN CONTINUE puts flags after both lengths",
			got: func() ([]byte, error) {
				return encodeAuthenContinue(tacacsAuthenContinue{UserMsg: "pw", Data: "x"})
			},
			want: []byte{0x00, 0x02, 0x00, 0x01, 0x00, 'p', 'w', 'x'},
		},
		{
			name: "AUTHOR REQUEST",
			got: func() ([]byte, error) {
				return encodeAuthorRequest(tacacsAuthorRequest{AuthenMethod: tacacsAuthenMethodTACACSPlus, PrivLvl: 15, AuthenType: tacacsAuthenASCII, AuthenService: 1, User: "u", Port: "p", Args: []string{"service=shell", "cmd*show"}})
			},
			want: []byte{0x06, 0x0f, 0x01, 0x01, 0x01, 0x01, 0x00, 0x02, 0x0d, 0x08, 'u', 'p', 's', 'e', 'r', 'v', 'i', 'c', 'e', '=', 's', 'h', 'e', 'l', 'l', 'c', 'm', 'd', '*', 's', 'h', 'o', 'w'},
		},
		{
			name: "AUTHOR REPLY",
			got: func() ([]byte, error) {
				return encodeAuthorReply(tacacsAuthorReply{Status: tacacsAuthorPassAdd, Args: []string{"priv-lvl=15"}})
			},
			want: []byte{0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x0b, 'p', 'r', 'i', 'v', '-', 'l', 'v', 'l', '=', '1', '5'},
		},
		{
			name: "ACCT REQUEST",
			got: func() ([]byte, error) {
				return encodeAcctRequest(tacacsAcctRequest{Flags: tacacsAcctStart, AuthenMethod: tacacsAuthenMethodTACACSPlus, PrivLvl: 15, AuthenType: tacacsAuthenASCII, AuthenService: 1, User: "u", Port: "p", Args: []string{"x"}})
			},
			want: []byte{0x02, 0x06, 0x0f, 0x01, 0x01, 0x01, 0x01, 0x00, 0x01, 0x01, 'u', 'p', 'x'},
		},
		{
			name: "ACCT REPLY puts status last",
			got: func() ([]byte, error) {
				return encodeAcctReply(tacacsAcctReply{Status: tacacsAcctSuccess, ServerMsg: "ok", Data: "d"})
			},
			want: []byte{0x00, 0x02, 0x00, 0x01, 0x01, 'o', 'k', 'd'},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.got()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("body = %x, want %x", got, tc.want)
			}
		})
	}
}

func TestTacacsWrongKeyRejectsLengthFields(t *testing.T) {
	body, err := encodeAuthenStart(tacacsAuthenStart{Action: tacacsAuthenLogin, PrivLvl: 1, AuthenType: tacacsAuthenASCII, AuthenService: 1, User: "alice", Port: "tty", RemoteAddr: "192.0.2.1", Data: ""})
	if err != nil {
		t.Fatal(err)
	}
	packetBytes, err := encodeTacacsPacket(tacacsHeader{Version: tacacsVersion, Type: tacacsTypeAuthen, SeqNo: 1, SessionID: 0x10203040}, body, []byte("key-a"))
	if err != nil {
		t.Fatal(err)
	}
	packet, err := decodeTacacsPacket(packetBytes, []byte("key-b"))
	if err != nil {
		t.Fatal("wrong-key decryption itself should produce bytes before structural validation:", err)
	}
	if _, err := decodeAuthenStart(packet.Body); err == nil {
		t.Fatal("wrong-key body unexpectedly passed AUTHEN START length validation")
	}
}

func TestTacacsUnencryptedBodyAlwaysRejected(t *testing.T) {
	body := []byte{0x01, 0x01, 0x01, 0x01, 0, 0, 0, 0}
	packet, err := encodeTacacsPacket(tacacsHeader{Version: tacacsVersion, Type: tacacsTypeAuthen, SeqNo: 1, Flags: tacacsFlagUnencrypted, SessionID: 7}, body, []byte("configured-key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeTacacsPacket(packet, []byte("configured-key")); err == nil || !bytes.Contains([]byte(err.Error()), []byte("unencrypted")) {
		t.Fatalf("unencrypted packet error = %v", err)
	}
}

func TestTacacsCodecLengthUsesBigEndian(t *testing.T) {
	body := []byte("body")
	packet, err := encodeTacacsPacket(tacacsHeader{Version: tacacsVersion, Type: tacacsTypeAuthen, SeqNo: 1, SessionID: 1}, body, []byte("k"))
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.BigEndian.Uint32(packet[8:12]); got != uint32(len(body)) {
		t.Fatalf("header body length = %d, want %d", got, len(body))
	}
}
