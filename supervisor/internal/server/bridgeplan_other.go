//go:build !linux

package server

// currentUID returns a fixed, deterministic uid off Linux so bridge-plan unit
// tests produce stable netio socket paths (/tmp/netio1000/<instance>) without
// depending on the host OS. The real value (os.Getuid) is used only on Linux,
// where the netio dir must match IOL's own /tmp/netio<uid> (bridgeplan_linux.go).
func currentUID() int { return 1000 }
