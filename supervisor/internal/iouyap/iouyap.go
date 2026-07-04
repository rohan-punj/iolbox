package iouyap

import (
	"context"
	"errors"
	"fmt"
)

// Config configures one netio<->tap bridge: the IOL netio unix-domain socket on
// one side and a Linux tap device on the other.
type Config struct {
	// NetioPath is the filesystem path of the unix-domain datagram socket
	// IOL connects to for this link's interface (per its NETMAP entry).
	NetioPath string

	// The netio header (see HeaderSize) exists only on the unix-socket side
	// of this bridge. On netio->tap the header is stripped, so the tap carries
	// raw ethernet frames. On tap->netio a fresh header is constructed from the
	// three fields below, addressing the frame to the REAL local IOL
	// instance+interface this bridge serves — not the pseudo-instance — because
	// IOL drops any datagram whose dst fields don't name one of its own
	// interfaces (P0 root cause, docs/p0-spike.md "netio header layout").

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

// validateTap checks the config fields relevant to the netio<->tap bridge
// (TapBridge). There is no UDP side — the frame's other end is a Linux tap
// device.
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
