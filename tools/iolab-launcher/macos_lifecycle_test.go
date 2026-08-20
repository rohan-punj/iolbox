package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type sequenceRunner struct {
	calls       [][]string
	outputs     map[string][]byte
	errors      map[string]error
	listOutputs [][]byte
}

func (r *sequenceRunner) Run(_ context.Context, path string, args ...string) ([]byte, error) {
	call := append([]string{path}, args...)
	r.calls = append(r.calls, call)
	if len(args) > 0 && args[0] == "list" && len(r.listOutputs) > 0 {
		out := r.listOutputs[0]
		r.listOutputs = r.listOutputs[1:]
		return out, nil
	}
	key := ""
	if len(args) > 0 {
		key = args[0]
	}
	if err := r.errors[key]; err != nil {
		return r.outputs[key], err
	}
	return r.outputs[key], nil
}

func commandNames(calls [][]string) []string {
	var names []string
	for _, call := range calls {
		if len(call) > 1 {
			names = append(names, strings.Join(call[1:], " "))
		}
	}
	return names
}

func TestEnsureMachineLifecycleOrdering(t *testing.T) {
	t.Run("absent creates then starts and removes only exact attestation", func(t *testing.T) {
		runner := &sequenceRunner{}
		client := &limaClient{info: limaInfo{Path: "limactl", Version: "2.2.0"}, runner: runner}
		attestation := t.TempDir() + "/machine-structural-gate.json"
		if err := writeStringFile(attestation, "stale"); err != nil {
			t.Fatal(err)
		}
		created, err := ensureMachine(t.Context(), client, "m", "", "template.yaml", attestation, nil)
		if err != nil || !created {
			t.Fatalf("ensure absent = %v, created=%v", err, created)
		}
		got := commandNames(runner.calls)
		if len(got) != 2 || !strings.HasPrefix(got[0], "create --name=m") || got[1] != "start m --tty=false" {
			t.Fatalf("commands = %v", got)
		}
		if _, err := readFileForTest(attestation); !errors.Is(err, errMissingTestFile) {
			t.Fatalf("stale attestation still exists or wrong error: %v", err)
		}
	})
	t.Run("running reuses without commands", func(t *testing.T) {
		runner := &sequenceRunner{}
		client := &limaClient{info: limaInfo{Path: "limactl"}, runner: runner}
		if _, err := ensureMachine(t.Context(), client, "m", "Running", "", "", func() error { return errors.New("must not run") }); err != nil {
			t.Fatal(err)
		}
		if len(runner.calls) != 0 {
			t.Fatalf("running machine commands = %v", runner.calls)
		}
	})
	t.Run("stopped with valid attestation starts", func(t *testing.T) {
		runner := &sequenceRunner{}
		client := &limaClient{info: limaInfo{Path: "limactl"}, runner: runner}
		if _, err := ensureMachine(t.Context(), client, "m", "Stopped", "", "", func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		if got := commandNames(runner.calls); len(got) != 1 || got[0] != "start m --tty=false" {
			t.Fatalf("commands = %v", got)
		}
	})
	t.Run("stopped with invalid attestation refuses", func(t *testing.T) {
		runner := &sequenceRunner{}
		client := &limaClient{info: limaInfo{Path: "limactl"}, runner: runner}
		err := func() error { return errors.New("bad attestation") }
		if _, got := ensureMachine(t.Context(), client, "m", "Stopped", "", "", err); exitCode(got) != exitPreflight {
			t.Fatalf("error = %v, code=%d", got, exitCode(got))
		}
		if len(runner.calls) != 0 {
			t.Fatalf("invalid-attestation commands = %v", runner.calls)
		}
	})
	t.Run("unknown state refuses", func(t *testing.T) {
		runner := &sequenceRunner{}
		client := &limaClient{info: limaInfo{Path: "limactl"}, runner: runner}
		if _, err := ensureMachine(t.Context(), client, "m", "Starting", "", "", nil); exitCode(err) != exitPreflight {
			t.Fatalf("unknown-state error = %v, code=%d", err, exitCode(err))
		}
		if len(runner.calls) != 0 {
			t.Fatalf("unknown-state commands = %v", runner.calls)
		}
	})
}

func TestEnsureMachineWithPortsReusesCompliantRunningInstance(t *testing.T) {
	runner := &sequenceRunner{}
	ports, err := newDarwinPortContract(defaultDarwinGUIPort)
	if err != nil {
		t.Fatal(err)
	}
	client := &limaClient{
		info:   limaInfo{Path: "limactl"},
		runner: runner,
		instanceConfig: func(string) ([]byte, error) {
			return []byte(ports.yamlPortForwards()), nil
		},
	}
	if _, err := ensureMachineWithPorts(t.Context(), client, "m3", "Running", "", "", nil, &ports); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("compliant running machine was edited or restarted: %v", runner.calls)
	}
}

