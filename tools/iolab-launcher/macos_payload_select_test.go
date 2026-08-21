package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Tests for profile-aware payload selection. The release archive ships BOTH
// the untagged amd64 payload (Rosetta profiles) and an -linux-arm64 one
// (native-arm64 profile), so selection can no longer be arch-blind.

func writePayload(t *testing.T, dir, name string, mtime time.Time) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestSelectPayloadTwoPayloadArchiveIsArchCorrect is the regression test for
// the hazard that made this change mandatory.
//
// pack-release.sh stamps every archive member with one SOURCE_DATE_EPOCH, so
// on a clean extraction both payloads have IDENTICAL mtimes and the
// lexicographic tie-break decides. "iolbox-server-v1-linux-arm64.tar.gz"
// sorts BEFORE "iolbox-server-v1.tar.gz" because '-' (0x2D) < '.' (0x2E) —
// so the arch-blind selector handed arm64 to everyone.
func TestSelectPayloadTwoPayloadArchiveIsArchCorrect(t *testing.T) {
	dir := t.TempDir()
	epoch := time.Unix(1700000000, 0)
	amd64Path := writePayload(t, dir, "iolbox-server-v1.tar.gz", epoch)
	arm64Path := writePayload(t, dir, "iolbox-server-v1-linux-arm64.tar.gz", epoch)

	// Guard the premise: without filtering, the arm64 name really does win.
	if !(filepath.Base(arm64Path) < filepath.Base(amd64Path)) {
		t.Fatal("premise broken: the arm64 basename no longer sorts first")
	}

	for _, tc := range []struct {
		profile string
		want    string
	}{
		{"debian13", amd64Path},
		{"jammy", amd64Path},
		{"debian12", amd64Path},
		{nativeProfileTableName, arm64Path},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			got, err := selectPayload("", dir, tc.profile)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("profile %q selected %q, want %q", tc.profile, filepath.Base(got), filepath.Base(tc.want))
			}
		})
	}
}

func TestSelectPayloadMissingArchIsANamedError(t *testing.T) {
	t.Run("native with only an amd64 payload", func(t *testing.T) {
		dir := t.TempDir()
		writePayload(t, dir, "iolbox-server-v1.tar.gz", time.Unix(1700000000, 0))
		_, err := selectPayload("", dir, nativeProfileTableName)
		if err == nil {
			t.Fatal("expected an error; an amd64 payload must never satisfy the native profile")
		}
		if !strings.Contains(err.Error(), "linux-arm64") {
			t.Fatalf("error should name the missing architecture, got: %v", err)
		}
		if code := exitCode(err); code != exitPreflight {
			t.Fatalf("exit code = %d, want exitPreflight (%d)", code, exitPreflight)
		}
	})

	t.Run("rosetta with only an arm64 payload", func(t *testing.T) {
		dir := t.TempDir()
		writePayload(t, dir, "iolbox-server-v1-linux-arm64.tar.gz", time.Unix(1700000000, 0))
		_, err := selectPayload("", dir, "debian13")
		if err == nil {
			t.Fatal("expected an error; an arm64 payload must never satisfy a Rosetta profile")
		}
		if !strings.Contains(err.Error(), "amd64") {
			t.Fatalf("error should name the missing architecture, got: %v", err)
		}
	})
}

// TestSelectPayloadOldArchiveStillWorks covers a user extracting an existing
// single-payload release with a newer launcher: auto falls back to Rosetta
// (no native pin file), and the untagged payload must still be found.
func TestSelectPayloadOldArchiveStillWorks(t *testing.T) {
	dir := t.TempDir()
	want := writePayload(t, dir, "iolbox-server-v0.5.2.tar.gz", time.Unix(1600000000, 0))
	got, err := selectPayload("", dir, "debian13")
	if err != nil || got != want {
		t.Fatalf("payload/error = %q/%v, want %q", got, err, want)
	}
}

// TestSelectPayloadAcceptsExplicitAmd64Tag: CI does not pass --arch amd64,
// but pack-native.sh can produce that name and it is still an amd64 payload.
func TestSelectPayloadAcceptsExplicitAmd64Tag(t *testing.T) {
	dir := t.TempDir()
	want := writePayload(t, dir, "iolbox-server-v1-linux-amd64.tar.gz", time.Unix(1700000000, 0))
	got, err := selectPayload("", dir, "debian13")
	if err != nil || got != want {
		t.Fatalf("payload/error = %q/%v, want %q", got, err, want)
	}
}

// TestSelectPayloadExplicitOverrideAlwaysWins pins the backward-compatibility
// contract every M4-M7 hardware harness relies on: IOLBOX_TARBALL/--tarball
// is used as given, even when its name contradicts the profile.
func TestSelectPayloadExplicitOverrideAlwaysWins(t *testing.T) {
	dir := t.TempDir()
	explicit := writePayload(t, dir, "hand-built.tar.gz", time.Unix(1700000000, 0))
	writePayload(t, dir, "iolbox-server-v1-linux-arm64.tar.gz", time.Unix(1700000000, 0))

	for _, profile := range []string{"debian13", nativeProfileTableName} {
		got, err := selectPayload(explicit, dir, profile)
		if err != nil {
			t.Fatalf("profile %q: unexpected error: %v", profile, err)
		}
		if got != explicit {
			t.Fatalf("profile %q selected %q, want the explicit %q", profile, got, explicit)
		}
	}

	if _, err := selectPayload(filepath.Join(dir, "nope.tar.gz"), dir, "debian13"); err == nil {
		t.Fatal("a nonexistent explicit payload must still be an error")
	}
}

func TestPayloadMatchesProfile(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile string
		want    bool
	}{
		{"iolbox-server-v1.tar.gz", "debian13", true},
		{"iolbox-server-v1-linux-amd64.tar.gz", "debian13", true},
		{"iolbox-server-v1-linux-arm64.tar.gz", "debian13", false},
		{"iolbox-server-v1-linux-arm64.tar.gz", nativeProfileTableName, true},
		{"iolbox-server-v1.tar.gz", nativeProfileTableName, false},
		{"iolbox-server-v1-linux-amd64.tar.gz", nativeProfileTableName, false},
		{"not-a-payload.tar.gz", "debian13", false},
		{"iolbox-server-v1.zip", "debian13", false},
		{"iolbox-rootfs.tar", "debian13", false},
	} {
		if got := payloadMatchesProfile(tc.name, tc.profile); got != tc.want {
			t.Errorf("payloadMatchesProfile(%q, %q) = %v, want %v", tc.name, tc.profile, got, tc.want)
		}
	}
}
