package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// prompt.go — the interactive vCPU/RAM questions asked at startup.
//
// The typical user DOUBLE-CLICKS iolbox-launcher.exe: a console window opens
// and this is their one chance to size the guest without learning flags.
// Ask the only two questions that matter, with Enter-Enter accepting the
// defaults. Anyone scripting the launcher passes --smp/--mem (or pipes
// stdin), and is never blocked on a prompt — CI and automation stay
// non-interactive by construction.

// stdinIsInteractive reports whether stdin is an actual console (a character
// device), as opposed to a pipe/file/closed handle. Prompting a non-console
// stdin would either hang a script forever or instantly consume garbage, so
// the prompt only fires for a real terminal.
func stdinIsInteractive() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// explicitFlags returns the set of flag names the user actually passed on
// the command line (flag.Visit only walks SET flags). An explicit --mem or
// --smp is a deliberate choice — asking again would be nagging.
// (Defined here, used by main; takes the visit func so tests can fake it.)
func explicitFlags(visit func(func(name string))) map[string]bool {
	set := make(map[string]bool)
	visit(func(name string) { set[name] = true })
	return set
}

// promptedValue asks one "label [default]: " question on w and reads one
// line from r. Empty input keeps def. Garbage or non-positive input prints a
// one-line note to w and keeps def — it deliberately does NOT re-prompt: a
// double-clicked console window must never wedge in a question loop, and the
// worst case of accepting the default is a working guest at the documented
// sizing. r/w are plain io interfaces (not os.Stdin/os.Stderr) so tests can
// drive this with a bufio.Reader over strings.NewReader and capture output in
// a bytes.Buffer — no real terminal required.
func promptedValue(r *bufio.Reader, w io.Writer, label string, def int) int {
	fmt.Fprintf(w, "%s [%d]: ", label, def)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		// EOF with nothing typed (e.g. the console closed): default.
		return def
	}
	return parsePromptValue(line, def, w)
}

// parsePromptValue is promptedValue's pure core, split out for tests: trim
// the line; empty keeps def; a positive integer wins; anything else keeps
// def (with a note written to w so the user sees why their input didn't take).
func parsePromptValue(line string, def int, w io.Writer) int {
	s := strings.TrimSpace(line)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		fmt.Fprintf(w, "  (%q is not a positive number - using %d)\n", s, def)
		return def
	}
	return n
}

// promptResources asks for vCPUs and RAM when appropriate and returns the
// values to use. It only prompts when stdin is interactive AND the given
// flag was not set explicitly — each question is skipped independently, so
// `--mem 8192` still asks just the vCPU question.
func promptResources(interactive bool, explicit map[string]bool, smp, memMB int) (int, int) {
	if !interactive {
		return smp, memMB
	}
	r := bufio.NewReader(os.Stdin)
	if !explicit["smp"] {
		smp = promptedValue(r, os.Stderr, "vCPUs for the guest", smp)
	}
	if !explicit["mem"] {
		memMB = promptedValue(r, os.Stderr, "RAM MB for the guest", memMB)
	}
	return smp, memMB
}
