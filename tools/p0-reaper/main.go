// p0-reaper is a focused ownership-split SIGCHLD probe. It registers its
// directly spawned GUI, leaves that PID to exec.Cmd.Wait, and reaps only an
// orphaned grandchild after the GUI is SIGKILLed.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	target := flag.String("target", "", "registered child executable")
	result := flag.String("result", "", "result file")
	setprivPath := flag.String("setpriv", "", "setpriv executable (setpriv launch mode)")
	launcherPath := flag.String("launcher", "", "p0-launcher executable (native launch mode)")
	flag.Parse()
	if *target == "" || *result == "" || (*setprivPath == "") == (*launcherPath == "") {
		fmt.Fprintln(os.Stderr, "usage: p0-reaper --target PATH --result PATH (--setpriv PATH | --launcher PATH)")
		os.Exit(2)
	}
	if err := runReaper(*target, *result, *setprivPath, *launcherPath); err != nil {
		fmt.Fprintf(os.Stderr, "p0-reaper: %v\n", err)
		os.Exit(1)
	}
}
