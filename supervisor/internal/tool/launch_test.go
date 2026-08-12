package tool

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestLaunchSetprivArgvOrder(t *testing.T) {
	spec := LaunchSpec{Binary: "/opt/pack/tool", Args: []string{"--listen", "eth1"}, AmbientCaps: []string{"NET_RAW"}}
	want := []string{
		"setpriv",
		"--reuid", "ioltool",
		"--regid", "ioltool",
		"--clear-groups",
		"--no-new-privs",
		"--bounding-set", "-all,+cap_net_raw",
		"--inh-caps", "-all,+cap_net_raw",
		"--ambient-caps", "-all,+cap_net_raw",
		"--", "/opt/pack/tool", "--listen", "eth1",
	}
	if got := launchSetprivArgv(spec); !reflect.DeepEqual(got, want) {
		t.Fatalf("setpriv argv = %#v, want %#v", got, want)
	}
}

// TestLaunchSetprivArgvMultiCap covers the netprobe pack (NET_RAW + NET_ADMIN
// — see tool.go's AllowedCaps comment) and confirms caps compose in
// declaration order rather than being clamped to a single hardcoded value.
func TestLaunchSetprivArgvMultiCap(t *testing.T) {
	spec := LaunchSpec{Binary: "/opt/pack/pc-gui", AmbientCaps: []string{"NET_RAW", "NET_ADMIN"}}
	got := launchSetprivArgv(spec)
	want := "-all,+cap_net_raw,+cap_net_admin"
	for _, flag := range []string{"--bounding-set", "--inh-caps", "--ambient-caps"} {
		if !launchContainsInOrder(got, flag, want) {
			t.Fatalf("argv %#v missing %q %q", got, flag, want)
		}
	}
}

// TestLaunchSetprivArgvNoCaps covers the common case (most packs declare
// caps:[]) — must drop everything, not silently fall back to any capability.
func TestLaunchSetprivArgvNoCaps(t *testing.T) {
	spec := LaunchSpec{Binary: "/opt/pack/tool"}
	got := launchSetprivArgv(spec)
	for _, flag := range []string{"--bounding-set", "--inh-caps", "--ambient-caps"} {
		if !launchContainsInOrder(got, flag, "-all") {
			t.Fatalf("argv %#v missing %q %q", got, flag, "-all")
		}
	}
}

func TestLaunchArgvNamespaceTransitionTargetOrder(t *testing.T) {
	spec := LaunchSpec{NodeID: 7, Binary: "/opt/pack/tool", Args: []string{"--serve"}, AmbientCaps: []string{"NET_RAW"}}
	got := NetnsExecArgs(spec.NodeID, launchSetprivArgv(spec))
	want := []string{
		"ip", "netns", "exec", "iolt7",
		"setpriv", "--reuid", "ioltool", "--regid", "ioltool",
		"--clear-groups", "--no-new-privs", "--bounding-set", "-all,+cap_net_raw",
		"--inh-caps", "-all,+cap_net_raw", "--ambient-caps", "-all,+cap_net_raw",
		"--", "/opt/pack/tool", "--serve",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("namespace/transition argv = %#v, want %#v", got, want)
	}
	for _, value := range []string{"iolt7", "setpriv", "/opt/pack/tool"} {
		if len(got) == 0 || !launchContainsInOrder(got, "ip", value) {
			t.Fatalf("argv does not put %q after namespace prefix: %#v", value, got)
		}
	}
}

func TestLaunchNativeArgvWithCgroup(t *testing.T) {
	spec := LaunchSpec{
		CgroupPath:  "/sys/fs/cgroup/tool-7",
		Binary:      "/opt/pack/tool",
		Args:        []string{"--serve"},
		AmbientCaps: []string{"NET_RAW"},
	}
	want := []string{
		"/opt/iolbox/iolbox-toollaunch",
		"--cgroup", "/sys/fs/cgroup/tool-7",
		"--user", "ioltool", "--caps", "cap_net_raw", "--",
		"/opt/pack/tool", "--serve",
	}
	if got := launchNativeArgv(spec, true); !reflect.DeepEqual(got, want) {
		t.Fatalf("native argv = %#v, want %#v", got, want)
	}
}

// TestLaunchNativeArgvMultiCap covers the native-launcher path with the same
// two-capability pack as TestLaunchSetprivArgvMultiCap.
func TestLaunchNativeArgvMultiCap(t *testing.T) {
	spec := LaunchSpec{Binary: "/opt/pack/pc-gui", AmbientCaps: []string{"NET_RAW", "NET_ADMIN"}}
	got := launchNativeArgv(spec, false)
	if !launchContainsInOrder(got, "--caps", "cap_net_raw,cap_net_admin") {
		t.Fatalf("native argv %#v missing --caps cap_net_raw,cap_net_admin", got)
	}
}

