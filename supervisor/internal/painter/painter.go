// Package painter parses live IOS 17.x `show` output into structured,
// canvas-mappable protocol-decision data for the Topology Painter overlays
// (STP / OSPF / EIGRP / BGP) and other supervisor live-state features.
// Everything here is a pure string->struct
// transform with NO device I/O, so the parsers are unit-testable on every
// platform without a running node. The device-touching console scrape that
// feeds these parsers lives in the server package behind //go:build linux.
//
// Tolerance contract: every parser must survive version banners, wrapped
// lines, blank output and a leading `% ...` IOS error by returning an empty
// (zero) result — never a panic. Callers treat an empty result as "no data
// for this node" (node not running / protocol not configured / still
// converging).
package painter

import "strings"

// PortRole is an STP port role as IOS reports it, normalized to the canonical
// four-letter form.
type PortRole string

const (
	RoleRoot PortRole = "Root" // this port is the node's path to the root bridge
	RoleDesg PortRole = "Desg" // designated port for its segment (forwarding)
	RoleAltn PortRole = "Altn" // alternate — a backup path to root, blocked
	RoleBack PortRole = "Back" // backup — a redundant link to the same segment, blocked
)

// PortState is an STP port state, normalized to FWD / BLK / LRN / LIS / DIS.
type PortState string

const (
	StateFWD PortState = "FWD"
	StateBLK PortState = "BLK"
	StateLRN PortState = "LRN"
	StateLIS PortState = "LIS"
	StateDIS PortState = "DIS"
)

// normIface returns a lowercased, whitespace-free short interface name (IOS
// already abbreviates in `show` output, e.g. "Et0/0"). We keep the raw form as
// reported and additionally emit this normalized form so the frontend can match
// either spelling against the lab doc's endpoint interface strings.
func normIface(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// isErrLine reports whether a line is an IOS `%` error / "not configured" style
// message. A block whose only meaningful content is such a line yields an empty
// parse result.
func isErrLine(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "%")
}
