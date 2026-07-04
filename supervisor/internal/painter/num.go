package painter

import (
	"strconv"
	"strings"
)

// atoi parses an int, returning 0 on any error (tolerant column parsing).
func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// atoi64 parses an int64, returning 0 on any error (EIGRP composite metrics
// overflow int32 on 32-bit builds).
func atoi64(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// itoa is strconv.Itoa, aliased for symmetry with atoi in reason strings.
func itoa(n int) string { return strconv.Itoa(n) }
