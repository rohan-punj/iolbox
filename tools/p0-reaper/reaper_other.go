//go:build !linux

package main

import "errors"

func runReaper(string, string, string) error { return errors.New("reaper probe requires linux") }
