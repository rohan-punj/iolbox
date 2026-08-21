package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Tests for the auto/existing-install continuity fix. See
// docs/macos-auto-profile-existing-machine-fix.md.
//
// Background: the Lima machine name is derived from the resolved profile
// ("iolbox-" + row), so letting bare `auto` migrate an existing install to
// native-arm64 points the launcher at a different, nonexistent VM — upgrade
// hard-fails and start silently creates a second VM while orphaning the real
// one.

func autoNativeSelection() profileSelectionResult {
	return profileSelectionResult{
		Requested:   selectionAuto,
		Selected:    selectionNativeARM64,
		ProfileName: nativeProfileTableName,
		Source:      "auto-native",
	}
}

func machineList(names ...string) []machineInfo {
	machines := make([]machineInfo, 0, len(names))
	for _, name := range names {
		machines = append(machines, machineInfo{Name: name, State: "Stopped"})
	}
	return machines
}

// noAttestation is the common case: nothing has ever written a
// structural-gate file for these machines.
func noAttestation(string) (string, bool) { return "", false }

// TestAutoKeepsExistingRosettaInstall is THE required case: an existing
// Rosetta VM, bare auto, native preflight already passed.
func TestAutoKeepsExistingRosettaInstall(t *testing.T) {
	table := testProfileTable(t, true)
	got := adjustAutoSelectionForExistingInstall(
		autoNativeSelection(), table, machineList("iolbox-debian13"), "", noAttestation)

	if got.ProfileName != "debian13" {
		t.Fatalf("ProfileName = %q, want debian13 (must not migrate an existing install)", got.ProfileName)
	}
	if got.Selected != selectionRosettaAMD64 {
		t.Fatalf("Selected = %q, want %q", got.Selected, selectionRosettaAMD64)
	}
	if got.Source != sourceAutoExistingRosettaMachine {
		t.Fatalf("Source = %q, want %q", got.Source, sourceAutoExistingRosettaMachine)
	}
	if got.Requested != selectionAuto {
		t.Fatalf("Requested = %q, want %q", got.Requested, selectionAuto)
	}
	// Non-empty FallbackReason is what makes runDarwinCLI print the
	// explanation and the migration command to stderr.
	if got.FallbackReason == "" {
		t.Fatal("FallbackReason is empty; the user would get no explanation")
	}
	if !strings.Contains(got.FallbackReason, "iolbox-debian13") {
		t.Fatalf("FallbackReason does not name the machine: %q", got.FallbackReason)
	}
	if !strings.Contains(got.FallbackReason, "--profile native-arm64") {
		t.Fatalf("FallbackReason does not tell the user how to migrate: %q", got.FallbackReason)
	}
}

// TestAutoStillPrefersNativeOnFreshHost is the guard that this fix does not
// quietly revert e2ffe34's promoted default.
func TestAutoStillPrefersNativeOnFreshHost(t *testing.T) {
	table := testProfileTable(t, true)
	for _, tc := range []struct {
		name     string
		machines []machineInfo
	}{
		{"no machines at all", nil},
		{"only foreign Lima machines", machineList("default", "docker")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := adjustAutoSelectionForExistingInstall(
				autoNativeSelection(), table, tc.machines, "", noAttestation)
			if got.Source != "auto-native" || got.ProfileName != nativeProfileTableName {
				t.Fatalf("fresh host must keep the promoted native default, got Source=%q ProfileName=%q", got.Source, got.ProfileName)
			}
		})
	}
}

func TestAutoKeepsNativeWhenNativeMachineExists(t *testing.T) {
	table := testProfileTable(t, true)
	for _, tc := range []struct {
		name     string
		machines []machineInfo
	}{
		{"already migrated", machineList("iolbox-native-arm64")},
		{"both present", machineList("iolbox-debian13", "iolbox-native-arm64")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := adjustAutoSelectionForExistingInstall(
				autoNativeSelection(), table, tc.machines, "", noAttestation)
			if got.Source != "auto-native" {
				t.Fatalf("Source = %q, want auto-native (user already has a native machine)", got.Source)
			}
		})
	}
}

