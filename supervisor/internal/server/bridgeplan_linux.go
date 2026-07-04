//go:build linux

package server

import "os"

// currentUID returns the process uid, which names the netio socket directory
// /tmp/netio<uid> IOL and the netio<->tap iouyap bridges share (confirmed via
// lsof on real IOL, docs/p0-spike.md).
func currentUID() int { return os.Getuid() }
