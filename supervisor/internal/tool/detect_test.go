package tool

import "testing"

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
