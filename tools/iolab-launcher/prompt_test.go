package main

import (
	"bufio"
	"bytes"
	"flag"
	"strings"
	"testing"
)

func TestParsePromptValue(t *testing.T) {
	cases := []struct {
		name string
		in   string
		def  int
		want int
	}{
		{"empty keeps default", "\n", 4, 4},
		{"whitespace keeps default", "   \r\n", 4, 4},
		{"valid integer wins", "8\n", 4, 8},
		{"windows CRLF trimmed", "6\r\n", 4, 6},
		{"garbage keeps default", "lots\n", 4096, 4096},
		{"negative keeps default", "-2\n", 4, 4},
		{"zero keeps default", "0\n", 4096, 4096},
		{"float keeps default (whole integers only)", "2.5\n", 4, 4},
		{"no trailing newline (EOF line) still parses", "12", 4, 12},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			if got := parsePromptValue(c.in, c.def, &buf); got != c.want {
				t.Fatalf("parsePromptValue(%q, %d) = %d, want %d", c.in, c.def, got, c.want)
			}
		})
	}
}

// TestPromptedValue drives promptedValue end-to-end with a fake terminal
// (bufio.Reader over strings.NewReader) and a bytes.Buffer sink — no real
// stdin/stdout needed, matching this codebase's preference for pure,
// unit-testable cores around thin OS-facing wrappers.
func TestPromptedValue(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		def       int
		want      int
		wantNote  bool
		wantLabel string
	}{
		{"empty input keeps default", "\n", 4, 4, false, "vCPUs for the guest"},
		{"garbage keeps default and notes it", "banana\n", 4096, 4096, true, "RAM MB for the guest"},
		{"negative keeps default and notes it", "-5\n", 4, 4, true, "vCPUs for the guest"},
		{"valid positive input is used", "12\n", 4, 12, false, "vCPUs for the guest"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := bufio.NewReader(strings.NewReader(c.input))
			var out bytes.Buffer
			got := promptedValue(r, &out, c.wantLabel, c.def)
			if got != c.want {
				t.Fatalf("promptedValue(%q) = %d, want %d", c.input, got, c.want)
			}
			printed := out.String()
			wantPrompt := c.wantLabel + " ["
			if !strings.Contains(printed, wantPrompt) {
				t.Errorf("output %q missing prompt %q", printed, wantPrompt)
			}
			hasNote := strings.Contains(printed, "not a positive number")
			if hasNote != c.wantNote {
				t.Errorf("output %q note-presence = %v, want %v", printed, hasNote, c.wantNote)
			}
		})
	}
}

func TestExplicitFlags(t *testing.T) {
	// Simulate a flag set where only --mem was passed: the prompt must treat
	// mem as decided and smp as still askable.
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.Int("mem", 4096, "")
	fs.Int("smp", 4, "")
	if err := fs.Parse([]string{"--mem", "8192"}); err != nil {
		t.Fatal(err)
	}
	set := explicitFlags(func(f func(string)) { fs.Visit(func(fl *flag.Flag) { f(fl.Name) }) })
	if !set["mem"] || set["smp"] {
		t.Fatalf("explicitFlags = %v, want mem set and smp unset", set)
	}
}

func TestPromptResourcesNonInteractiveNeverPrompts(t *testing.T) {
	// interactive=false must return the inputs untouched without reading
	// stdin at all — this is the CI/piped-stdin guarantee. (If it tried to
	// read, this test would hang; go test's stdin is not a terminal.)
	smp, mem := promptResources(false, map[string]bool{}, 4, 4096)
	if smp != 4 || mem != 4096 {
		t.Fatalf("promptResources(non-interactive) = %d/%d, want 4/4096", smp, mem)
	}
}
