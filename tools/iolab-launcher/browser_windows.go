//go:build windows

package main

import "os/exec"

// openBrowser preserves the existing Windows command exactly.
func openBrowser(url string) {
	logf("Opening %s in your browser...", url)
	cmd := exec.Command("cmd", "/c", "start", "", url)
	if err := cmd.Start(); err != nil {
		logf("  (could not auto-open the browser: %v — open %s manually)", err, url)
	}
}
