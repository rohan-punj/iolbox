//go:build !linux

package main

import "errors"

func launchTransition(string, string, []string) error {
	return errors.New("native capability transition requires linux")
}
