// Command iolbox-app-stub is the compiled Mach-O executable behind
// IOLbox.app's Contents/MacOS/IOLbox. It resolves the archive root next to
// the .app, sanity-checks the sibling CLI is there, and opens a Terminal
// window running `./iolbox start` from that root — the double-click
// launcher described in docs/macos-launcher-icon-plan.md §11.
//
// This binary must stay tiny and dependency-free: it exists to reach a
// Terminal window, not to reimplement anything the iolbox CLI already does.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	exePath, err := resolveExecutable()
	if err != nil {
		alert("IOLbox couldn't start", "Could not resolve its own location: "+err.Error())
		os.Exit(1)
	}

	translocated := isTranslocated(exePath)
	root := computeRoot(exePath)

	if !sanityCheckRoot(root) {
		if translocated {
			alert(
				"IOLbox needs one more step",
				"macOS moved this app to a temporary, read-only location because "+
					"it's still marked as downloaded. In Terminal, run:\n\n"+
					"xattr -dr com.apple.quarantine <path to the extracted iolbox-macos-arm64 folder>\n\n"+
					"then double-click IOLbox.app again.",
			)
		} else {
			alert(
				"IOLbox can't find its files",
				"IOLbox.app can't find the iolbox CLI or lima/ folder next to it.\n\n"+
					"If you downloaded this from a browser, run "+
					"xattr -dr com.apple.quarantine on the extracted folder and try again. "+
					"Otherwise, make sure IOLbox.app is still inside the extracted "+
					"iolbox-macos-arm64 folder.",
			)
		}
		os.Exit(1)
	}

	scriptPath, err := writeLaunchScript(root)
	if err != nil {
		alert("IOLbox couldn't start", "Could not prepare the launch script: "+err.Error())
		os.Exit(1)
	}

	if err := exec.Command("/usr/bin/open", "-b", "com.apple.Terminal", scriptPath).Start(); err != nil {
		alert("IOLbox couldn't start", "Could not open Terminal: "+err.Error())
		os.Exit(1)
	}
}

// resolveExecutable returns the real on-disk path of this binary, following
// any symlink so a translocation-masking alias doesn't hide the actual
// (possibly translocated) location.
func resolveExecutable() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

// isTranslocated is a best-effort hint, not a documented API: Apple provides
// no supported way to detect App Translocation from inside the app, and the
// randomized path shape is an implementation detail that could change. A
// miss here only changes which alert wording is shown — see main()'s
// sanityCheckRoot branch, which never trusts this as the sole diagnosis.
func isTranslocated(resolvedExePath string) bool {
	// macOS paths are always POSIX-style regardless of the host this binary
	// was built on, so this deliberately checks "/" rather than
	// filepath.Separator.
	return strings.Contains(resolvedExePath, "/AppTranslocation/")
}

// computeRoot walks up from .../IOLbox.app/Contents/MacOS/<exe> to the
// archive root that IOLbox.app and the bare iolbox CLI both live in.
func computeRoot(exePath string) string {
	macOSDir := filepath.Dir(exePath)     // .../IOLbox.app/Contents/MacOS
	contentsDir := filepath.Dir(macOSDir) // .../IOLbox.app/Contents
	appDir := filepath.Dir(contentsDir)   // .../IOLbox.app
	return filepath.Dir(appDir)           // archive root
}

func sanityCheckRoot(root string) bool {
	cliInfo, err := os.Stat(filepath.Join(root, "iolbox"))
	if err != nil || cliInfo.IsDir() || cliInfo.Mode()&0o111 == 0 {
		return false
	}
	if _, err := os.Stat(filepath.Join(root, "lima", "profiles.env")); err != nil {
		return false
	}
	return true
}

// writeLaunchScript writes a fresh, per-process launch script and returns
// its path. A file this process creates at runtime does not inherit
// com.apple.quarantine the way a downloaded/extracted file does, so this
// designs out the third quarantine target that a committed .command file
// would carry (docs/macos-launcher-icon-plan.md §10 finding 2, §11.1 step 5).
// Naming it per-PID and writing via a temp file + rename avoids the
// concurrent-launch overwrite/race a single fixed path would have.
func writeLaunchScript(root string) (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cacheDir, "io.github.rohan-punj.iolbox")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	script := "#!/bin/bash\nset -e\ncd " + posixSingleQuote(root) + "\nexec ./iolbox start\n"

	final := filepath.Join(dir, "start-"+strconv.Itoa(os.Getpid())+".command")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, []byte(script), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return final, nil
}

// posixSingleQuote wraps s in single quotes for safe interpolation into a
// POSIX shell command, escaping any embedded single quotes.
func posixSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func alert(title, message string) {
	script := fmt.Sprintf(
		"display alert %s message %s as critical",
		appleScriptQuote(title),
		appleScriptQuote(message),
	)
	_ = exec.Command("/usr/bin/osascript", "-e", script).Run()
}

// appleScriptQuote wraps s in double quotes for an AppleScript string
// literal, escaping embedded double quotes and backslashes.
func appleScriptQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