// TestAdjustLeavesNonAutoNativeSelectionsUntouched is the narrowness guard.
func TestAdjustLeavesNonAutoNativeSelectionsUntouched(t *testing.T) {
	table := testProfileTable(t, true)
	machines := machineList("iolbox-debian13")
	for _, source := range []string{
		"explicit-flag",
		"persisted",
		"persisted-fallback-rosetta",
		"auto-fallback-rosetta",
	} {
		t.Run(source, func(t *testing.T) {
			in := profileSelectionResult{
				Requested: selectionAuto, Selected: selectionNativeARM64,
				ProfileName: nativeProfileTableName, Source: source,
			}
			if got := adjustAutoSelectionForExistingInstall(in, table, machines, "", noAttestation); got != in {
				t.Fatalf("selection with Source %q was modified: %+v", source, got)
			}
		})
	}
}

// TestAutoKeepsExistingLegacyRowWhenDefaultMachineAbsent covers a user whose
// only machine is iolbox-jammy: they must keep jammy, not be handed
// debian13 (whose machine does not exist either).
func TestAutoKeepsExistingLegacyRowWhenDefaultMachineAbsent(t *testing.T) {
	table := testProfileTable(t, true)
	got := adjustAutoSelectionForExistingInstall(
		autoNativeSelection(), table, machineList("iolbox-jammy"), "", noAttestation)
	if got.ProfileName != "jammy" {
		t.Fatalf("ProfileName = %q, want jammy", got.ProfileName)
	}
	if got.Source != sourceAutoExistingRosettaMachine {
		t.Fatalf("Source = %q, want %q", got.Source, sourceAutoExistingRosettaMachine)
	}
}

// TestExistingNonNativeProfileRowPrefersDefault pins the precedence and the
// determinism. profileTable.Profiles is an unordered map, so without the
// sort in existingNonNativeProfileRow a host with two eligible non-default
// rows would resolve differently from run to run.
func TestExistingNonNativeProfileRowPrefersDefault(t *testing.T) {
	table := testProfileTable(t, true)
	row, ok := existingNonNativeProfileRow(machineList("iolbox-jammy", "iolbox-debian13"), table)
	if !ok || row != "debian13" {
		t.Fatalf("row = %q ok = %v, want debian13 true (DEFAULT wins)", row, ok)
	}
	if _, ok := existingNonNativeProfileRow(machineList("iolbox-native-arm64"), table); ok {
		t.Fatal("the native row must never be returned as an existing non-native install")
	}
}

func TestExistingNonNativeProfileRowIsDeterministic(t *testing.T) {
	table := testProfileTable(t, true)
	// Two eligible non-default rows, DEFAULT machine absent. Repeat enough
	// times that Go's randomized map iteration would surface a difference.
	table.Profiles["aaa-row"] = macOSProfile{Name: "aaa-row", Role: "COMPATIBILITY"}
	machines := machineList("iolbox-jammy", "iolbox-aaa-row")
	first, ok := existingNonNativeProfileRow(machines, table)
	if !ok {
		t.Fatal("expected a row")
	}
	for i := 0; i < 200; i++ {
		got, ok := existingNonNativeProfileRow(machines, table)
		if !ok || got != first {
			t.Fatalf("nondeterministic row selection: got %q, first was %q", got, first)
		}
	}
	if first != "aaa-row" {
		t.Fatalf("row = %q, want aaa-row (lowest sorted name wins once DEFAULT is absent)", first)
	}
}

// --- attestation precedence -------------------------------------------------
//
// A machine NAME is not proof of its profile. These pin that the attestation
// outranks the name in both directions.

