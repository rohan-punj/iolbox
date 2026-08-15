package main

import (
	"bytes"
	"context"
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
