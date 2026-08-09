//go:build linux

package attackcommon

import (
	"fmt"
	"net"
	"time"

	"golang.org/x/sys/unix"
)

// RawReceiver reads complete Ethernet frames from a bound Linux AF_PACKET
// socket. Filtering is intentionally left to the caller's frame parsers.
type RawReceiver struct {
	fd int
}

// OpenRawReceiver opens and binds a raw packet receiver to ifaceName.
func OpenRawReceiver(ifaceName string) (*RawReceiver, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("resolve interface %q: %w", ifaceName, err)
	}
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(rawProtocol))
	if err != nil {
		return nil, fmt.Errorf("open AF_PACKET socket: %w", err)
	}
	sa := &unix.SockaddrLinklayer{Protocol: rawProtocol, Ifindex: iface.Index}
	if err := unix.Bind(fd, sa); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("bind AF_PACKET socket to %q: %w", ifaceName, err)
	}
	return &RawReceiver{fd: fd}, nil
}

// SetReadDeadline sets the receive timeout relative to the supplied absolute
// deadline. A zero deadline restores blocking reads.
func (r *RawReceiver) SetReadDeadline(deadline time.Time) error {
	if r == nil || r.fd < 0 {
		return fmt.Errorf("raw receiver is closed")
	}
	var timeout unix.Timeval
	if !deadline.IsZero() {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			remaining = time.Microsecond
		}
		timeout.Sec = int64(remaining / time.Second)
		timeout.Usec = int64((remaining % time.Second) / time.Microsecond)
		if timeout.Sec == 0 && timeout.Usec == 0 {
			timeout.Usec = 1
		}
	}
	if err := unix.SetsockoptTimeval(r.fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &timeout); err != nil {
		return fmt.Errorf("set raw receiver read deadline: %w", err)
	}
	return nil
}

// ReadFrame reads one complete Ethernet frame.
func (r *RawReceiver) ReadFrame() ([]byte, error) {
	if r == nil || r.fd < 0 {
		return nil, fmt.Errorf("raw receiver is closed")
	}
	frame := make([]byte, 65536)
	n, _, err := unix.Recvfrom(r.fd, frame, 0)
	if err != nil {
		return nil, err
	}
	return frame[:n], nil
}

// Close releases the raw receive socket.
func (r *RawReceiver) Close() error {
	if r == nil || r.fd < 0 {
		return nil
	}
	err := unix.Close(r.fd)
	r.fd = -1
	return err
}
