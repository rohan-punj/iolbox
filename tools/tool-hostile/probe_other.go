//go:build !linux

package main

import "errors"

func runHostileProbe() error { return errors.New("hostile probe requires linux") }

// A non-nil error means "a regain vector worked" on linux; off linux there is
// no probe to run at all, so this is an unsupported-platform error and the
// caller exits non-zero rather than claiming either verdict.
func attemptCapRegain() error { return errors.New("cap-regain probe requires linux") }

func memoryHog() {}

func forkBomb() {}
