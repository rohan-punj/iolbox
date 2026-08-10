//go:build !linux

package main

import "errors"

func killCgroup(string) error { return errors.New("stale cleanup requires linux") }

func deleteKernelObjects(string, string) error { return errors.New("stale cleanup requires linux") }
