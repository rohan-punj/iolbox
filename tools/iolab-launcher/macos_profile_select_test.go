package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func testProfileTable(t *testing.T, includeNative bool) profileTable {
	t.Helper()
	table := profileTable{
		Default: "debian13",
		Profiles: map[string]macOSProfile{
			"debian13": {Name: "debian13", Role: "DEFAULT"},
			"jammy":    {Name: "jammy", Role: "COMPATIBILITY"},
		},
	}
	if includeNative {
		table.Profiles[nativeProfileTableName] = macOSProfile{
			Name:        nativeProfileTableName,
			Role:        "CANDIDATE",
			ImageURL:    "https://cloud.debian.org/x.qcow2",
			ImageDigest: "sha512:deadbeef",
		}
	}
	return table
}

func passingFacts() hostFacts {
	return hostFacts{System: "Darwin", Arch: "arm64", FreeDiskKB: 64 * 1024 * 1024}
}

// stubLimactlSupportingVZ writes a tiny executable stub that answers
// `limactl info` with a vmTypes list containing "vz", so nativePreflight's
// lima_vz check can PASS on a dev box that has no real Lima installed. This
// is the only preflight check that shells out; every other check is
// satisfied by passingFacts()/testProfileTable() directly.
func stubLimactlSupportingVZ(t *testing.T) string {
	t.Helper()
	name, body := "limactl", "#!/bin/sh\necho '{\"vmTypes\":[\"qemu\",\"vz\"]}'\n"
	if runtime.GOOS == "windows" {
		name, body = "limactl.bat", "@echo off\r\necho {\"vmTypes\":[\"qemu\",\"vz\"]}\r\n"
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func tempConfigDir(t *testing.T) func() (string, error) {
	t.Helper()
	dir := t.TempDir()
	return func() (string, error) { return dir, nil }
}

func TestResolveProfileSelectionNameMapping(t *testing.T) {
	table := testProfileTable(t, true)
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{selectionRosettaAMD64, "debian13", false},
		{selectionNativeARM64, nativeProfileTableName, false},
		{"jammy", "jammy", false},
		{"nonexistent", "", true},
	}
	for _, tc := range cases {
		got, err := resolveProfileSelectionName(tc.in, table)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: expected error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: got (%q, %v), want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestResolveProfileSelectionExplicitFlagWins(t *testing.T) {
	table := testProfileTable(t, true)
	configDir := tempConfigDir(t)
	// Persist a conflicting prior choice; the explicit flag must still win.
	if err := persistProfileChoice(selectionNativeARM64, configDir); err != nil {
		t.Fatal(err)
	}
	res, err := resolveProfileSelection(context.Background(), selectionRosettaAMD64, table, passingFacts(), "", "", configDir)
	if err != nil {
		t.Fatal(err)
	}
	if res.Selected != selectionRosettaAMD64 || res.Source != "explicit-flag" || res.ProfileName != "debian13" {
		t.Fatalf("got %+v", res)
	}
}

func TestResolveProfileSelectionForcedNativeFailsClosed(t *testing.T) {
	table := testProfileTable(t, true)
	configDir := tempConfigDir(t)
	badFacts := hostFacts{System: "Darwin", Arch: "amd64"} // not Apple Silicon -> preflight must fail

	_, err := resolveProfileSelection(context.Background(), selectionNativeARM64, table, badFacts, "", "", configDir)
	if err == nil {
		t.Fatal("expected forced native-arm64 to fail closed on a non-Apple-Silicon host, got nil error")
	}
}

// TestResolveProfileSelectionAutoPrefersNativeWhenPreflightPasses covers the
// owner's promotion ruling (docs/macos-m7-phase6-handoff.md): a bare "auto"
// with no persisted choice now runs native preflight unconditionally and
// picks native-arm64 whenever it passes. There is no test-only opt-in hook
// any more -- this is production behavior.
func TestResolveProfileSelectionAutoPrefersNativeWhenPreflightPasses(t *testing.T) {
	table := testProfileTable(t, true)
	configDir := tempConfigDir(t)
	limactl := stubLimactlSupportingVZ(t)
	res, err := resolveProfileSelection(context.Background(), "", table, passingFacts(), limactl, "", configDir)
	if err != nil {
		t.Fatal(err)
	}
	if res.Requested != selectionAuto || res.Selected != selectionNativeARM64 || res.ProfileName != nativeProfileTableName || res.Source != "auto-native" {
		t.Fatalf("got %+v, want auto to select native-arm64 via auto-native", res)
	}
	if res.FallbackReason != "" {
		t.Fatalf("got FallbackReason %q, want empty on a clean native selection", res.FallbackReason)
	}
}

// TestResolveProfileSelectionAutoFallsBackToRosettaOnFailedPreflight is the
// retained explicit Rosetta fallback the PROMOTE clause requires: auto must
// degrade to rosetta-amd64 with a reason, never error out.
func TestResolveProfileSelectionAutoFallsBackToRosettaOnFailedPreflight(t *testing.T) {
	table := testProfileTable(t, true)
	configDir := tempConfigDir(t)
	limactl := stubLimactlSupportingVZ(t)
	badFacts := hostFacts{System: "Darwin", Arch: "amd64", FreeDiskKB: 64 * 1024 * 1024} // not Apple Silicon
	res, err := resolveProfileSelection(context.Background(), "", table, badFacts, limactl, "", configDir)
	if err != nil {
		t.Fatal(err)
	}
	if res.Requested != selectionAuto || res.Selected != selectionRosettaAMD64 || res.ProfileName != "debian13" || res.Source != "auto-fallback-rosetta" || res.FallbackReason == "" {
		t.Fatalf("got %+v, want an auto-fallback-rosetta result with a FallbackReason", res)
	}
}

func TestResolveProfileSelectionAutoFallsBackWithoutLimactl(t *testing.T) {
	table := testProfileTable(t, true)
	configDir := tempConfigDir(t)
	// No limactl path at all -> lima_vz preflight check fails closed -> auto
	// must fall back to rosetta-amd64, never error.
	res, err := resolveProfileSelection(context.Background(), "", table, passingFacts(), "", "", configDir)
	if err != nil {
		t.Fatal(err)
	}
	if res.Requested != selectionAuto || res.Selected != selectionRosettaAMD64 || res.Source != "auto-fallback-rosetta" || res.FallbackReason == "" {
		t.Fatalf("got %+v, want an auto-fallback-rosetta result with a FallbackReason", res)
	}
}

// TestResolveProfileSelectionAutoFallsBackWhenNativeRowAbsent covers an asset
// root whose profiles.env has no native-arm64 row at all: auto must still
// resolve, to rosetta-amd64, with the missing-row reason recorded.
func TestResolveProfileSelectionAutoFallsBackWhenNativeRowAbsent(t *testing.T) {
	table := testProfileTable(t, false)
	configDir := tempConfigDir(t)
	res, err := resolveProfileSelection(context.Background(), "", table, passingFacts(), stubLimactlSupportingVZ(t), "", configDir)
	if err != nil {
		t.Fatal(err)
	}
	if res.Selected != selectionRosettaAMD64 || res.Source != "auto-fallback-rosetta" || res.FallbackReason == "" {
		t.Fatalf("got %+v, want an auto-fallback-rosetta result with a FallbackReason", res)
	}
}

func TestResolveProfileSelectionPersistedChoiceHonored(t *testing.T) {
	table := testProfileTable(t, true)
	configDir := tempConfigDir(t)
	if err := persistProfileChoice(selectionRosettaAMD64, configDir); err != nil {
		t.Fatal(err)
	}
	res, err := resolveProfileSelection(context.Background(), "", table, passingFacts(), "", "", configDir)
	if err != nil {
		t.Fatal(err)
	}
	if res.Selected != selectionRosettaAMD64 || res.Source != "persisted" {
		t.Fatalf("got %+v, want persisted rosetta-amd64", res)
	}
}

func TestResolveProfileSelectionPersistedNativeFallsBackOnFailedPreflight(t *testing.T) {
	table := testProfileTable(t, true)
	configDir := tempConfigDir(t)
	if err := persistProfileChoice(selectionNativeARM64, configDir); err != nil {
		t.Fatal(err)
	}
	badFacts := hostFacts{System: "Darwin", Arch: "amd64"}
	res, err := resolveProfileSelection(context.Background(), "", table, badFacts, "", "", configDir)
	if err != nil {
		t.Fatal(err)
	}
	if res.Requested != selectionNativeARM64 || res.Selected != selectionRosettaAMD64 || res.Source != "persisted-fallback-rosetta" || res.FallbackReason == "" {
		t.Fatalf("got %+v, want a persisted-fallback-rosetta result", res)
	}
}

func TestPersistAndReadProfileChoiceRoundTrip(t *testing.T) {
	configDir := tempConfigDir(t)
	if err := persistProfileChoice(selectionNativeARM64, configDir); err != nil {
		t.Fatal(err)
	}
	got, err := readPersistedProfileChoice(configDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != selectionNativeARM64 {
		t.Fatalf("got %q, want %q", got, selectionNativeARM64)
	}
}

func TestReadPersistedProfileChoiceMissingIsNotAnError(t *testing.T) {
	configDir := tempConfigDir(t)
	got, err := readPersistedProfileChoice(configDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty for a first run with no history", got)
	}
}

func TestNativePreflightFailsClosedOnEachCheck(t *testing.T) {
	table := testProfileTable(t, true)

	// Apple Silicon check fails.
	pf := nativePreflight(context.Background(), hostFacts{System: "Darwin", Arch: "amd64", FreeDiskKB: 64 * 1024 * 1024}, table, "", "")
	if pf.OK {
		t.Fatal("expected apple_silicon check to fail closed")
	}
	if pf.Checks["apple_silicon"] == "" {
		t.Fatal("expected apple_silicon check to be recorded")
	}

	// Missing native profile row -> digests check fails.
	pf2 := nativePreflight(context.Background(), passingFacts(), testProfileTable(t, false), "", "")
	if pf2.OK || pf2.Checks["digests"] == "" {
		t.Fatalf("expected digests check to fail closed when native-arm64 row is absent: %+v", pf2)
	}

	// Low disk -> resources check fails.
	lowDisk := hostFacts{System: "Darwin", Arch: "arm64", FreeDiskKB: 1}
	pf3 := nativePreflight(context.Background(), lowDisk, table, "", "")
	if pf3.OK || pf3.Checks["resources"] == "" {
		t.Fatalf("expected resources check to fail closed on low disk: %+v", pf3)
	}
}

// TestNativePreflightDigestsPassOnTheRealShallowTable is a regression test
// for a real bug found on physical hardware (2026-08-19): the production
// call chain (resolveProfileSelection <- loadProfileTableOnly, NOT the
// fully-populated loadMacOSProfile) only ever gives nativePreflight a
// macOSProfile whose ImageURL/ImageDigest were never set -- those fields
// are populated exclusively by loadMacOSProfile's own pin-file read. Every
// existing test up to this one used testProfileTable(), which pre-populates
// ImageURL/ImageDigest directly and so could never catch this: on the
// actual Mac, "forced native-arm64" failed preflight's digests check with
// "native-arm64 profile is missing a pinned image URL/digest" on every
// single attempt, because the table it received really did have those
// fields empty. This test exercises the exact real path -- parse the
// shipped profiles.env with loadProfileTableOnly, then run nativePreflight
// with the real assetRoot -- so a regression here fails loudly again.
func TestNativePreflightDigestsPassOnTheRealShallowTable(t *testing.T) {
	assetRoot := filepath.Join("..", "..", "packaging", "macos")
	table, err := loadProfileTableOnly(assetRoot)
	if err != nil {
		t.Fatal(err)
	}
	shallow := table.Profiles[nativeProfileTableName]
	if shallow.ImageDigest != "" || shallow.ImageURL != "" {
		t.Fatalf("test premise violated: loadProfileTableOnly unexpectedly populated pin fields: %+v", shallow)
	}
	pf := nativePreflight(context.Background(), passingFacts(), table, "", assetRoot)
	if got := pf.Checks["digests"]; got == "" || got[:4] != "PASS" {
		t.Fatalf("digests check = %q, want a PASS derived from the real pin file at %s", got, filepath.Join(assetRoot, "lima", shallow.PinEnv))
	}
}

func TestParseLimaVZSupport(t *testing.T) {
	ok, detail := parseLimaVZSupport([]byte(`{"vmTypes":["qemu","vz"]}`))
	if !ok || detail == "" {
		t.Fatalf("expected vz to be detected, got ok=%v detail=%q", ok, detail)
	}
	ok2, _ := parseLimaVZSupport([]byte(`{"vmTypes":["qemu"]}`))
	if ok2 {
		t.Fatal("expected vz absence to fail closed")
	}
	ok3, _ := parseLimaVZSupport([]byte(`not json`))
	if ok3 {
		t.Fatal("expected unparseable info to fail closed")
	}
}

func TestPersistProfileChoiceCreatesConfigDir(t *testing.T) {
	base := t.TempDir()
	configDir := func() (string, error) { return filepath.Join(base, "nested", "dir"), nil }
	if err := persistProfileChoice(selectionRosettaAMD64, configDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "nested", "dir", "iolbox", profileChoiceFileName)); err != nil {
		t.Fatalf("expected persisted file to exist: %v", err)
	}
}
