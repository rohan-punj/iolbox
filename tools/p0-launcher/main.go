// p0-launcher is the native fallback for the exact util-linux setpriv command
// pinned by the learning-tool plan. It is only used when setpriv is too old or
// cannot express the securebits portion of the transition.
package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 5 || os.Args[1] != "--user" || os.Args[3] != "--" {
		fmt.Fprintln(os.Stderr, "usage: p0-launcher --user USER -- TARGET [ARGS...]")
		os.Exit(2)
	}
	if err := launchAs(os.Args[2], os.Args[4], os.Args[5:]); err != nil {
		fmt.Fprintf(os.Stderr, "p0-launcher: %v\n", err)
		os.Exit(1)
	}
}

func launchAs(user, target string, args []string) error {
	if user == "" || target == "" {
		return errors.New("user and target are required")
	}
	return launchTransition(user, target, args)
}
