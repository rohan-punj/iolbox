package egress

import (
	"net"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name    string
		gateway net.IP
		addrs   []net.IP
		want    string
	}{
		{
			name:    "slirp default gateway",
			gateway: net.IPv4(10, 0, 2, 2),
			addrs:   []net.IP{net.IPv4(10, 0, 2, 15)},
			want:    Slirp,
		},
		{
			name:    "slirp addr without matching gateway",
			gateway: nil,
			addrs:   []net.IP{net.IPv4(10, 0, 2, 15)},
			want:    Slirp,
		},
		{
			name:    "slirp gateway alone (no addrs)",
			gateway: net.IPv4(10, 0, 2, 2),
			addrs:   nil,
			want:    Slirp,
		},
		{
			name:    "routed real NAT",
			gateway: net.IPv4(192, 168, 1, 1),
			addrs:   []net.IP{net.IPv4(192, 168, 1, 50)},
			want:    Routed,
		},
		{
			name:    "no data at all",
			gateway: nil,
			addrs:   nil,
			want:    Routed,
		},
		{
			name:    "10.0.x.x but not the slirp /24",
			gateway: net.IPv4(10, 0, 3, 1),
			addrs:   []net.IP{net.IPv4(10, 0, 3, 15)},
			want:    Routed,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classify(c.gateway, c.addrs); got != c.want {
				t.Fatalf("classify(%v, %v) = %q, want %q", c.gateway, c.addrs, got, c.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	// Forced values never consult the detector.
	if got := Resolve(Slirp); got != Slirp {
		t.Fatalf("Resolve(slirp) = %q, want %q", got, Slirp)
	}
	if got := Resolve(Routed); got != Routed {
		t.Fatalf("Resolve(routed) = %q, want %q", got, Routed)
	}
	// Unrecognized flag falls back to the permissive default.
	if got := Resolve("nonsense"); got != Routed {
		t.Fatalf("Resolve(nonsense) = %q, want %q", got, Routed)
	}
	// "auto" runs Detect(); off Linux (test host build) it is Routed, on Linux
	// the dev/CI box is not behind slirp, so Routed either way. Assert it returns
	// a valid value.
	if got := Resolve("auto"); got != Slirp && got != Routed {
		t.Fatalf("Resolve(auto) = %q, want slirp or routed", got)
	}
}

func TestNote(t *testing.T) {
	if Note(Slirp) == "" {
		t.Fatal("Note(slirp) should be non-empty")
	}
	if Note(Routed) != "" {
		t.Fatalf("Note(routed) = %q, want empty", Note(Routed))
	}
}

// TestParseHexLEIP guards the /proc/net/route little-endian decode on Linux
// (where the function is compiled).
func TestParseHexLEIPValue(t *testing.T) {
	testParseHexLEIP(t)
}
