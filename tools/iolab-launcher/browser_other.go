//go:build !windows && !darwin

package main

import "os/exec"

func openBrowser(url string) {
	logf("Opening %s in your browser...", url)
	if err := exec.Command("xdg-open", url).Start(); err != nil {
		logf("  (could not auto-open the browser: %v — open %s manually)", err, url)
	}
}
