package attackcommon

import (
	"reflect"
	"testing"
	"time"
)

func TestBaseParserDefaults(t *testing.T) {
	fs, opts := BaseParser("test")
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("Parse(nil): %v", err)
	}
	if opts.Iface != "eth1" || opts.Selftest || opts.Count != 0 || opts.Interval != 1.0 {
		t.Fatalf("defaults = %+v", *opts)
	}
}

func TestBaseParserValues(t *testing.T) {
	fs, opts := BaseParser("test")
	if err := fs.Parse([]string{
		"--iface", "eth1",
		"--selftest",
		"--count", "3",
		"--interval", "0.25",
	}); err != nil {
		t.Fatalf("Parse(args): %v", err)
	}
	want := Options{Iface: "eth1", Selftest: true, Count: 3, Interval: 0.25}
	if !reflect.DeepEqual(*opts, want) {
		t.Fatalf("options = %+v, want %+v", *opts, want)
	}
}

func TestFormatStatus(t *testing.T) {
	now := time.Date(2026, time.August, 8, 12, 34, 56, 0, time.FixedZone("test", -5*60*60))
	got := FormatStatus("SENT", "packet #1", now)
	want := "[SENT] 12:34:56 packet #1\n"
	if got != want {
		t.Fatalf("FormatStatus() = %q, want %q", got, want)
	}
}

func TestCDPChecksumKnownPayload(t *testing.T) {
	// CDPv2 header with version 2, TTL 180, zero checksum, and one empty
	// Device-ID TLV (type 1, length 4). The Scapy-compatible checksum is 0xfd46.
	payload := []byte{0x02, 0xb4, 0x00, 0x00, 0x00, 0x01, 0x00, 0x04}
	if got := CDPChecksum(payload); got != 0xfd46 {
		t.Fatalf("CDPChecksum() = %#04x, want %#04x", got, uint16(0xfd46))
	}
}

func TestCDPChecksumOddLengthScapyPadding(t *testing.T) {
	// The supplied Scapy helper moves an odd final byte into the low-order
	// position, so this is deliberately not append-zero-at-the-end padding.
	if got := CDPChecksum([]byte{0x01, 0x02, 0x03}); got != 0xfefa {
		t.Fatalf("odd CDPChecksum() = %#04x, want %#04x", got, uint16(0xfefa))
	}
}

func TestRunLoopFiniteCount(t *testing.T) {
	var got []int
	n, err := RunLoop(3, 0, func(iteration int) error {
		got = append(got, iteration)
		return nil
	})
	if err != nil {
		t.Fatalf("RunLoop(): %v", err)
	}
	if n != 3 || !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Fatalf("RunLoop() = n=%d iterations=%v", n, got)
	}
}

func TestForgedMAC(t *testing.T) {
	mac := ForgedMAC()
	if len(mac) != 6 || mac[0] != 0x02 || mac[0]&1 != 0 {
		t.Fatalf("ForgedMAC() = %s", mac)
	}
}
