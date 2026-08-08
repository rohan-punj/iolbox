//go:build !linux

package tool

// Detect reports no support off Linux because none of the namespace, veth,
// delegated-cgroup, capability, or AF_UNIX runtime probes is meaningful there.
func Detect(root CgroupRoot) Capabilities {
	return Capabilities{}
}
