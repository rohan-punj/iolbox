//go:build !linux

package main

import "errors"

func runHostileProbe() error { return errors.New("hostile probe requires linux") }

func attemptCapRegain() error { return errors.New("hostile probe requires linux") }

func memoryHog() {}

func forkBomb() {}
