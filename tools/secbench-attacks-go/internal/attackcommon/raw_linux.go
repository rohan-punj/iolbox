//go:build linux

package attackcommon

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// Linux AF_PACKET expects the protocol in network byte order. ETH_P_ALL is
// 0x0003 in host notation, hence 0x0300 on a little-endian Linux target.
const rawProtocol uint16 = 0x0300

type RawSender struct {
	fd      int
	ifindex int
}

func OpenRawSender(ifaceName string) (*RawSender, error) {
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
	return &RawSender{fd: fd, ifindex: iface.Index}, nil
}

func (s *RawSender) Send(frame []byte) error {
	if s == nil || s.fd < 0 {
		return fmt.Errorf("raw sender is closed")
	}
	if len(frame) < 6 {
		return fmt.Errorf("Ethernet frame is too short: %d bytes", len(frame))
	}
	sa := &unix.SockaddrLinklayer{
		Protocol: rawProtocol,
		Ifindex:  s.ifindex,
		Halen:    6,
	}
	copy(sa.Addr[:], frame[:6])
	if err := unix.Sendto(s.fd, frame, 0, sa); err != nil {
		return err
	}
	return nil
}

func (s *RawSender) Close() error {
	if s == nil || s.fd < 0 {
		return nil
	}
	err := unix.Close(s.fd)
	s.fd = -1
	return err
}
