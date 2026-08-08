package tool

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The cleanup verification in the Linux probe treats "absent" as the success
// case after teardown, and the absence is observed through a stat error that
// has been annotated with context. os.IsNotExist does not traverse %w chains,
// so it classified a correct teardown as an unverifiable failure on the
// appliance. detectIsNotExist must see through arbitrary wrapping.
func TestDetectIsNotExistTraversesWrappedErrors(t *testing.T) {
	_, rawErr := os.Stat(filepath.Join(t.TempDir(), "absent"))
	if rawErr == nil {
		t.Fatal("stat of an absent path returned no error")
	}
	if !detectIsNotExist(rawErr) {
		t.Fatalf("detectIsNotExist(raw stat error) = false, error = %v", rawErr)
	}

	wrapped := fmt.Errorf("tool: host veth vtool9000000 is absent: %w", rawErr)
	if !detectIsNotExist(wrapped) {
		t.Fatalf("detectIsNotExist(wrapped stat error) = false, error = %v", wrapped)
	}
	doubleWrapped := fmt.Errorf("cleanup verify veth: %w", wrapped)
	if !detectIsNotExist(doubleWrapped) {
		t.Fatalf("detectIsNotExist(double-wrapped stat error) = false, error = %v", doubleWrapped)
	}

	if detectIsNotExist(nil) {
		t.Fatal("detectIsNotExist(nil) = true")
	}
	if detectIsNotExist(errors.New("permission denied")) {
		t.Fatal("detectIsNotExist(unrelated error) = true")
	}
}

func TestDetectCapabilitiesAggregation(t *testing.T) {
	results := make(map[string]detectStepResult, len(detectProbeSteps))
	for _, step := range detectProbeSteps {
		results[step.key] = detectStepResult{ok: true}
	}

	all := detectCapabilitiesFromResults(results)
	if !all.OK() {
		t.Fatalf("all probe results produced %+v", all)
	}
	if got := all.GateFeatures(); len(got) != 1 || got[0] != "tools" {
		t.Fatalf("all probe results gate features = %v, want [tools]", got)
	}
	if len(all.Reasons) != 0 {
		t.Fatalf("successful probe results retained reasons: %v", all.Reasons)
	}

	results["vethMoveRename"] = detectStepResult{
		reason: "tool: veth move/rename probe failed: test injection",
	}
	failed := detectCapabilitiesFromResults(results)
	if failed.OK() {
		t.Fatal("one false capability was advertised as OK")
	}
	if got := failed.GateFeatures(); len(got) != 0 {
		t.Fatalf("failed probe results gate features = %v, want empty", got)
	}
	if got := failed.Reasons["vethMoveRename"]; got != "tool: veth move/rename probe failed: test injection" {
		t.Fatalf("failed capability reason = %q", got)
	}
}

func TestDetectProbeStepsAreOrderedAndHaveReasons(t *testing.T) {
	want := []string{
		"netnsCreate",
		"vethCreate",
		"vethMoveRename",
		"cgroupDelegated",
		"ambientCapTransition",
		"unixProxy",
	}
	if len(detectProbeSteps) != len(want) {
		t.Fatalf("probe step count = %d, want %d", len(detectProbeSteps), len(want))
	}
	for index, step := range detectProbeSteps {
		if step.key != want[index] {
			t.Fatalf("probe step %d = %q, want %q", index, step.key, want[index])
		}
		if step.reason == "" {
			t.Fatalf("probe step %q has no failure reason", step.key)
		}
	}
}
