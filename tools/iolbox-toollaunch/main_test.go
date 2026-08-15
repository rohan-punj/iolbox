package main

import (
	"os"
	"reflect"
	"runtime"
	"testing"
)

func TestParseCapsListEmpty(t *testing.T) {
	got, err := parseCapsList("")
	if err != nil {
		t.Fatalf("parseCapsList(\"\") error: %v", err)
	}
	if got != nil {
		t.Fatalf("parseCapsList(\"\") = %#v, want nil", got)
	}
}

func TestParseCapsListSingle(t *testing.T) {
	got, err := parseCapsList("cap_net_raw")
	if err != nil {
		t.Fatalf("parseCapsList error: %v", err)
	}
	want := []string{"cap_net_raw"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseCapsList = %#v, want %#v", got, want)
	}
}

func TestParseCapsListMulti(t *testing.T) {
	got, err := parseCapsList("cap_net_raw,cap_net_admin")
	if err != nil {
		t.Fatalf("parseCapsList error: %v", err)
	}
	want := []string{"cap_net_raw", "cap_net_admin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseCapsList = %#v, want %#v", got, want)
	}
}

func TestParseCapsListUnknown(t *testing.T) {
	if _, err := parseCapsList("cap_sys_admin"); err == nil {
		t.Fatal("parseCapsList(\"cap_sys_admin\") should have been rejected — not in knownCapNumbers")
	}
}

func TestParseLaunchArgsMultiCap(t *testing.T) {
	opts, err := parseLaunchArgs([]string{
		"--user", "ioltool", "--caps", "cap_net_raw,cap_net_admin", "--", "/opt/pack/pc-gui", "--serve",
	})
	if err != nil {
		t.Fatalf("parseLaunchArgs error: %v", err)
	}
	want := []string{"cap_net_raw", "cap_net_admin"}
	if !reflect.DeepEqual(opts.caps, want) {
		t.Fatalf("opts.caps = %#v, want %#v", opts.caps, want)
	}
	if opts.target != "/opt/pack/pc-gui" {
		t.Fatalf("opts.target = %q", opts.target)
	}
}

func TestParseLaunchArgsEmptyCaps(t *testing.T) {
	opts, err := parseLaunchArgs([]string{
		"--user", "ioltool", "--caps", "", "--", "/opt/pack/webserver-gui",
	})
	if err != nil {
		t.Fatalf("parseLaunchArgs error: %v", err)
	}
	if opts.caps != nil {
		t.Fatalf("opts.caps = %#v, want nil (zero capabilities)", opts.caps)
	}
}

func TestParseLaunchArgsRequiresCapsFlag(t *testing.T) {
	if _, err := parseLaunchArgs([]string{"--user", "ioltool", "--", "/opt/pack/tool"}); err == nil {
		t.Fatal("parseLaunchArgs should require --caps even when the value can be empty")
	}
}

func TestParseLaunchArgsRejectsUnknownCap(t *testing.T) {
	if _, err := parseLaunchArgs([]string{
		"--user", "ioltool", "--caps", "cap_sys_admin", "--", "/opt/pack/tool",
	}); err == nil {
		t.Fatal("parseLaunchArgs should reject a capability outside knownCapNumbers")
	}
}

func TestParseLaunchArgsCgroupFD(t *testing.T) {
	opts, err := parseLaunchArgs([]string{
		"--cgroup-fd", "3", "--user", "ioltool", "--caps", "cap_net_raw", "--", "/opt/pack/pc-gui",
	})
	if err != nil {
		t.Fatalf("parseLaunchArgs error: %v", err)
	}
	if opts.cgroupFD != 3 {
		t.Fatalf("opts.cgroupFD = %d, want 3", opts.cgroupFD)
	}
	if opts.cgroup != "" {
		t.Fatalf("opts.cgroup = %q, want empty when --cgroup-fd is used", opts.cgroup)
	}
}

func TestParseLaunchArgsCgroupFDDefaultsUnset(t *testing.T) {
	opts, err := parseLaunchArgs([]string{
		"--user", "ioltool", "--caps", "cap_net_raw", "--", "/opt/pack/pc-gui",
	})
	if err != nil {
		t.Fatalf("parseLaunchArgs error: %v", err)
	}
	if opts.cgroupFD != -1 {
		t.Fatalf("opts.cgroupFD = %d, want -1 (unset) when neither --cgroup nor --cgroup-fd is given", opts.cgroupFD)
	}
}

func TestParseLaunchArgsCgroupAndCgroupFDMutuallyExclusive(t *testing.T) {
	if _, err := parseLaunchArgs([]string{
		"--cgroup", "/sys/fs/cgroup/tool-7", "--cgroup-fd", "3",
		"--user", "ioltool", "--caps", "cap_net_raw", "--", "/opt/pack/pc-gui",
	}); err == nil {
		t.Fatal("parseLaunchArgs should reject combining --cgroup and --cgroup-fd")
	}
	if _, err := parseLaunchArgs([]string{
		"--cgroup-fd", "3", "--cgroup", "/sys/fs/cgroup/tool-7",
		"--user", "ioltool", "--caps", "cap_net_raw", "--", "/opt/pack/pc-gui",
	}); err == nil {
		t.Fatal("parseLaunchArgs should reject combining --cgroup-fd and --cgroup")
	}
}

func TestParseLaunchArgsCgroupFDInvalidNumber(t *testing.T) {
	if _, err := parseLaunchArgs([]string{
		"--cgroup-fd", "not-a-number", "--user", "ioltool", "--caps", "cap_net_raw", "--", "/opt/pack/pc-gui",
	}); err == nil {
		t.Fatal("parseLaunchArgs should reject a non-numeric --cgroup-fd")
	}
}

func TestWriteCgroupMembershipPrefersFDOverPath(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc/self/fd is Linux-only; this binary only ever runs on the Linux guest")
	}
	// A bogus path that would fail if ever consulted, paired with a real,
	// writable target via fd — proves the fd branch takes precedence and the
	// path is never touched, matching production: Launch's fallback branch
	// always prefers the fd when one is available.
	dir := t.TempDir()
	f, err := os.Open(dir)
	if err != nil {
		t.Fatalf("open %s: %v", dir, err)
	}
	defer f.Close()
	if err := os.WriteFile(dir+"/cgroup.procs", nil, 0o644); err != nil {
		t.Fatalf("seed cgroup.procs: %v", err)
	}
	if err := writeCgroupMembership("/does/not/exist", int(f.Fd())); err != nil {
		t.Fatalf("writeCgroupMembership with fd = %v, want nil (path must not be consulted)", err)
	}
}
