package tool

import (
	"reflect"
	"testing"
)

func TestLaunchSetprivArgvOrder(t *testing.T) {
	spec := LaunchSpec{Binary: "/opt/pack/tool", Args: []string{"--listen", "eth1"}}
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

func TestLaunchArgvNamespaceTransitionTargetOrder(t *testing.T) {
	spec := LaunchSpec{NodeID: 7, Binary: "/opt/pack/tool", Args: []string{"--serve"}}
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
		CgroupPath: "/sys/fs/cgroup/tool-7",
		Binary:     "/opt/pack/tool",
		Args:       []string{"--serve"},
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
