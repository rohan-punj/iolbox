package fabric

import "testing"

func TestIsBenignNetemClear(t *testing.T) {
	for _, msg := range []string{
		"no such file or directory",
		"cannot delete qdisc with handle of zero",
		"invalid handle",
		"cannot find device tap0",
		"device tap0 does not exist",
		"no such device",
	} {
		if !isBenign(opNetemClear, msg) {
			t.Errorf("opNetemClear should tolerate %q", msg)
		}
	}
	for _, msg := range []string{
		"operation not permitted",
		"sudo: a password is required",
		"Error: unknown qdisc 'netem'",
	} {
		if isBenign(opNetemClear, msg) {
			t.Errorf("opNetemClear must not tolerate %q", msg)
		}
		if isBenign(opNetemSet, msg) {
			t.Errorf("opNetemSet must not tolerate %q", msg)
		}
	}
}
