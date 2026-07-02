package iouyap

import (
	"context"
	"errors"
	"fmt"
)

// DefaultHost is used when Config.Host is empty. The bridge, IOL, and the
// relay all run inside the same runtime, so loopback is the normal case;
// Host exists mainly for the cross-host relay scenario described in
// docs/p0-spike.md correction #2, where the UDP side may need to reach a
// peer supervisor's relay over a real interface.
const DefaultHost = "127.0.0.1"

// Config configures one Bridge: the IOL netio unix-domain socket on one side
// and the UDP relay endpoint on the other.
type Config struct {
	// NetioPath is the filesystem path of the unix-domain datagram socket
	// IOL connects to for this link's interface (per its NETMAP entry).
	NetioPath string
	// UDPLocal is the local UDP port this bridge binds to receive frames
	// forwarded by the relay.
	UDPLocal int
	// UDPRemote is the relay's UDP port this bridge forwards frames to.
	UDPRemote int
	// Host is the address for both the UDPLocal bind and the UDPRemote
	// peer. Defaults to DefaultHost ("127.0.0.1") when empty.
	Host string
	// StripHeader documents and controls whether the 8-byte netio header
	// (see HeaderSize) is present on datagrams read from the unix socket
	// and must be re-added on datagrams written back to it.
	//
	// ASSUMPTION (P0-VERIFY): real IOL prepends the netio header on every
	// datagram it sends over the unix socket, and expects it prepended on
	// every datagram it receives — i.e. the header travels end-to-end
	// unix<->UDP unchanged, exactly like relay.StripIOLHeader/IOLHeaderSize
	// assumes on the UDP<->UDP hop. StripHeader=true (the expected/default
	// setting) means: on unix->UDP, validate+pass the header through
	// unmodified; on UDP->unix, same. It does NOT mean the header is
	// dropped — "strip" here names the operation this bridge shares with
	// relay.StripIOLHeader (locating where the ethernet payload starts),
	// not a change in framing. Set StripHeader=false only if P0 discovers
	// IOL's unix-socket framing has NO header (raw ethernet frames only),
	// in which case the bridge passes datagrams through byte-for-byte with
	// no header interpretation at all.
	StripHeader bool
}

// resolvedHost returns cfg.Host, defaulting to DefaultHost.
func (c Config) resolvedHost() string {
	if c.Host == "" {
		return DefaultHost
	}
	return c.Host
}

// validate checks the config fields that are meaningful on every platform.
// Platform-specific New implementations call this before touching sockets.
func (c Config) validate() error {
	if c.NetioPath == "" {
		return errors.New("iouyap: NetioPath is required")
	}
	if c.UDPLocal <= 0 || c.UDPLocal > 65535 {
		return fmt.Errorf("iouyap: invalid UDPLocal port %d", c.UDPLocal)
	}
	if c.UDPRemote <= 0 || c.UDPRemote > 65535 {
		return fmt.Errorf("iouyap: invalid UDPRemote port %d", c.UDPRemote)
	}
	return nil
}

// transform is the pure per-datagram logic applied in both directions: given
// StripHeader, decide what (if anything) needs validating/rewriting before a
// datagram read from one side is written to the other. Today the netio and
// UDP framing are identical (same 8-byte header, same payload), so this is a
// pass-through once the length sanity check succeeds; it exists as a single
// named seam so P0 findings that require header rewriting (e.g. swapping
// src/dst ids between the two sides) land in one place instead of being
// duplicated across the linux pump loops.
//
// It returns the datagram to send on, or ok=false if the datagram should be
// dropped (too short to contain a header when one is expected).
func transform(datagram []byte, stripHeader bool) (out []byte, ok bool) {
	if !stripHeader {
		return datagram, true
	}
	if len(datagram) < HeaderSize {
		return nil, false
	}
	return datagram, true
}

// dgram is a length-delimited datagram used by the in-memory fakes in tests
// and as the common unit the pump functions operate on.
type dgram = []byte

// pumpOne performs one read-transform-write step using injectable read/write
// functions, so the exact same logic backs both the real linux sockets and
// the in-memory fakes used in unit tests. read should block until a datagram
// is available or ctx is done; it returns (nil, ctx.Err()) style errors on
// cancellation which pumpOne treats as a clean stop.
func pumpOne(read func() ([]byte, error), write func([]byte) error, stripHeader bool) error {
	datagram, err := read()
	if err != nil {
		return err
	}
	out, ok := transform(datagram, stripHeader)
	if !ok {
		// Short/malformed datagram: drop silently, same as a dropped packet
		// on a real link. Not an error worth stopping the pump for.
		return nil
	}
	return write(out)
}

// runPump loops pumpOne until ctx is cancelled or a non-cancellation error
// occurs from read/write, reporting the latter on errs (best-effort, never
// blocks: errs must be buffered or drained).
func runPump(ctx context.Context, read func() ([]byte, error), write func([]byte) error, stripHeader bool, errs chan<- error) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := pumpOne(read, write, stripHeader); err != nil {
			if ctx.Err() != nil {
				return
			}
			select {
			case errs <- err:
			default:
			}
			return
		}
	}
}
