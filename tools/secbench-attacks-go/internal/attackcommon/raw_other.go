//go:build !linux

package attackcommon

import "errors"

type RawSender struct{}

func OpenRawSender(string) (*RawSender, error) {
	return nil, errors.New("raw AF_PACKET sending is only supported on Linux")
}

func (*RawSender) Send([]byte) error {
	return errors.New("raw AF_PACKET sending is only supported on Linux")
}

func (*RawSender) Close() error { return nil }