func TestAttestationOverridesMachineName(t *testing.T) {
	table := testProfileTable(t, true)

	t.Run("name says rosetta but attestation says native", func(t *testing.T) {
		attested := func(machine string) (string, bool) {
			if machine == "iolbox-debian13" {
				return nativeProfileTableName, true
			}
			return "", false
		}
		got := adjustAutoSelectionForExistingInstall(
			autoNativeSelection(), table, machineList("iolbox-debian13"), "", attested)
		if got.Source != "auto-native" {
			t.Fatalf("Source = %q, want auto-native (attestation says this machine is already native)", got.Source)
		}
	})

	t.Run("attestation names a different rosetta row", func(t *testing.T) {
		attested := func(machine string) (string, bool) {
			if machine == "iolbox-debian13" {
				return "jammy", true
			}
			return "", false
		}
		got := adjustAutoSelectionForExistingInstall(
			autoNativeSelection(), table, machineList("iolbox-debian13"), "", attested)
		if got.ProfileName != "jammy" {
			t.Fatalf("ProfileName = %q, want jammy (attestation outranks the derived name)", got.ProfileName)
		}
	})
}

// --- --machine / IOLBOX_MACHINE override ------------------------------------

func TestMachineOverrideBehaviour(t *testing.T) {
	table := testProfileTable(t, true)

	t.Run("disposable name that does not exist keeps native", func(t *testing.T) {
		// Every M4-M7 hardware harness lands here.
		got := adjustAutoSelectionForExistingInstall(
			autoNativeSelection(), table, machineList("iolbox-debian13"), "m4-run-12345", noAttestation)
		if got.Source != "auto-native" {
			t.Fatalf("Source = %q, want auto-native for a fresh override machine", got.Source)
		}
	})

	t.Run("existing override with unknown provenance keeps native", func(t *testing.T) {
		got := adjustAutoSelectionForExistingInstall(
			autoNativeSelection(), table, machineList("my-vm"), "my-vm", noAttestation)
		if got.Source != "auto-native" {
			t.Fatalf("Source = %q, want auto-native; provenance is unknown so we must not guess", got.Source)
		}
	})

	t.Run("existing override attested rosetta is protected", func(t *testing.T) {
		// The destructive case: without this, native guest scripts would be
		// staged into a Rosetta VM, and a RUNNING machine skips attestation
		// validation downstream so nothing else would catch it.
		attested := func(machine string) (string, bool) {
			if machine == "my-vm" {
				return "debian13", true
			}
			return "", false
		}
		got := adjustAutoSelectionForExistingInstall(
			autoNativeSelection(), table, machineList("my-vm"), "my-vm", attested)
		if got.ProfileName != "debian13" {
			t.Fatalf("ProfileName = %q, want debian13", got.ProfileName)
		}
		if got.Source != sourceAutoExistingRosettaMachine {
			t.Fatalf("Source = %q, want %q", got.Source, sourceAutoExistingRosettaMachine)
		}
	})

	t.Run("existing override attested native keeps native", func(t *testing.T) {
		attested := func(string) (string, bool) { return nativeProfileTableName, true }
		got := adjustAutoSelectionForExistingInstall(
			autoNativeSelection(), table, machineList("my-vm"), "my-vm", attested)
		if got.Source != "auto-native" {
			t.Fatalf("Source = %q, want auto-native", got.Source)
		}
	})
}

func TestMachineNameForProfileRowMatchesCLIDerivation(t *testing.T) {
	// Guards the duplicated derivation in macos_cli.go
	// (opts.Machine = "iolbox-" + profile.Name).
	if got := machineNameForProfileRow("debian13"); got != "iolbox-debian13" {
		t.Fatalf("machineNameForProfileRow = %q, want iolbox-debian13", got)
	}
}

// TestAttestedProfileRowForRejectsUnknownRows makes sure a stale or foreign
// attestation cannot inject a profile name this asset root does not have.
func TestAttestedProfileRowForRejectsUnknownRows(t *testing.T) {
	table := testProfileTable(t, true)
	fn := attestedProfileRowFor(table)
	// No attestation file exists for this machine on the test host.
	if row, ok := fn("iolbox-does-not-exist-" + t.Name()); ok {
		t.Fatalf("expected no attested row, got %q", row)
	}
}

