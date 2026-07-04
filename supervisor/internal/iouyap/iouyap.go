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

	// The netio header (see HeaderSize) exists only on the unix-socket side
	// of this bridge. On netio->UDP the header is stripped, so the UDP mesh
	// (relay, VPCS, capture tee) carries raw ethernet frames. On UDP->netio
	// a fresh header is constructed from the three fields below, addressing
	// the frame to the REAL local IOL instance+interface this bridge serves
	// — not the pseudo-instance — because IOL drops any datagram whose dst
	// fields don't name one of its own interfaces (P0 root cause of the
	// bridged-link failures, docs/p0-spike.md "netio header layout").

	// LocalInstance is the IOL instance id of the node this bridge serves
	// (netmap.InstanceID of its lab node id). It becomes dst_id on every
	// frame delivered into the netio socket.
	LocalInstance int
	// LocalAdapter and LocalPort are the adapter/port coordinates of the
	// served interface (e.g. Ethernet1/0 -> adapter 1, port 0). They become
	// dst_port (via EncodePortByte) on every delivered frame.
	LocalAdapter int
	LocalPort    int
	// PseudoInstance is the pseudo-instance id this bridge's socket is
	// named for in the served IOL's NETMAP line ("<real>:<a>/<p>
	// <pseudo>:0/0"). It becomes src_id (with src_port 0) on every
	// delivered frame, so the frame appears to come from exactly the peer
	// the IOL believes that interface is wired to.
	PseudoInstance int
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
	if c.LocalInstance < 1 || c.LocalInstance > 1024 {
		return fmt.Errorf("iouyap: invalid LocalInstance %d (IOL accepts 1-1024)", c.LocalInstance)
	}
	if c.PseudoInstance < 1 || c.PseudoInstance > 1024 {
		return fmt.Errorf("iouyap: invalid PseudoInstance %d (IOL accepts 1-1024)", c.PseudoInstance)
	}
	if c.LocalAdapter < 0 || c.LocalAdapter > 15 || c.LocalPort < 0 || c.LocalPort > 15 {
		return fmt.Errorf("iouyap: interface %d/%d outside the 0-15 nibble range of the netio port byte",
			c.LocalAdapter, c.LocalPort)
	}
	return nil
}

// validateTap checks the config fields relevant to tap-mode bridges
// (TapBridge): everything validate checks except UDPLocal/UDPRemote, since
// tap mode has no UDP side at all — the frame's other end is a Linux tap
// device, not a UDP relay.
func (c Config) validateTap() error {
	if c.NetioPath == "" {
		return errors.New("iouyap: NetioPath is required")
	}
	if c.LocalInstance < 1 || c.LocalInstance > 1024 {
		return fmt.Errorf("iouyap: invalid LocalInstance %d (IOL accepts 1-1024)", c.LocalInstance)
	}
	if c.PseudoInstance < 1 || c.PseudoInstance > 1024 {
		return fmt.Errorf("iouyap: invalid PseudoInstance %d (IOL accepts 1-1024)", c.PseudoInstance)
	}
	if c.LocalAdapter < 0 || c.LocalAdapter > 15 || c.LocalPort < 0 || c.LocalPort > 15 {
		return fmt.Errorf("iouyap: interface %d/%d outside the 0-15 nibble range of the netio port byte",
			c.LocalAdapter, c.LocalPort)
	}
	return nil
}

// stripToUDP is the netio->UDP transform: drop the 8-byte netio header so the
// UDP mesh carries the raw ethernet frame. Datagrams too short to hold a
// header are dropped (ok=false), like a runt frame on a real link.
func stripToUDP(datagram []byte) (out []byte, ok bool) {
	if len(datagram) < HeaderSize {
		return nil, false
	}
	return datagram[HeaderSize:], true
}

// wrapToNetio is the UDP->netio transform: prepend a netio header addressing
// the raw ethernet frame to the real local IOL instance+interface, sourced
// from the pseudo-instance its NETMAP names as the peer. Empty datagrams are
// dropped (ok=false) — there is no such thing as a zero-byte ethernet frame.
func (c Config) wrapToNetio(frame []byte) (out []byte, ok bool) {
	if len(frame) == 0 {
		return nil, false
	}
	return WithPayload(Header{
		DstID:   uint16(c.LocalInstance),
		SrcID:   uint16(c.PseudoInstance),
		DstPort: EncodePortByte(c.LocalAdapter, c.LocalPort),
		SrcPort: EncodePortByte(0, 0),
		MsgType: MsgTypeData,
	}, frame), true
}

// dgram is a length-delimited datagram used by the in-memory fakes in tests
// and as the common unit the pump functions operate on.
type dgram = []byte

// pumpOne performs one read-transform-write step using injectable read/write
// functions, so the exact same logic backs both the real linux sockets and
// the in-memory fakes used in unit tests. read should block until a datagram
// is available or ctx is done; it returns (nil, ctx.Err()) style errors on
// cancellation which pumpOne treats as a clean stop. transform is stripToUDP
// or Config.wrapToNetio depending on the pump's direction.
func pumpOne(read func() ([]byte, error), write func([]byte) error, transform func([]byte) ([]byte, bool)) error {
	datagram, err := read()
	if err != nil {
		return err
	}
	out, ok := transform(datagram)
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
func runPump(ctx context.Context, read func() ([]byte, error), write func([]byte) error, transform func([]byte) ([]byte, bool), errs chan<- error) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := pumpOne(read, write, transform); err != nil {
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
