//go:build !linux

package egress

// Detect returns the permissive default off Linux: the slirp signature only
// exists inside the Linux runtime (there is no /proc/net/route on the dev box),
// and non-launcher runtimes must never be mislabeled. Keeps the Windows
// cross-compile green.
func Detect() string {
	return Routed
}
