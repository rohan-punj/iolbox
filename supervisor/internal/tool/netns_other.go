//go:build !linux

package tool

// CreateNetns reports the platform boundary without attempting a partial
// network setup on a host that has no Linux network namespaces.
func CreateNetns(nodeID int) error { return ErrUnsupportedPlatform }

// CreateVethPair reports the platform boundary without invoking host tools.
func CreateVethPair(nodeID int) error { return ErrUnsupportedPlatform }

// AttachVethToBridge reports the platform boundary without changing a bridge.
func AttachVethToBridge(nodeID int, br string) error { return ErrUnsupportedPlatform }

// DetachVethFromBridge reports the platform boundary without changing a
// bridge.
func DetachVethFromBridge(nodeID int) error { return ErrUnsupportedPlatform }

// DeleteNetns reports the platform boundary; there is no object to clean on
// this platform.
func DeleteNetns(nodeID int) error { return ErrUnsupportedPlatform }

// DeleteVeth reports the platform boundary; there is no object to clean on
// this platform.
func DeleteVeth(nodeID int) error { return ErrUnsupportedPlatform }

// SetupMgmt reports the platform boundary without returning synthetic
// addresses that a caller might accidentally use.
func SetupMgmt(nodeID int) (hostCIDR, guestCIDR string, err error) {
	return "", "", ErrUnsupportedPlatform
}

// TeardownMgmt reports the platform boundary without changing host state.
func TeardownMgmt(nodeID int) error { return ErrUnsupportedPlatform }