func TestEnsureMachineWithPortsRefusesRunningOldInstanceWithoutEdit(t *testing.T) {
	runner := &sequenceRunner{errors: map[string]error{
		"edit": errors.New("cannot edit a running instance"),
	}}
	ports, err := newDarwinPortContract(defaultDarwinGUIPort)
	if err != nil {
		t.Fatal(err)
	}
	client := &limaClient{
		info:   limaInfo{Path: "limactl"},
		runner: runner,
		instanceConfig: func(string) ([]byte, error) {
			return []byte("portForwards:\n  - guestPort: 4001\n    hostPort: 4001\n"), nil
		},
	}
	_, err = ensureMachineWithPorts(t.Context(), client, "old-m3", "Running", "", "", nil, &ports)
	if err == nil || !strings.Contains(err.Error(), "stop then start to migrate") {
		t.Fatalf("old running instance error = %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("old running instance attempted a Lima mutation: %v", runner.calls)
	}
}

var errMissingTestFile = errors.New("missing test file")

func writeStringFile(path, value string) error {
	return atomicWriteFile(path, []byte(value), 0o600)
}

func readFileForTest(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errMissingTestFile
	}
	return data, nil
}

func TestRunDarwinStopSyncFailureLeavesMachineRunning(t *testing.T) {
	cc := newStubControlClient()
	cc.labs["lab1"] = labYAML("lab1", "guest")
	cc.getErrFor["lab1"] = errors.New("control channel dropped")
	runner := &sequenceRunner{errors: map[string]error{
		"stop": errors.New("stop must not be called"),
	}}
	client := &limaClient{info: limaInfo{Path: "limactl"}, runner: runner}
	state := "Running"
	config := darwinSyncConfig{
		ImagesDir: filepath.Join(t.TempDir(), "images"),
		LabsDir:   filepath.Join(t.TempDir(), "labs"),
	}
	err := runDarwinStop(t.Context(), client, "m3", state, config, time.Second, func() (controlClient, func(), error) {
		return cc, func() {}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "control channel dropped") {
		t.Fatalf("stop sync error = %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("Lima was called after stop sync failure: %v", runner.calls)
	}
	if state != "Running" {
		t.Fatalf("machine state changed after sync failure: %q", state)
	}
}

func TestStopNeverBuildsDeleteOrDataRemovalCommand(t *testing.T) {

	runner := &sequenceRunner{listOutputs: [][]byte{[]byte("m|Stopped\n")}}
	client := &limaClient{info: limaInfo{Path: "limactl"}, runner: runner}
	if err := stopMachine(t.Context(), client, "m", "Running", 2*time.Second); err != nil {
		t.Fatal(err)
	}
	for _, call := range runner.calls {
		joined := strings.ToLower(strings.Join(call, " "))
		if strings.Contains(joined, "delete") || strings.Contains(joined, "factory-reset") || strings.Contains(joined, "rm -rf") {
			t.Fatalf("stop constructed destructive command: %v", call)
		}
	}
	if len(runner.calls) < 2 || runner.calls[0][1] != "stop" || runner.calls[1][1] != "list" {
		t.Fatalf("stop ordering = %v", commandNames(runner.calls))
	}
}
