//go:build linux

package fabric

import "testing"

// TestIsBenign pins which representative `ip` error strings are treated as
// idempotent no-ops per operation, vs. real failures that must propagate.
func TestIsBenign(t *testing.T) {
	cases := []struct {
		name   string
		op     op
		output string
		want   bool
	}{
		{"create tap: file exists", opCreateTap, "RTNETLINK answers: File exists", true},
		{"create tap: busy", opCreateTap, "RTNETLINK answers: Device or resource busy", true},
		{"create tap: real error", opCreateTap, "operation not permitted", false},
		{"create bridge: file exists", opCreateBridge, "RTNETLINK answers: File exists", true},
		{"create bridge: real error", opCreateBridge, "permission denied", false},

		{"delete tap: cannot find device", opDeleteTap, `Cannot find device "iol3_17"`, true},
		{"delete tap: does not exist", opDeleteTap, "device does not exist", true},
		{"delete tap: no such device", opDeleteTap, "ip: no such device", true},
		{"delete tap: real error", opDeleteTap, "operation not permitted", false},

		{"delete bridge: cannot find device", opDeleteBridge, `Cannot find device "iolbr12"`, true},
		{"delete bridge: real error", opDeleteBridge, "device busy elsewhere", false},

		{"detach: cannot find device", opDetach, `Cannot find device "iol3_17"`, true},
		{"detach: real error", opDetach, "operation not permitted", false},

		{"attach: file exists", opAttach, "RTNETLINK answers: File exists", true},
		{"attach: real error", opAttach, "no such device", false}, // not in the attach op's benign set
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isBenign(c.op, toLowerASCII(c.output))
			if got != c.want {
				t.Fatalf("isBenign(%v, %q) = %v, want %v", c.op, c.output, got, c.want)
			}
		})
	}
}

// toLowerASCII mirrors the strings.ToLower call sites do before calling
// isBenign, kept local to the test so it doesn't need to import strings just
// for this one call.
func toLowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
