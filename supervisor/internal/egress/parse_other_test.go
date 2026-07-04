//go:build !linux

package egress

import "testing"

// testParseHexLEIP is a no-op off Linux: parseHexLEIP is linux-only (it decodes
// /proc/net/route, which does not exist elsewhere). Keeps the shared test file
// compiling on the Windows cross-build.
func testParseHexLEIP(t *testing.T) {
	t.Skip("parseHexLEIP is only compiled on linux")
}
