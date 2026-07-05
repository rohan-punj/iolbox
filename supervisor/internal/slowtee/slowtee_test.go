package slowtee

import "testing"

// TestIsSlowProtocols exercises the pure destination-MAC filter that decides
// whether a frame is forwarded across the tee. AF_PACKET itself needs
// root/netns, so (mirroring dirstat_test.go) only this pure logic is unit
// tested here.
func TestIsSlowProtocols(t *testing.T) {
	cases := []struct {
		name  string
		frame []byte
		want  bool
	}{
		{
			name: "LACP slow-protocols frame",
			frame: []byte{
				0x01, 0x80, 0xc2, 0x00, 0x00, 0x02, // dst: slow-protocols multicast
				0x00, 0x11, 0x22, 0x33, 0x44, 0x55, // src
				0x88, 0x09, // ethertype: slow protocols
				0x01, 0x01, // LACP subtype + version (payload start)
			},
			want: true,
		},
		{
			name: "normal unicast frame",
			frame: []byte{
				0x00, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, // dst: unicast
				0x00, 0x11, 0x22, 0x33, 0x44, 0x55, // src
				0x08, 0x00, // ethertype: IPv4
				0x45, 0x00,
			},
			want: false,
		},
		{
			name: "broadcast frame",
			frame: []byte{
				0xff, 0xff, 0xff, 0xff, 0xff, 0xff, // dst: broadcast
				0x00, 0x11, 0x22, 0x33, 0x44, 0x55, // src
				0x08, 0x06, // ethertype: ARP
				0x00, 0x01,
			},
			want: false,
		},
		{
			name: "CDP frame (different 01:80:c2 multicast)",
			frame: []byte{
				0x01, 0x00, 0x0c, 0xcc, 0xcc, 0xcc, // dst: CDP/VTP multicast, not slow-proto
				0x00, 0x11, 0x22, 0x33, 0x44, 0x55, // src
				0xaa, 0xaa, // SNAP
				0x03, 0x00,
			},
			want: false,
		},
		{
			name: "other 01:80:c2:00:00:0X multicast (e.g. STP)",
			frame: []byte{
				0x01, 0x80, 0xc2, 0x00, 0x00, 0x00, // dst: STP multicast, not LACP
				0x00, 0x11, 0x22, 0x33, 0x44, 0x55, // src
				0x08, 0x00,
				0x00, 0x00,
			},
			want: false,
		},
		{
			name:  "short frame below Ethernet header length",
			frame: []byte{0x01, 0x80, 0xc2, 0x00, 0x00},
			want:  false,
		},
		{
			name:  "empty frame",
			frame: []byte{},
			want:  false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isSlowProtocols(c.frame); got != c.want {
				t.Errorf("isSlowProtocols(%v) = %v, want %v", c.frame, got, c.want)
			}
		})
	}
}
