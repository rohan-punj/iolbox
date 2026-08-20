//go:build linux

package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	netnsFailureHelperModeEnv     = "IOLBOX_NETNS_FAILURE_HELPER"
	netnsFailureHelperLogEnv      = "IOLBOX_NETNS_FAILURE_LOG"
	netnsFailureHelperStateEnv    = "IOLBOX_NETNS_FAILURE_STATE"
	netnsFailureHelperSelfExecArg = "-test.run=^TestNetnsCreateSysctlFailureTearsDownNetns$"
)

// TestNetnsCreateSysctlFailureTearsDownNetns exercises the existing
// endpointStartFailure cleanup at the new second command in CreateNetns. The
// helper emulates ip so the test verifies command ordering and cleanup without
// requiring a privileged test runner or modifying a real network namespace.
func TestNetnsCreateSysctlFailureTearsDownNetns(t *testing.T) {
	if os.Getenv(netnsFailureHelperModeEnv) == "1" {
		netnsFailureHelper(t)
		return
	}

	testBinary, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	helpDir := t.TempDir()
	logPath := filepath.Join(helpDir, "calls.log")
	statePath := filepath.Join(helpDir, "netns-created")
	helpPath := filepath.Join(helpDir, "ip")
	script := fmt.Sprintf("#!/bin/sh\nexec %s %s \"$@\"\n", shellQuote(testBinary), shellQuote(netnsFailureHelperSelfExecArg))
	if err := os.WriteFile(helpPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", helpDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(netnsFailureHelperModeEnv, "1")
	t.Setenv(netnsFailureHelperLogEnv, logPath)
	t.Setenv(netnsFailureHelperStateEnv, statePath)

	const nodeID = 17
	e := &Endpoint{
		endpointCfg:          Config{NodeID: nodeID},
		endpointLivenessStop: make(chan struct{}),
	}
	createErr := CreateNetns(nodeID)
	if createErr == nil {
		t.Fatal("CreateNetns unexpectedly succeeded after the sysctl write failed")
	}
	if !strings.Contains(createErr.Error(), "sysctl") {
		t.Fatalf("CreateNetns error = %v, want the failed sysctl command named", createErr)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("fake netns was not created by the first command: %v", err)
	}

	if err := e.endpointStartFailure(createErr); err == nil {
		t.Fatal("endpointStartFailure unexpectedly returned nil")
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("fake netns state after endpointStartFailure = %v, want removed", err)
	}

	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	callText := string(calls)
	wantCalls := []string{
		"netns add iolt17",
		"netns exec iolt17 sysctl -w net.ipv4.ip_unprivileged_port_start=1",
		"link set vtool17 nomaster",
		"link del vtool17",
		"netns del iolt17",
	}
	gotCalls := strings.Split(strings.TrimSpace(callText), "\n")
	if len(gotCalls) != len(wantCalls) {
		t.Fatalf("fake ip calls = %#v, want %#v", gotCalls, wantCalls)
	}
	for index, want := range wantCalls {
		if gotCalls[index] != want {
			t.Fatalf("fake ip call %d = %q, want %q; all calls = %#v", index, gotCalls[index], want, gotCalls)
		}
	}
}

func netnsFailureHelper(t *testing.T) {
	args := os.Args[1:]
	filtered := make([]string, 0, len(args))
	removedSelfExecArg := false
	for _, arg := range args {
		if arg == netnsFailureHelperSelfExecArg && !removedSelfExecArg {
			removedSelfExecArg = true
			continue
		}
		filtered = append(filtered, arg)
	}
	if !removedSelfExecArg {
		t.Fatal("fake ip helper did not receive its known self-exec test argument")
	}
	args = filtered
	if len(args) == 0 {
		t.Fatal("fake ip helper received no ip arguments")
	}

	logFile, err := os.OpenFile(os.Getenv(netnsFailureHelperLogEnv), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprintln(logFile, strings.Join(args, " "))
	_ = logFile.Close()

	statePath := os.Getenv(netnsFailureHelperStateEnv)
	switch {
	case len(args) >= 3 && args[0] == "netns" && args[1] == "add":
		if err := os.WriteFile(statePath, []byte(args[2]), 0o600); err != nil {
			t.Fatal(err)
		}
	case len(args) >= 4 && args[0] == "netns" && args[1] == "exec" && args[3] == "sysctl":
		fmt.Fprintln(os.Stderr, "simulated sysctl write failure")
		os.Exit(1)
	case len(args) >= 3 && args[0] == "netns" && args[1] == "del":
		_ = os.Remove(statePath)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
