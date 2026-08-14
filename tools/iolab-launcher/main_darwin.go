//go:build darwin

package main

import (
	"fmt"
	"os"
	"runtime"
)

func main() {
	if runtime.GOARCH != "arm64" {
		fmt.Fprintln(os.Stderr, "iolbox macOS launcher requires Apple Silicon (arm64)")
		os.Exit(exitPreflight)
	}
	os.Exit(runDarwinCLI(os.Args[1:]))
}
