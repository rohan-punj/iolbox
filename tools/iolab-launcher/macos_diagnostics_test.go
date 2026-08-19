package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type diagnosticLimaMock struct{}

func (diagnosticLimaMock) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	command := strings.Join(args[2:], " ")
	switch {
	case command == "uname -m":
		return []byte("aarch64\n"), nil
	case command == "uname -r":
		return []byte("6.8.0-m5\n"), nil
	case strings.Contains(command, "cat /proc/sys/fs/binfmt_misc/rosetta"):
		return []byte("enabled\ninterpreter /mnt/lima-rosetta/rosetta\n"), nil
	case strings.Contains(command, "cat /var/lib/iolbox/macos-canary.json"):
		return []byte(`{"schema":1,"macos_product":"26.6.1","macos_build":"25G42","lima_version":"2.2.0","profile":"sequoia","kernel":"6.8.0-m5","binfmt":"enabled","verdict":"PASS","timestamp":"2026-08-14T12:00:00Z","version":"ld.so (Debian GLIBC 2.41-12) stable release version 2.41.","error":""}`), nil
	case strings.Contains(command, "cat /etc/systemd/system/iolbox-supervisor.service.d/10-iolbox-macos-canary.conf"):
		return []byte("Environment=IOLBOX_DISABLE_I386=1\n"), nil
	case strings.Contains(command, "systemctl is-active"):
		return []byte("inactive\n"), nil
	case strings.Contains(command, "curl"):
		return []byte("503\n"), nil
	case strings.Contains(command, "30-canary.sh"):
		return []byte("PASS\n"), nil
	default:
		return nil, nil
	}
}

// TestRosettaPresenceStringReportsFalseForANoSuchFileGuest is a regression
// test for a real bug found on physical hardware (2026-08-19): on a healthy
// native-arm64 guest, /proc/sys/fs/binfmt_misc/rosetta genuinely does not
// exist, so probeGuest's `cat` fails and unavailableValue() wraps that as
// "unavailable (cat: ... No such file or directory)". The presence-string
// classifier used to check the "unavailable" prefix before the "no such
// file" substring, so this perfectly truthful "Rosetta is absent" case was
// misreported as "unknown" instead of "false".
func TestRosettaPresenceStringReportsFalseForANoSuchFileGuest(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"no such file wrapped as unavailable", "unavailable (cat: /proc/sys/fs/binfmt_misc/rosetta: No such file or directory)", "false"},
		{"enabled", "enabled\ninterpreter /mnt/lima-rosetta/rosetta\n", "true"},
		{"disabled", "disabled\n", "true (registered but disabled)"},
		{"empty", "", "false"},
		{"genuinely unknown failure", "unavailable (permission denied)", "unknown (unavailable (permission denied))"},
	}
	for _, tc := range cases {
		if got := rosettaPresenceString(tc.raw); got != tc.want {
			t.Errorf("%s: rosettaPresenceString(%q) = %q, want %q", tc.name, tc.raw, got, tc.want)
		}
	}
}

func TestDiagnosticsUsesMeasuredExecutionAndSeparatesService(t *testing.T) {
	lima := &limaClient{info: limaInfo{Path: "limactl", Version: "2.2.0"}, runner: diagnosticLimaMock{}}
	hello := func() (helloResult, error) {
		return helloResult{Supervisor: "m5", Runtime: "debian", Arch: "x86_64", Features: []string{"nvram", "capture"}}, nil
	}
	d := collectDarwinDiagnostics(context.Background(), lima, "iolbox-m5-e2e", "Running", macOSProfile{Name: "sequoia"}, hostFacts{}, lima.info, diagnosticsOptions{GUIPort: 4001, hello: hello})
	var captured bytes.Buffer
	printDiagnosticSummary(&captured, d)
	out := captured.String()
	for _, want := range []string{
		"guest_arch=aarch64",
		"execution=rosetta-amd64",
		"guest_kernel=6.8.0-m5",
		"service=FAIL (inactive)",
		"http=FAIL (HTTP 503)",
		"capability_policy=PASS",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("captured diagnostics missing %q:\n%s", want, out)
		}
	}
}

type diagnosticNativeLimaMock struct{}

func (diagnosticNativeLimaMock) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	command := strings.Join(args[2:], " ")
	switch {
	case command == "uname -m":
		return []byte("aarch64\n"), nil
	case command == "uname -r":
		return []byte("6.12.101+deb13-cloud-arm64\n"), nil
	case strings.Contains(command, "cat /proc/sys/fs/binfmt_misc/rosetta"):
		// No Rosetta entry on a genuinely native guest; probeGuest's `cat`
		// fails, matching the real hardware shape unavailableValue() wraps.
		return []byte("cat: /proc/sys/fs/binfmt_misc/rosetta: No such file or directory\n"), errors.New("exit status 1")
	case strings.Contains(command, "cat /var/lib/iolbox/macos-canary.json"):
		return []byte(`{"schema":1,"macos_product":"26.6.2","macos_build":"25G83","lima_version":"2.2.0","profile":"native-arm64","kernel":"6.12.101+deb13-cloud-arm64","binfmt":"absent (expected)","verdict":"PASS","timestamp":"2026-08-19T18:32:07Z","version":"ld.so (Debian GLIBC 2.41-12) stable release version 2.41.","error":""}`), nil
	case strings.Contains(command, "cat /etc/systemd/system/iolbox-supervisor.service.d/10-iolbox-macos-canary.conf"):
		return []byte("Environment=IOLBOX_DISABLE_I386=1\n"), nil
	case strings.Contains(command, "systemctl is-active"):
		return []byte("active\n"), nil
	case strings.Contains(command, "curl"):
		return []byte("200\n"), nil
	case strings.Contains(command, "30-canary-native.sh"):
		return []byte("PASS\n"), nil
	default:
		return nil, nil
	}
}

// TestDiagnosticsRecognizesNativeArm64Execution is a companion to
// TestDiagnosticsUsesMeasuredExecutionAndSeparatesService for the
// native-arm64 path this session added: execution must read "native-arm64"
// (not "not qualified" or the Rosetta value) when the guest is aarch64,
// Rosetta is absent, and the profile's own native canary passes; and
// rosetta_present must read "false", not "unknown" (see
// TestRosettaPresenceStringReportsFalseForANoSuchFileGuest for the unit-level
// cause this was regression-testing).
func TestDiagnosticsRecognizesNativeArm64Execution(t *testing.T) {
	lima := &limaClient{info: limaInfo{Path: "limactl", Version: "2.2.0"}, runner: diagnosticNativeLimaMock{}}
	hello := func() (helloResult, error) {
		return helloResult{Supervisor: "0.1.0", Runtime: "debian-slim-12", Arch: "arm64", Features: []string{"nvram", "capture", "natgw", "tools"}}, nil
	}
	profile := macOSProfile{Name: nativeProfileTableName, CanaryStep: "30-canary-native.sh"}
	d := collectDarwinDiagnostics(context.Background(), lima, "iolbox-native-arm64", "Running", profile, hostFacts{}, lima.info, diagnosticsOptions{GUIPort: 4001, hello: hello})
	var captured bytes.Buffer
	printDiagnosticSummary(&captured, d)
	out := captured.String()
	for _, want := range []string{
		"guest_arch=aarch64",
		"supervisor_arch=arm64",
		"execution=native-arm64",
		"backend=lima-vz",
		"translator=qemu-user",
		"rosetta_present=false",
		"service=PASS (active)",
		"http=PASS (HTTP 200)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("captured native diagnostics missing %q:\n%s", want, out)
		}
	}
}
