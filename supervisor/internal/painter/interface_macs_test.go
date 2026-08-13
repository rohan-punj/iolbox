package painter

import (
	"strings"
	"testing"
)

const interfaceMACFiltered = `
Ethernet0/0 is up, line protocol is up
  Hardware is AmdP2, address is aabb.cc00.0800 (bia aabb.cc00.0800)
Et0/1 is administratively down, line protocol is down
  Hardware is AmdP2, address is AABB.CC00.0801 (bia aabb.cc00.0801)
Serial0/0 is down, line protocol is down
  Hardware is HDLC, no address
`

const interfaceMACFull = `
Ethernet0/0 is up, line protocol is up
  Hardware is AmdP2, address is aabb.cc00.0800 (bia aabb.cc00.0800)
  Internet address is 192.0.2.1/24
  Last input never, output 00:00:01

Ethernet0/1 is down, line protocol is down (notconnect)
  Hardware is AmdP2, address is 02:aa:bb:cc:dd:01 (bia 02:aa:bb:cc:dd:01)
  MTU 1500 bytes, BW 100000 Kbit

Serial0/0 is down, line protocol is down
  Hardware is HDLC

Vlan1 is up, line protocol is up
  Hardware is EtherSVI, address is 0200.0000.0001 (bia 0200.0000.0001)
`

func TestParseInterfaceMACs(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []InterfaceMAC
	}{
		{
			name: "filtered and state variance",
			in:   interfaceMACFiltered,
			want: []InterfaceMAC{
				{Interface: "Ethernet0/0", InterfaceNorm: "ethernet0/0", MAC: "aa:bb:cc:00:08:00"},
				{Interface: "Et0/1", InterfaceNorm: "et0/1", MAC: "aa:bb:cc:00:08:01"},
			},
		},
		{
			name: "full output and logical interface",
			in:   interfaceMACFull,
			want: []InterfaceMAC{
				{Interface: "Ethernet0/0", InterfaceNorm: "ethernet0/0", MAC: "aa:bb:cc:00:08:00"},
				{Interface: "Ethernet0/1", InterfaceNorm: "ethernet0/1", MAC: "02:aa:bb:cc:dd:01"},
				{Interface: "Vlan1", InterfaceNorm: "vlan1", MAC: "02:00:00:00:00:01"},
			},
		},
		{
			name: "current address wins over bia",
			in: `Ethernet0/0 is up, line protocol is up
  Hardware is AmdP2, address is aabb.cc00.0801 (bia aabb.cc00.0800)
`,
			want: []InterfaceMAC{{Interface: "Ethernet0/0", InterfaceNorm: "ethernet0/0", MAC: "aa:bb:cc:00:08:01"}},
		},
		{
			name: "crlf and first valid address",
			in: strings.ReplaceAll(`
Ethernet0/0 is reset, line protocol is down (testing)
  Hardware is AmdP2, address is 0000.0000.0000 (bia 0200.0000.0001)
  Hardware is AmdP2, address is aa-bb-cc-dd-ee-ff (bia 0200.0000.0001)
`, "\n", "\r\n"),
			want: []InterfaceMAC{{Interface: "Ethernet0/0", InterfaceNorm: "ethernet0/0", MAC: "aa:bb:cc:dd:ee:ff"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseInterfaceMACs(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d: %+v", len(got), len(tc.want), got)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("record[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestParseInterfaceMACsMalformedAndPartial(t *testing.T) {
	in := `
  Hardware is AmdP2, address is 0200.0000.0001 (bia 0200.0000.0001)
Ethernet0/0 is up, line protocol is up
  Hardware is AmdP2, address is zzzz.cc00.0800
Ethernet0/1 is up, line protocol is up
  Hardware is AmdP2, address is aabb.cc00.0800
Ethernet0/1 is up, line protocol is up
  Hardware is AmdP2, address is aabb..cc00.0800
Ethernet0/2 is up, line protocol is up
  Hardware is AmdP2, address is aabb.cc00.080000
Ethernet0/6 is up, line protocol is up
  Hardware is AmdP2, address is aabb.cc00.08
Ethernet0/3 is up, line protocol is up
  Hardware is AmdP2, address is ffff.ffff.ffff
Ethernet0/4 is up, line protocol is up
  Hardware is AmdP2, address is 0000.0000.0000
Ethernet0/5 is up, line protocol is up
  Hardware is AmdP2
Serial0/0 is down, line protocol is down
  Hardware is HDLC
% Invalid input detected at '^' marker.
`

	got := ParseInterfaceMACs(in)
	if len(got) != 1 {
		t.Fatalf("len = %d, want one valid partial result: %+v", len(got), got)
	}
	if got[0].Interface != "Ethernet0/1" || got[0].MAC != "aa:bb:cc:00:08:00" {
		t.Fatalf("partial result = %+v", got[0])
	}
}

func TestParseInterfaceMACsEmptyAndErrors(t *testing.T) {
	for _, in := range []string{"", "\n\n", "% Invalid input detected", "show interfaces: incomplete"} {
		if got := ParseInterfaceMACs(in); len(got) != 0 {
			t.Errorf("ParseInterfaceMACs(%q) = %+v, want empty", in, got)
		}
	}
}
