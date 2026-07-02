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

func TestTransformPassesValidDatagram(t *testing.T) {
	datagram := WithPayload(Header{DstID: 1, SrcID: 2}, []byte("hello"))
	out, ok := transform(datagram, true)
	if !ok {
		t.Fatal("transform reported not-ok for a valid framed datagram")
	}
	if !bytes.Equal(out, datagram) {
		t.Fatalf("transform mutated a datagram it should pass through: got % x, want % x", out, datagram)
	}
}

func TestTransformDropsShortDatagramWhenHeaderExpected(t *testing.T) {
	short := make([]byte, HeaderSize-1)
	_, ok := transform(short, true)
	if ok {
		t.Fatal("transform accepted a too-short datagram when StripHeader=true")
	}
}

func TestTransformPassthroughWhenHeaderNotExpected(t *testing.T) {
	// StripHeader=false: no header interpretation at all, so even a
	// zero-length datagram passes through untouched.
	for _, in := range [][]byte{nil, {}, {0x01}, make([]byte, HeaderSize-1)} {
		out, ok := transform(in, false)
		if !ok {
			t.Fatalf("transform(%v, false) reported not-ok, want pass-through", in)
		}
		if !bytes.Equal(out, in) {
			t.Fatalf("transform(%v, false) = %v, want unchanged", in, out)
		}
	}
}

func TestPumpOneForwardsOneDatagram(t *testing.T) {
	in := newFakeChan(1)
	out := newFakeChan(1)
	datagram := WithPayload(Header{DstID: 5, SrcID: 6}, []byte("payload"))
	in.send(datagram)

	ctx := context.Background()
	if err := pumpOne(in.readFunc(ctx), out.writeFunc(), true); err != nil {
		t.Fatalf("pumpOne returned error: %v", err)
	}

	select {
	case got := <-out.ch:
		if !bytes.Equal(got, datagram) {
			t.Fatalf("forwarded datagram = % x, want % x", got, datagram)
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
	if err := pumpOne(in.readFunc(ctx), out.writeFunc(), true); err != nil {
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
	if err := pumpOne(read, write, true); !errors.Is(err, wantErr) {
		t.Fatalf("pumpOne error = %v, want %v", err, wantErr)
	}
}

func TestPumpOnePropagatesWriteError(t *testing.T) {
	wantErr := errors.New("write boom")
	datagram := WithPayload(Header{}, []byte("x"))
	read := func() ([]byte, error) { return datagram, nil }
	write := func([]byte) error { return wantErr }
	if err := pumpOne(read, write, true); !errors.Is(err, wantErr) {
		t.Fatalf("pumpOne error = %v, want %v", err, wantErr)
	}
}

func TestRunPumpForwardsMultipleDatagramsThenStopsOnCancel(t *testing.T) {
	in := newFakeChan(4)
	out := newFakeChan(4)
	want := [][]byte{
		WithPayload(Header{SrcID: 1}, []byte("a")),
		WithPayload(Header{SrcID: 2}, []byte("bb")),
		WithPayload(Header{SrcID: 3}, []byte("ccc")),
	}
	for _, d := range want {
		in.send(d)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		runPump(ctx, in.readFunc(ctx), out.writeFunc(), true, errs)
		close(done)
	}()

	for i, w := range want {
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
	runPump(ctx, read, write, true, errs)

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
	base := Config{NetioPath: "/tmp/link1.sock", UDPLocal: 20001, UDPRemote: 20002}
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
