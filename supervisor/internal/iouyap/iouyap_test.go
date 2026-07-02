package iouyap

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

// fakeChan is a tiny in-memory datagram queue used to test pumpOne/runPump
// without any real sockets, so this logic is exercised on every OS.
type fakeChan struct {
	ch chan []byte
}

func newFakeChan(capacity int) *fakeChan {
	return &fakeChan{ch: make(chan []byte, capacity)}
}

func (f *fakeChan) send(datagram []byte) {
	f.ch <- datagram
}

// readFunc returns a read closure compatible with pumpOne/runPump: it blocks
// until a datagram is queued or ctx is done.
func (f *fakeChan) readFunc(ctx context.Context) func() ([]byte, error) {
	return func() ([]byte, error) {
		select {
		case d := <-f.ch:
			return d, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (f *fakeChan) writeFunc() func([]byte) error {
	return func(datagram []byte) error {
		f.ch <- datagram
		return nil
	}
}

func TestStripToUDPStripsHeader(t *testing.T) {
	frame := []byte("hello")
	datagram := WithPayload(Header{DstID: 501, SrcID: 1, MsgType: MsgTypeData}, frame)
	out, ok := stripToUDP(datagram)
	if !ok {
		t.Fatal("stripToUDP reported not-ok for a valid framed datagram")
	}
	if !bytes.Equal(out, frame) {
		t.Fatalf("stripToUDP = % x, want the raw frame % x", out, frame)
	}
}

func TestStripToUDPDropsShortDatagram(t *testing.T) {
	for n := 0; n < HeaderSize; n++ {
		if _, ok := stripToUDP(make([]byte, n)); ok {
			t.Fatalf("stripToUDP accepted a %d-byte datagram, want drop", n)
		}
	}
}

func TestWrapToNetioAddressesLocalInstance(t *testing.T) {
	// A bridge serving instance 2's Ethernet1/0, bound as pseudo 502: every
	// delivered frame must be addressed dst=(2, 1/0) src=(502, 0/0), the
	// exact header real IOL accepts (docs/p0-spike.md "netio header layout").
	cfg := Config{LocalInstance: 2, LocalAdapter: 1, LocalPort: 0, PseudoInstance: 502}
	frame := []byte{0xAA, 0xBB, 0xCC, 0xDD}
	out, ok := cfg.wrapToNetio(frame)
	if !ok {
		t.Fatal("wrapToNetio reported not-ok for a valid frame")
	}
	h, hok := ParseHeader(out)
	if !hok {
		t.Fatal("wrapToNetio output has no parseable header")
	}
	want := Header{
		DstID:   2,
		SrcID:   502,
		DstPort: EncodePortByte(1, 0),
		SrcPort: EncodePortByte(0, 0),
		MsgType: MsgTypeData,
	}
	if h != want {
		t.Fatalf("wrapToNetio header = %+v, want %+v", h, want)
	}
	if !bytes.Equal(Payload(out), frame) {
		t.Fatalf("wrapToNetio payload = % x, want % x", Payload(out), frame)
	}
}

func TestWrapToNetioDropsEmptyFrame(t *testing.T) {
	cfg := Config{LocalInstance: 1, PseudoInstance: 501}
	for _, in := range [][]byte{nil, {}} {
		if _, ok := cfg.wrapToNetio(in); ok {
			t.Fatalf("wrapToNetio accepted an empty frame %v, want drop", in)
		}
	}
}

func TestPumpOneForwardsOneDatagram(t *testing.T) {
	in := newFakeChan(1)
	out := newFakeChan(1)
	frame := []byte("payload")
	in.send(WithPayload(Header{DstID: 5, SrcID: 6}, frame))

	ctx := context.Background()
	if err := pumpOne(in.readFunc(ctx), out.writeFunc(), stripToUDP); err != nil {
		t.Fatalf("pumpOne returned error: %v", err)
	}

	select {
	case got := <-out.ch:
		if !bytes.Equal(got, frame) {
			t.Fatalf("forwarded datagram = % x, want the stripped frame % x", got, frame)
		}
	default:
		t.Fatal("pumpOne did not forward the datagram")
	}
}

func TestPumpOneDropsShortDatagramWithoutError(t *testing.T) {
	in := newFakeChan(1)
	out := newFakeChan(1)
	in.send([]byte{0x01, 0x02}) // shorter than HeaderSize

	ctx := context.Background()
	if err := pumpOne(in.readFunc(ctx), out.writeFunc(), stripToUDP); err != nil {
		t.Fatalf("pumpOne returned error for a short (dropped) datagram: %v", err)
	}
	select {
	case got := <-out.ch:
		t.Fatalf("pumpOne forwarded a short datagram it should have dropped: % x", got)
	default:
		// expected: nothing forwarded
	}
}

func TestPumpOnePropagatesReadError(t *testing.T) {
	wantErr := errors.New("boom")
	read := func() ([]byte, error) { return nil, wantErr }
	write := func([]byte) error {
		t.Fatal("write should not be called when read fails")
		return nil
	}
	if err := pumpOne(read, write, stripToUDP); !errors.Is(err, wantErr) {
		t.Fatalf("pumpOne error = %v, want %v", err, wantErr)
	}
}

func TestPumpOnePropagatesWriteError(t *testing.T) {
	wantErr := errors.New("write boom")
	datagram := WithPayload(Header{}, []byte("x"))
	read := func() ([]byte, error) { return datagram, nil }
	write := func([]byte) error { return wantErr }
	if err := pumpOne(read, write, stripToUDP); !errors.Is(err, wantErr) {
		t.Fatalf("pumpOne error = %v, want %v", err, wantErr)
	}
}

func TestRunPumpForwardsMultipleDatagramsThenStopsOnCancel(t *testing.T) {
	in := newFakeChan(4)
	out := newFakeChan(4)
	frames := [][]byte{[]byte("a"), []byte("bb"), []byte("ccc")}
	for i, f := range frames {
		in.send(WithPayload(Header{SrcID: uint16(i + 1)}, f))
	}

	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		runPump(ctx, in.readFunc(ctx), out.writeFunc(), stripToUDP, errs)
		close(done)
	}()

	for i, w := range frames {
		select {
		case got := <-out.ch:
			if !bytes.Equal(got, w) {
				t.Fatalf("datagram %d = % x, want % x", i, got, w)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for forwarded datagram %d", i)
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runPump did not stop after context cancellation")
	}
	select {
	case err := <-errs:
		t.Fatalf("runPump reported an error after clean cancellation: %v", err)
	default:
	}
}

func TestRunPumpReportsNonCancellationError(t *testing.T) {
	wantErr := errors.New("socket died")
	read := func() ([]byte, error) { return nil, wantErr }
	write := func([]byte) error { return nil }

	ctx := context.Background()
	errs := make(chan error, 1)
	runPump(ctx, read, write, stripToUDP, errs)

	select {
	case err := <-errs:
		if !errors.Is(err, wantErr) {
			t.Fatalf("runPump reported %v, want %v", err, wantErr)
		}
	default:
		t.Fatal("runPump did not report the read error")
	}
}

func TestConfigResolvedHostDefault(t *testing.T) {
	c := Config{}
	if got := c.resolvedHost(); got != DefaultHost {
		t.Fatalf("resolvedHost() = %q, want %q", got, DefaultHost)
	}
	c.Host = "10.0.0.5"
	if got := c.resolvedHost(); got != "10.0.0.5" {
		t.Fatalf("resolvedHost() = %q, want %q", got, "10.0.0.5")
	}
}

func TestConfigValidate(t *testing.T) {
	base := Config{
		NetioPath: "/tmp/link1.sock", UDPLocal: 20001, UDPRemote: 20002,
		LocalInstance: 1, LocalAdapter: 0, LocalPort: 0, PseudoInstance: 501,
	}
	if err := base.validate(); err != nil {
		t.Fatalf("validate() on a well-formed config returned %v", err)
	}

	cases := []struct {
		name string
		mut  func(c Config) Config
	}{
		{"empty path", func(c Config) Config { c.NetioPath = ""; return c }},
		{"zero local port", func(c Config) Config { c.UDPLocal = 0; return c }},
		{"negative local port", func(c Config) Config { c.UDPLocal = -1; return c }},
		{"local port too large", func(c Config) Config { c.UDPLocal = 70000; return c }},
		{"zero remote port", func(c Config) Config { c.UDPRemote = 0; return c }},
		{"remote port too large", func(c Config) Config { c.UDPRemote = 99999; return c }},
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
			if err := tc.mut(base).validate(); err == nil {
				t.Fatalf("validate() accepted an invalid config (%s)", tc.name)
			}
		})
	}
}

func TestNewRejectsInvalidConfigOnAnyPlatform(t *testing.T) {
	// New must validate before touching any platform-specific socket code,
	// so this must fail identically on linux and non-linux.
	_, err := New(Config{})
	if err == nil {
		t.Fatal("New(Config{}) succeeded, want a validation error")
	}
}
