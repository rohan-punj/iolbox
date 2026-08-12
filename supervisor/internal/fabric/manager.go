package fabric

import "strings"

// op identifies a privileged fabric operation so idempotent command failures
// can be classified without executing a command. It is shared with the pure
// tests, which intentionally run on the Windows development host too.
type op int

const (
	opCreateTap op = iota
	opDeleteTap
	opCreateBridge
	opDeleteBridge
	opAttach
	opDetach
	opNetemSet
	opNetemClear
)

// isBenign reports whether outputLower (already lowercased) from the given
// operation's first command represents an idempotent no-op.
func isBenign(o op, outputLower string) bool {
	switch o {
	case opCreateTap, opCreateBridge:
		return strings.Contains(outputLower, "file exists") ||
			strings.Contains(outputLower, "device or resource busy")
	case opDeleteTap, opDeleteBridge, opDetach:
		return strings.Contains(outputLower, "cannot find device") ||
			strings.Contains(outputLower, "does not exist") ||
			strings.Contains(outputLower, "no such device")
	case opAttach:
		// Re-attaching to the same bridge is a no-op in practice.
		return strings.Contains(outputLower, "file exists")
	case opNetemClear:
		// Both absent-qdisc and absent-device errors are expected during
		// idempotent teardown, including after a partial endpoint startup.
		return strings.Contains(outputLower, "no such file or directory") ||
			strings.Contains(outputLower, "cannot delete qdisc with handle of zero") ||
			strings.Contains(outputLower, "invalid handle") ||
			strings.Contains(outputLower, "cannot find device") ||
			strings.Contains(outputLower, "does not exist") ||
			strings.Contains(outputLower, "no such device")
	default:
		return false
	}
}