// --- orchestration ----------------------------------------------------------
//
// These exercise finalizeAutoSelection, the seam runDarwinCLI actually calls.
// Without them, every pure-helper test above could pass while the CLI never
// invoked the helper at all. runDarwinCLI itself cannot be driven here — it
// refuses to run anywhere but Darwin/arm64 — so this seam is the closest
// honest coverage available on a non-Mac builder.

func TestFinalizeAutoSelectionAppliesTheAdjustment(t *testing.T) {
	table := testProfileTable(t, true)
	list := func(context.Context) ([]machineInfo, error) {
		return machineList("iolbox-debian13"), nil
	}
	got, err := finalizeAutoSelection(context.Background(), autoNativeSelection(), table, "upgrade", "", list)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ProfileName != "debian13" {
		t.Fatalf("ProfileName = %q, want debian13", got.ProfileName)
	}
	// The whole point: the machine the CLI will derive must be the user's
	// existing one, never iolbox-native-arm64.
	if machine := machineNameForProfileRow(got.ProfileName); machine != "iolbox-debian13" {
		t.Fatalf("effective machine = %q, want iolbox-debian13", machine)
	}
}

func TestFinalizeAutoSelectionFailsClosedOnInventoryError(t *testing.T) {
	table := testProfileTable(t, true)
	failing := func(context.Context) ([]machineInfo, error) {
		return nil, errors.New("limactl list: boom")
	}
	for _, command := range []string{"start", "upgrade"} {
		t.Run(command+" fails closed", func(t *testing.T) {
			_, err := finalizeAutoSelection(context.Background(), autoNativeSelection(), table, command, "", failing)
			if err == nil {
				t.Fatal("expected a fail-closed error; a transient list failure here followed by a success in runProvision would create the orphaning second VM")
			}
			if code := exitCode(err); code != exitPreflight {
				t.Fatalf("exit code = %d, want exitPreflight (%d)", code, exitPreflight)
			}
		})
	}
	for _, command := range []string{"status", "diagnose", "stop"} {
		t.Run(command+" tolerates the error", func(t *testing.T) {
			got, err := finalizeAutoSelection(context.Background(), autoNativeSelection(), table, command, "", failing)
			if err != nil {
				t.Fatalf("non-mutating command must not fail closed: %v", err)
			}
			if got.Source != "auto-native" {
				t.Fatalf("Source = %q, want auto-native unchanged", got.Source)
			}
		})
	}
}

func TestFinalizeAutoSelectionSkipsNonAutoNativeWithoutListing(t *testing.T) {
	table := testProfileTable(t, true)
	called := false
	list := func(context.Context) ([]machineInfo, error) {
		called = true
		return nil, errors.New("must not be called")
	}
	in := profileSelectionResult{
		Requested: selectionNativeARM64, Selected: selectionNativeARM64,
		ProfileName: nativeProfileTableName, Source: "explicit-flag",
	}
	got, err := finalizeAutoSelection(context.Background(), in, table, "start", "", list)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != in {
		t.Fatalf("explicit selection was modified: %+v", got)
	}
	if called {
		t.Fatal("limactl list must not be shelled for a non-auto-native selection")
	}
}

func TestFinalizeAutoSelectionHonoursMachineOverride(t *testing.T) {
	table := testProfileTable(t, true)
	// A disposable harness machine that does not exist yet: native proceeds.
	list := func(context.Context) ([]machineInfo, error) {
		return machineList("iolbox-debian13"), nil
	}
	got, err := finalizeAutoSelection(context.Background(), autoNativeSelection(), table, "start", "m4-run-999", list)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Source != "auto-native" {
		t.Fatalf("Source = %q, want auto-native for a fresh override machine", got.Source)
	}
}