func TestScrubEnvAllowlistOnly(t *testing.T) {
	extra := map[string]string{
		"PATH":                "/usr/bin",
		"HOME":                "/home/ioltool",
		"LANG":                "C.UTF-8",
		"PYTHONHOME":          "/opt/python",
		"PYTHONPATH":          "/opt/pack",
		"IOLBOX_TOOL_SOCK":    "/run/iolbox/tool/7/gui.sock",
		"IOLBOX_TOOL_OPTIONS": "/run/iolbox/tool/7/options.json",
		"IOLBOX_PACK_DIR":     "/opt/iolbox/tools/packs/stub",
		"IOLBOX_NODE_ID":      "7",
		"SECRET":              "1",
		"IOLBOX_TOOL_OPTS":    "wrong-name",
	}
	want := []string{
		"PATH=/usr/bin",
		"HOME=/home/ioltool",
		"LANG=C.UTF-8",
		"PYTHONHOME=/opt/python",
		"PYTHONPATH=/opt/pack",
		"IOLBOX_TOOL_SOCK=/run/iolbox/tool/7/gui.sock",
		"IOLBOX_TOOL_OPTIONS=/run/iolbox/tool/7/options.json",
		"IOLBOX_PACK_DIR=/opt/iolbox/tools/packs/stub",
		"IOLBOX_NODE_ID=7",
	}
	if got := ScrubEnv(extra); !reflect.DeepEqual(got, want) {
		t.Fatalf("scrubbed env = %#v, want %#v", got, want)
	}
}

func TestLaunchSelectModeRequiresVerifiedSetpriv(t *testing.T) {
	probeFailure := errors.New(`setpriv: unknown capability "cap_net_raw"`)
	nativeFailure := errors.New("no such file")

	if mode, err := launchSelectMode(nil, nativeFailure); mode != "setpriv" || err != nil {
		t.Fatalf("verified setpriv = (%q, %v), want (\"setpriv\", nil)", mode, err)
	}
	if mode, err := launchSelectMode(probeFailure, nil); mode != "native" || err != nil {
		t.Fatalf("unverified setpriv with native = (%q, %v), want (\"native\", nil)", mode, err)
	}
	mode, err := launchSelectMode(probeFailure, nativeFailure)
	if mode != "" || err == nil {
		t.Fatalf("no usable launcher = (%q, %v), want (\"\", error)", mode, err)
	}
	// The failure must name both attempts so a deployment can tell an old or
	// broken setpriv apart from a missing helper binary.
	if !strings.Contains(err.Error(), probeFailure.Error()) || !strings.Contains(err.Error(), nativeFailure.Error()) {
		t.Fatalf("error %q does not report both launcher failures", err)
	}
}

func TestLaunchSelectorProbesOnlyOnce(t *testing.T) {
	calls := 0
	selector := &launchSelector{probe: func() (string, error) {
		calls++
		return "setpriv", nil
	}}
	for attempt := 0; attempt < 3; attempt++ {
		mode, err := selector.selectMode()
		if mode != "setpriv" || err != nil {
			t.Fatalf("attempt %d = (%q, %v), want (\"setpriv\", nil)", attempt, mode, err)
		}
	}
	if calls != 1 {
		t.Fatalf("probe ran %d times, want 1", calls)
	}
}

func TestLaunchSelectorCachesFailureWithoutReprobing(t *testing.T) {
	calls := 0
	want := errors.New("no usable cap-transition launcher")
	selector := &launchSelector{probe: func() (string, error) {
		calls++
		return "", want
	}}
	for attempt := 0; attempt < 3; attempt++ {
		mode, err := selector.selectMode()
		if mode != "" || !errors.Is(err, want) {
			t.Fatalf("attempt %d = (%q, %v), want (\"\", %v)", attempt, mode, err, want)
		}
	}
	if calls != 1 {
		t.Fatalf("probe ran %d times, want 1", calls)
	}
}

func TestLaunchSelectorWithoutProbeFailsClosed(t *testing.T) {
	selector := &launchSelector{}
	if mode, err := selector.selectMode(); mode != "" || err == nil {
		t.Fatalf("unconfigured selector = (%q, %v), want (\"\", error)", mode, err)
	}
}

func launchContainsInOrder(values []string, first, second string) bool {
	firstAt := -1
	secondAt := -1
	for index, value := range values {
		if value == first && firstAt < 0 {
			firstAt = index
		}
		if value == second && firstAt >= 0 && secondAt < 0 {
			secondAt = index
		}
	}
	return firstAt >= 0 && secondAt > firstAt
}
