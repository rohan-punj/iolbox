//go:build linux

package iouyap

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTapBridgeRunStopsBothPumpsAfterTapWriteError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "netio.sock")
	addr, err := net.ResolveUnixAddr("unixgram", path)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		t.Fatal(err)
	}
	tapRead, tapWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer tapWrite.Close()
	wantErr := errors.New("injected tap write failure")
	b := &TapBridge{
		cfg:      Config{NetioPath: path},
		unixConn: listener,
		tap:      tapRead,
		closed:   make(chan struct{}),
		tapWrite: func([]byte) (int, error) { return 0, wantErr },
	}

	done := make(chan error, 1)
	go func() { done <- b.Run(context.Background()) }()
	peer, err := net.DialUnix("unixgram", nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if _, err := peer.Write(WithPayload(Header{DstID: 501, SrcID: 1, MsgType: MsgTypeData}, []byte("frame"))); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-done:
		if !errors.Is(got, wantErr) {
			t.Fatalf("Run error = %v, want injected write error", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after one pump died")
	}
	select {
	case <-b.closed:
	default:
		t.Fatal("Run did not close the bridge after pump failure")
	}
}
