//go:build linux

package egress

import (
	"net"
	"testing"
)

// testParseHexLEIP verifies the /proc/net/route little-endian hex decode. It is
// linux-only because parseHexLEIP lives in detect_linux.go.
func testParseHexLEIP(t *testing.T) {
	cases := []struct {
		in   string
		want net.IP // nil = expect nil
	}{
		{"0202000A", net.IPv4(10, 0, 2, 2)}, // slirp gateway, LE encoded
		{"00000000", net.IPv4(0, 0, 0, 0)},
		{"0100A8C0", net.IPv4(192, 168, 0, 1)},
		{"zzzz", nil},     // wrong length
		{"GGGGGGGG", nil}, // not hex
	}
	for _, c := range cases {
		got := parseHexLEIP(c.in)
		if c.want == nil {
			if got != nil {
				t.Fatalf("parseHexLEIP(%q) = %v, want nil", c.in, got)
			}
			continue
		}
		if got == nil || !got.Equal(c.want) {
			t.Fatalf("parseHexLEIP(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
