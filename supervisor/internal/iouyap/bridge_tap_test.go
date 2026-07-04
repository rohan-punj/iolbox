package iouyap

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// These tests exercise the same netio<->tap pump transforms TapBridge wires
// up on linux (pumpNetioToTap/pumpTapToNetio), but drive them through
// pumpOne/runPump directly with in-memory fakes standing in for the tap fd,
// so they run on every OS without a real /dev/net/tun or any privilege. The
// transforms themselves (stripToUDP, Config.wrapToNetio) are exactly what
// TapBridge's linux pumps use.

// TestNetioToTapStripsHeaderForTap verifies the netio->tap direction: a
// datagram written on the netio side arrives on the tap side with the 8-byte
// netio header stripped, leaving only the raw ethernet frame (IFF_NO_PI: the
// tap carries no header of its own either).
func TestNetioToTapStripsHeaderForTap(t *testing.T) {
	netioSide := newFakeChan(1)
	tapSide := newFakeChan(1)
	frame := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0xAA, 0xBB}
	netioSide.send(WithPayload(Header{DstID: 501, SrcID: 1, MsgType: MsgTypeData}, frame))

	ctx := context.Background()
	if err := pumpOne(netioSide.readFunc(ctx), tapSide.writeFunc(), stripToUDP); err != nil {
		t.Fatalf("pumpOne (netio->tap) returned error: %v", err)
	}

	select {
	case got := <-tapSide.ch:
		if !bytes.Equal(got, frame) {
			t.Fatalf("frame delivered to tap = % x, want raw frame % x (header stripped)", got, frame)
		}
	default:
		t.Fatal("pumpOne (netio->tap) did not deliver a frame to the tap side")
	}
}

// TestTapToNetioWrapsHeaderCorrectly verifies the tap->netio direction: a raw
// ethernet frame read from the tap side is delivered to the netio side with a
// header addressed dst=LocalInstance/LocalAdapter/LocalPort,
// src=PseudoInstance/0/0 — the same addressing Bridge.pumpUDPToNetio uses,
// since IOL drops any datagram not naming one of its own interfaces as dst.
func TestTapToNetioWrapsHeaderCorrectly(t *testing.T) {
	tapSide := newFakeChan(1)
	netioSide := newFakeChan(1)
	cfg := Config{LocalInstance: 3, LocalAdapter: 2, LocalPort: 1, PseudoInstance: 503}
	frame := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	tapSide.send(frame)

	ctx := context.Background()
	if err := pumpOne(tapSide.readFunc(ctx), netioSide.writeFunc(), cfg.wrapToNetio); err != nil {
		t.Fatalf("pumpOne (tap->netio) returned error: %v", err)
	}

	select {
	case got := <-netioSide.ch:
		h, ok := ParseHeader(got)
		if !ok {
			t.Fatal("datagram delivered to netio side has no parseable header")
		}
		want := Header{
			DstID:   3,
			SrcID:   503,
			DstPort: EncodePortByte(2, 1),
			SrcPort: EncodePortByte(0, 0),
			MsgType: MsgTypeData,
		}
		if h != want {
			t.Fatalf("header delivered to netio side = %+v, want %+v", h, want)
		}
		if !bytes.Equal(Payload(got), frame) {
			t.Fatalf("payload delivered to netio side = % x, want % x", Payload(got), frame)
		}
	default:
		t.Fatal("pumpOne (tap->netio) did not deliver a datagram to the netio side")
	}
}

// TestTapPumpsRunConcurrentlyThenStopOnCancel drives both directions through
// runPump concurrently (mirroring TapBridge.Run's two goroutines) to confirm
// they forward independently and both stop cleanly on context cancellation.
func TestTapPumpsRunConcurrentlyThenStopOnCancel(t *testing.T) {
	netioIn := newFakeChan(2)
	tapOut := newFakeChan(2)
	tapIn := newFakeChan(2)
	netioOut := newFakeChan(2)

	cfg := Config{LocalInstance: 1, LocalAdapter: 0, LocalPort: 0, PseudoInstance: 501}

	netioFrame := []byte("from-netio")
	netioIn.send(WithPayload(Header{DstID: 501, SrcID: 1, MsgType: MsgTypeData}, netioFrame))
	tapFrame := []byte("from-tap")
	tapIn.send(tapFrame)

	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		go runPump(ctx, netioIn.readFunc(ctx), tapOut.writeFunc(), stripToUDP, errs)
		runPump(ctx, tapIn.readFunc(ctx), netioOut.writeFunc(), cfg.wrapToNetio, errs)
	}()

	select {
	case got := <-tapOut.ch:
		if !bytes.Equal(got, netioFrame) {
			t.Fatalf("netio->tap forwarded % x, want % x", got, netioFrame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for netio->tap forward")
	}

	select {
	case got := <-netioOut.ch:
		if _, ok := ParseHeader(got); !ok || !bytes.Equal(Payload(got), tapFrame) {
			t.Fatalf("tap->netio forwarded % x, header ok=%v, want payload % x", got, ok, tapFrame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for tap->netio forward")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pumps did not stop after context cancellation")
	}
}

func TestConfigValidateTap(t *testing.T) {
	base := Config{
		NetioPath:     "/tmp/link1.sock",
		LocalInstance: 1, LocalAdapter: 0, LocalPort: 0, PseudoInstance: 501,
		// UDPLocal/UDPRemote deliberately left zero: tap mode must not
		// require them.
	}
	if err := base.validateTap(); err != nil {
		t.Fatalf("validateTap() on a well-formed tap config returned %v", err)
	}

	cases := []struct {
		name string
		mut  func(c Config) Config
	}{
		{"empty path", func(c Config) Config { c.NetioPath = ""; return c }},
		{"zero local instance", func(c Config) Config { c.LocalInstance = 0; return c }},
		{"local instance too large", func(c Config) Config { c.LocalInstance = 1025; return c }},
		{"zero pseudo instance", func(c Config) Config { c.PseudoInstance = 0; return c }},
		{"pseudo instance too large", func(c Config) Config { c.PseudoInstance = 1025; return c }},
		{"adapter beyond nibble", func(c Config) Config { c.LocalAdapter = 16; return c }},
		{"port beyond nibble", func(c Config) Config { c.LocalPort = 16; return c }},
		{"negative adapter", func(c Config) Config { c.LocalAdapter = -1; return c }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.mut(base).validateTap(); err == nil {
				t.Fatalf("validateTap() accepted an invalid config (%s)", tc.name)
			}
		})
	}
}

func TestNewTapRejectsInvalidConfigOnAnyPlatform(t *testing.T) {
	// NewTap must validate before touching any platform-specific socket/tap
	// code, so this must fail identically on linux and non-linux.
	_, err := NewTap(Config{}, "tap0")
	if err == nil {
		t.Fatal("NewTap(Config{}, ...) succeeded, want a validation error")
	}
}
