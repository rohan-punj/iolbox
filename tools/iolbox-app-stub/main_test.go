package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIsTranslocated(t *testing.T) {
	cases := map[string]bool{
		"/private/var/folders/zz/xyz/AppTranslocation/ABCD/d/IOLbox.app/Contents/MacOS/IOLbox": true,
		"/Users/rohan/Downloads/iolbox-macos-arm64/IOLbox.app/Contents/MacOS/IOLbox":           false,
		"/Users/rohan/Desktop/AppTranslocationNotes/IOLbox.app/Contents/MacOS/IOLbox":          false,
	}
	for path, want := range cases {
		if got := isTranslocated(path); got != want {
			t.Errorf("isTranslocated(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestComputeRoot(t *testing.T) {
	exe := filepath.Join("Users", "rohan", "Downloads", "iolbox-macos-arm64", "IOLbox.app", "Contents", "MacOS", "IOLbox")
	want := filepath.Join("Users", "rohan", "Downloads", "iolbox-macos-arm64")
	if got := computeRoot(exe); got != want {
		t.Errorf("computeRoot(%q) = %q, want %q", exe, got, want)
	}
}

func TestPosixSingleQuote(t *testing.T) {
	cases := map[string]string{
		"/plain/path":      `'/plain/path'`,
		"it's got a quote": `'it'"'"'s got a quote'`,
		"":                 `''`,
	}
	for in, want := range cases {
		if got := posixSingleQuote(in); got != want {
			t.Errorf("posixSingleQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAppleScriptQuote(t *testing.T) {
	cases := map[string]string{
		`hello`:      `"hello"`,
		`say "hi"`:   `"say \"hi\""`,
		`back\slash`: `"back\\slash"`,
	}
	for in, want := range cases {
		if got := appleScriptQuote(in); got != want {
			t.Errorf("appleScriptQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanityCheckRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on POSIX executable bits, meaningless on Windows")
	}
	root := t.TempDir()

	if sanityCheckRoot(root) {
		t.Fatal("expected sanityCheckRoot to fail on an empty root")
	}

	cliPath := filepath.Join(root, "iolbox")
	if err := os.WriteFile(cliPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if sanityCheckRoot(root) {
		t.Fatal("expected sanityCheckRoot to fail without lima/profiles.env")
	}

	limaDir := filepath.Join(root, "lima")
	if err := os.MkdirAll(limaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(limaDir, "profiles.env"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if !sanityCheckRoot(root) {
		t.Fatal("expected sanityCheckRoot to pass once iolbox + lima/profiles.env exist")
	}
}

func TestWriteLaunchScriptIsPerProcessAndExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on POSIX executable bits, meaningless on Windows")
	}
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	t.Setenv("LocalAppData", cacheHome)

	root := filepath.Join("some", "archive", "root")
	scriptPath, err := writeLaunchScript(root)
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected launch script to be executable, mode=%v", info.Mode())
	}

	contents, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(contents)
	if !strings.Contains(got, posixSingleQuote(root)) {
		t.Fatalf("expected script to reference quoted root %q, got: %s", root, got)
	}
	if !strings.Contains(got, "exec ./iolbox start") {
		t.Fatalf("expected script to exec ./iolbox start, got: %s", got)
	}
}
