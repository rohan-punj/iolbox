//go:build linux

package tool

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestLaunchSetprivProbeArgvMatchesProductionTransition(t *testing.T) {
	argv := launchSetprivProbeArgv()
	want := launchSetprivArgv(LaunchSpec{Binary: "/bin/sh", Args: []string{"-c", detectProbeCapabilityScript}})
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("probe argv = %#v, want %#v", argv, want)
	}
	// The probe must transition exactly like a real launch, otherwise a host
	// whose setpriv rejects the pinned flags would still be selected.
	for _, flag := range []string{"--reuid", "--no-new-privs", "--ambient-caps"} {
		if !launchContainsInOrder(argv, "setpriv", flag) {
			t.Fatalf("probe argv is missing %q: %#v", flag, argv)
		}
	}
	if argv[len(argv)-2] != "-c" || argv[len(argv)-1] != detectProbeCapabilityScript {
		t.Fatalf("probe target is not the shared capability assertion: %#v", argv[len(argv)-3:])
	}
}

func TestLaunchVerifySetprivTransitionIsEmpirical(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the pinned setpriv transition requires root")
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh is unavailable")
	}

	setprivErr := launchVerifySetpriv()
	nativeErr := launchVerifyNative()
	mode, err := launchSelectMode(setprivErr, nativeErr)
	if err != nil {
		t.Fatalf("no usable launcher on this host: %v", err)
	}
	if mode == "setpriv" && setprivErr != nil {
		t.Fatalf("setpriv selected despite verification failure: %v", setprivErr)
	}
	if mode == "native" && nativeErr != nil {
		t.Fatalf("native selected despite %v", nativeErr)
	}
	// Appliance evidence: util-linux 2.38.1 passes the version floor and still
	// fails the pinned transition with `unknown capability "cap_net_raw"`.
	// Selecting setpriv there is exactly the bug this verification prevents.
	if setprivErr != nil && !strings.Contains(setprivErr.Error(), "tool: ") {
		t.Fatalf("setpriv failure is not attributed to this package: %v", setprivErr)
	}
}
