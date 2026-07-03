package main

import (
	"strings"
	"testing"
)

func TestParseHypervisorPresent(t *testing.T) {
	cases := []struct {
		in      string
		wantVal bool
		wantOK  bool
	}{
		{"True\r\n", true, true},
		{"False\r\n", false, true},
		{"  true  ", true, true},
		{"T\x00r\x00u\x00e\x00", true, true}, // UTF-16LE-ish with NULs
		{"", false, false},
		{"garbage", false, false},
	}
	for _, c := range cases {
		val, ok := parseHypervisorPresent(c.in)
		if val != c.wantVal || ok != c.wantOK {
			t.Errorf("parseHypervisorPresent(%q) = (%v,%v), want (%v,%v)", c.in, val, ok, c.wantVal, c.wantOK)
		}
	}
}

func TestWslUsable(t *testing.T) {
	// All present -> usable.
	d := detection{wslExePresent: true, wslVersionOK: true, hypervisorKnown: true, hypervisorPresent: true}
	if !d.wslUsable() {
		t.Error("all-present should be usable")
	}
	// The VMware-box case: wsl.exe + version OK, but NO hypervisor -> not usable.
	d = detection{wslExePresent: true, wslVersionOK: true, hypervisorKnown: true, hypervisorPresent: false}
	if d.wslUsable() {
		t.Error("no-hypervisor must be unusable even with wsl.exe + version")
	}
	// hypervisor unknown -> not usable (fail safe).
	d = detection{wslExePresent: true, wslVersionOK: true, hypervisorKnown: false}
	if d.wslUsable() {
		t.Error("unknown hypervisor must be treated as unusable")
	}
	// no wsl.exe -> not usable.
	d = detection{wslExePresent: false, hypervisorKnown: true, hypervisorPresent: true}
	if d.wslUsable() {
		t.Error("missing wsl.exe must be unusable")
	}
}

func TestChooseBackend(t *testing.T) {
	usable := detection{wslExePresent: true, wslVersionOK: true, hypervisorKnown: true, hypervisorPresent: true}
	unusable := detection{wslExePresent: true, wslVersionOK: true, hypervisorKnown: true, hypervisorPresent: false}

	// auto + usable -> wsl
	d := usable
	if got := chooseBackend(backendAuto, &d); got != backendWSL {
		t.Errorf("auto+usable want wsl, got %s", got)
	}
	// auto + unusable -> qemu, with a hypervisor explanation
	d = unusable
	if got := chooseBackend(backendAuto, &d); got != backendQEMU {
		t.Errorf("auto+unusable want qemu, got %s", got)
	}
	if !strings.Contains(d.reason, "QEMU") && !strings.Contains(d.reason, "qemu") {
		t.Errorf("auto+unusable reason should mention qemu fallback: %q", d.reason)
	}
	// forced qemu -> qemu
	d = usable
	if got := chooseBackend(backendQEMU, &d); got != backendQEMU {
		t.Errorf("forced qemu want qemu, got %s", got)
	}
	// forced wsl on unusable -> still returns wsl (main.go rejects), reason explains
	d = unusable
	if got := chooseBackend(backendWSL, &d); got != backendWSL {
		t.Errorf("forced wsl want wsl, got %s", got)
	}
	if !strings.Contains(d.reason, "NOT usable") {
		t.Errorf("forced-wsl-on-unusable reason should say NOT usable: %q", d.reason)
	}
}

func TestWslUnusableReason(t *testing.T) {
	// Ordering: no wsl.exe first.
	if got := wslUnusableReason(detection{}); !strings.Contains(got, "wsl.exe") {
		t.Errorf("no-wsl reason: %q", got)
	}
	// version fail
	if got := wslUnusableReason(detection{wslExePresent: true}); !strings.Contains(got, "wsl --version") {
		t.Errorf("version reason: %q", got)
	}
	// hypervisor absent -> mentions VMware degradation warning
	d := detection{wslExePresent: true, wslVersionOK: true, hypervisorKnown: true, hypervisorPresent: false}
	got := wslUnusableReason(d)
	if !strings.Contains(got, "HypervisorPresent") || !strings.Contains(got, "VMware") {
		t.Errorf("hypervisor-absent reason should cite HypervisorPresent + VMware: %q", got)
	}
}
