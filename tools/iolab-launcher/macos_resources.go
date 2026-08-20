package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
)

// macos_resources.go — editable vCPU/RAM sizing for the guest, applied
// before the launcher creates/starts the Lima VM. Mirrors prompt.go's
// existing Windows QEMU pattern (--smp/--mem, prompted interactively when
// stdin is a real terminal and the flag wasn't set explicitly) rather than
// inventing a new UX: --cpus/--memory-gib win when passed; otherwise, on an
// interactive terminal, the operator gets one "[default]: " question per
// value with the PROFILE's own declared sizing as the default (not a
// hardcoded guess), so accepting the defaults reproduces today's behavior
// exactly. Non-interactive/scripted/CI runs are never blocked on a prompt.

// giBValuePattern matches the leading integer GiB count in a Lima-format
// memory string like "4GiB" — profiles.env/pin files only ever use whole
// GiB values today, so this intentionally doesn't handle MiB or fractional
// GiB; if that ever changes, this is the one place to extend.
var giBValuePattern = regexp.MustCompile(`^(\d+)GiB$`)

// parseGiBValue extracts the integer GiB count from a Lima-format memory
// string, falling back to def for anything it doesn't recognize (should
// only happen if a profile's pin file is malformed — the prompt still needs
// SOME default to show rather than failing outright).
func parseGiBValue(s string, def int) int {
	m := giBValuePattern.FindStringSubmatch(s)
	if m == nil {
		return def
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// resolveDarwinResources decides the final CPUs/Memory to render into the
// Lima template, given the profile's own declared defaults (defaultCPUs,
// defaultMemory — Lima-format strings, e.g. "4" and "4GiB") and whatever
// the operator supplied via --cpus/--memory-gib. Explicit flags always win.
// Otherwise, on an interactive terminal, each value not explicitly set is
// asked about individually (matching promptResources' per-flag
// independence: --memory-gib alone still asks just the CPUs question).
// Returns Lima-format strings ready to substitute directly for
// profile.CPUs/profile.Memory.
func resolveDarwinResources(interactive bool, explicit map[string]bool, cpusFlag, memGiBFlag int, defaultCPUs, defaultMemory string) (string, string) {
	cpus := defaultCPUs
	memory := defaultMemory
	if explicit["cpus"] {
		cpus = strconv.Itoa(cpusFlag)
	}
	if explicit["memory-gib"] {
		memory = fmt.Sprintf("%dGiB", memGiBFlag)
	}
	if !interactive {
		return cpus, memory
	}
	r := bufio.NewReader(os.Stdin)
	if !explicit["cpus"] {
		cpus = strconv.Itoa(promptedValue(r, os.Stderr, "vCPUs for the guest", parseGiBOrInt(defaultCPUs)))
	}
	if !explicit["memory-gib"] {
		memory = fmt.Sprintf("%dGiB", promptedValue(r, os.Stderr, "RAM GiB for the guest", parseGiBValue(defaultMemory, 4)))
	}
	return cpus, memory
}

// parseGiBOrInt parses a plain integer string (profile.CPUs is just "4",
// no unit suffix) for use as a prompt default, falling back to 4 — the
// documented default guest sizing — on anything unparseable.
func parseGiBOrInt(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 4
	}
	return n
}
