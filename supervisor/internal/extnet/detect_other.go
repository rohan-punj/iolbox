//go:build !linux

package extnet

import "errors"

// DefaultRouteIface is unavailable off Linux (no /proc/net/route); the data
// plane only runs inside the Linux runtime.
func DefaultRouteIface() (string, error) {
	return "", errors.New("extnet: default-route detection is only supported on linux")
}

// PickMgmtIface is unavailable off Linux.
func PickMgmtIface(pref string) (string, error) {
	if pref != "" {
		return pref, nil
	}
	return "", errors.New("extnet: management-interface detection is only supported on linux")
}

// Detect reports no support off Linux: nat/mgmt need a Linux tap/macvtap data
// plane, so the hello handshake never advertises them on the dev box.
func Detect(sudoOK bool, mgmtPref string) Capabilities {
	return Capabilities{}
}

// SudoOK is always false off Linux (no privileged data plane to gate).
func SudoOK() bool { return false }
