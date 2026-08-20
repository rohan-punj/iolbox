//go:build !linux

package main

import "errors"

func launchTransition(string, string, []string, string, []string) error {
	return newLaunchFailure(launchExitLinuxOnly, "launch", errors.New("the capability transition is linux only"))
}
