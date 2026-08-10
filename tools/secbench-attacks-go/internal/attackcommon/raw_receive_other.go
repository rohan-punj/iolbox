//go:build !linux

package attackcommon

import (
	"errors"
	"time"
)

// RawReceiver is the unsupported-platform placeholder for the Linux raw
// packet receiver.
type RawReceiver struct{}

// OpenRawReceiver reports that AF_PACKET receiving is Linux-only.
func OpenRawReceiver(string) (*RawReceiver, error) {
	return nil, errors.New("raw AF_PACKET receiving is only supported on Linux")
}

// SetReadDeadline reports that AF_PACKET receiving is Linux-only.
func (*RawReceiver) SetReadDeadline(time.Time) error {
	return errors.New("raw AF_PACKET receiving is only supported on Linux")
}

// ReadFrame reports that AF_PACKET receiving is Linux-only.
func (*RawReceiver) ReadFrame() ([]byte, error) {
	return nil, errors.New("raw AF_PACKET receiving is only supported on Linux")
}

// Close is a no-op for the unsupported-platform placeholder.
func (*RawReceiver) Close() error { return nil }
