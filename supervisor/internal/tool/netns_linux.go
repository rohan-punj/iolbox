//go:build linux

package tool

// CreateNetns creates the isolated namespace before any peer is moved into
// it, so a failed setup cannot strand a veth in an implicit target.
func CreateNetns(nodeID int) error {
	return runCmds(netnsCreateNetnsCmds(nodeID))
}

// CreateVethPair builds the collision-free lab veth sequence whose bridge end
// remains in the root namespace for fabric capture and directional statistics.
func CreateVethPair(nodeID int) error {
	return runCmds(netnsCreateVethCmds(nodeID))
}

// AttachVethToBridge joins the root-side lab veth to the requested fabric
// bridge without moving either endpoint into another namespace.
func AttachVethToBridge(nodeID int, br string) error {
	return runCmds(netnsAttachVethCmds(nodeID, br))
}

// DetachVethFromBridge removes only the bridge membership so the caller can
// still perform the separate veth and namespace cleanup steps.
func DetachVethFromBridge(nodeID int) error {
	return runCmds(netnsDetachVethCmds(nodeID))
}

// DeleteNetns makes namespace teardown idempotent; a missing namespace is
// expected after crash recovery and is therefore never returned as an error.
func DeleteNetns(nodeID int) error {
	runCmdsBestEffort(netnsDeleteNetnsCmds(nodeID))
	return nil
}

// DeleteVeth makes lab-veth teardown idempotent; deleting the root-side end
// also removes a surviving moved peer.
func DeleteVeth(nodeID int) error {
	runCmdsBestEffort(netnsDeleteVethCmds(nodeID))
	return nil
}

// SetupMgmt creates the non-unix management escape hatch. Its caller must
// treat stripIfaceFlag, enforce_lab_iface, and SO_BINDTODEVICE as load-bearing
// again: mgmt0 is a second non-loopback interface in the namespace. This path
// is intentionally unexercised at the P1 gate because its only pack is unix.
func SetupMgmt(nodeID int) (hostCIDR, guestCIDR string, err error) {
	hostCIDR, guestCIDR, err = netnsMgmtCIDRs(nodeID)
	if err != nil {
		return "", "", err
	}
	if err := runCmds(netnsSetupMgmtCmds(nodeID)); err != nil {
		return "", "", err
	}
	return hostCIDR, guestCIDR, nil
}

// TeardownMgmt removes the TCP-only management policy, addresses, and veth;
// all of those operations are best-effort so repeated cleanup is harmless.
func TeardownMgmt(nodeID int) error {
	runCmdsBestEffort(netnsTeardownMgmtCmds(nodeID))
	return nil
}
