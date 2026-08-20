package main

import (
	"bytes"
	"testing"
)

// TestParseDarwinArgsCPUsMemoryFlags confirms --cpus/--memory-gib actually
// reach darwinOptions and get recorded in ExplicitFlags, so
// resolveDarwinResources' explicit-flag precedence has correct input to
// work with. No prior test covered parseDarwinArgs at all before this.
func TestParseDarwinArgsCPUsMemoryFlags(t *testing.T) {
	var stderr bytes.Buffer
	opts, err := parseDarwinArgs([]string{"start", "--cpus", "8", "--memory-gib", "16"}, &stderr)
	if err != nil {
		t.Fatalf("parseDarwinArgs: %v", err)
	}
	if opts.CPUs != 8 || opts.MemoryGiB != 16 {
		t.Fatalf("opts.CPUs/MemoryGiB = %d/%d, want 8/16", opts.CPUs, opts.MemoryGiB)
	}
	if !opts.ExplicitFlags["cpus"] || !opts.ExplicitFlags["memory-gib"] {
		t.Fatalf("ExplicitFlags = %v, want both cpus and memory-gib set", opts.ExplicitFlags)
	}
}

// TestParseDarwinArgsCPUsMemoryDefaultToUnset confirms that WITHOUT the
// flags, CPUs/MemoryGiB stay at their zero value (meaning "use the
// profile's own default") and ExplicitFlags does not claim they were set —
// this is what lets resolveDarwinResources tell "operator asked for 0" (not
// possible, --cpus 0 would be rejected downstream by any real use) apart
// from "operator didn't pass the flag at all".
func TestParseDarwinArgsCPUsMemoryDefaultToUnset(t *testing.T) {
	var stderr bytes.Buffer
	opts, err := parseDarwinArgs([]string{"start"}, &stderr)
	if err != nil {
		t.Fatalf("parseDarwinArgs: %v", err)
	}
	if opts.CPUs != 0 || opts.MemoryGiB != 0 {
		t.Fatalf("opts.CPUs/MemoryGiB = %d/%d, want 0/0 (unset)", opts.CPUs, opts.MemoryGiB)
	}
	if opts.ExplicitFlags["cpus"] || opts.ExplicitFlags["memory-gib"] {
		t.Fatalf("ExplicitFlags = %v, want neither cpus nor memory-gib set", opts.ExplicitFlags)
	}
}

func TestParseGiBValue(t *testing.T) {
	cases := []struct {
		name string
		in   string
		def  int
		want int
	}{
		{"typical profile value", "4GiB", 99, 4},
		{"larger value", "16GiB", 99, 16},
		{"unrecognized unit falls back to default", "4096MiB", 99, 99},
		{"garbage falls back to default", "lots", 99, 99},
		{"empty falls back to default", "", 99, 99},
		{"zero GiB falls back to default (not a usable size)", "0GiB", 99, 99},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseGiBValue(c.in, c.def); got != c.want {
				t.Fatalf("parseGiBValue(%q, %d) = %d, want %d", c.in, c.def, got, c.want)
			}
		})
	}
}

func TestResolveDarwinResourcesNonInteractiveUsesProfileDefaults(t *testing.T) {
	// No flags passed, no terminal: must return the profile's own values
	// completely untouched, and must not attempt to read stdin (this test
	// would hang if it tried — go test's stdin is not a terminal).
	cpus, mem := resolveDarwinResources(false, map[string]bool{}, 0, 0, "4", "4GiB")
	if cpus != "4" || mem != "4GiB" {
		t.Fatalf("resolveDarwinResources(non-interactive, no flags) = %q/%q, want 4/4GiB", cpus, mem)
	}
}

func TestResolveDarwinResourcesExplicitFlagsWinNonInteractive(t *testing.T) {
	explicit := map[string]bool{"cpus": true, "memory-gib": true}
	cpus, mem := resolveDarwinResources(false, explicit, 8, 16, "4", "4GiB")
	if cpus != "8" || mem != "16GiB" {
		t.Fatalf("resolveDarwinResources(explicit flags) = %q/%q, want 8/16GiB", cpus, mem)
	}
}

func TestResolveDarwinResourcesExplicitFlagsWinEvenInteractive(t *testing.T) {
	// An explicit --cpus/--memory-gib must never be second-guessed by the
	// interactive prompt, same contract as prompt.go's --smp/--mem. If this
	// tried to prompt anyway it would hang reading go test's non-terminal
	// stdin, so a passing test here also proves no prompt was attempted.
	explicit := map[string]bool{"cpus": true, "memory-gib": true}
	cpus, mem := resolveDarwinResources(true, explicit, 8, 16, "4", "4GiB")
	if cpus != "8" || mem != "16GiB" {
		t.Fatalf("resolveDarwinResources(explicit flags, interactive) = %q/%q, want 8/16GiB", cpus, mem)
	}
}

func TestResolveDarwinResourcesPartialExplicitOnlyAsksTheOther(t *testing.T) {
	// Non-interactive path only, mirroring TestPromptResourcesNonInteractiveNeverPrompts's
	// approach: explicit memory-gib, unset cpus, but interactive=false means
	// neither question is actually asked -- this just confirms the
	// per-flag independence doesn't crash or cross-contaminate when only one
	// of the two is explicit.
	explicit := map[string]bool{"memory-gib": true}
	cpus, mem := resolveDarwinResources(false, explicit, 0, 32, "6", "4GiB")
	if cpus != "6" {
		t.Fatalf("cpus = %q, want unchanged profile default 6 (not explicit, non-interactive)", cpus)
	}
	if mem != "32GiB" {
		t.Fatalf("mem = %q, want the explicit 32GiB", mem)
	}
}
