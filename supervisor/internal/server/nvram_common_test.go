package server

import (
	"strings"
	"testing"

	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
)

func TestSanitizeHostname(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"R1", "R1"},
		{"Core Switch", "CoreSwitch"},
		{"edge-router-1", "edge-router-1"},
		{"  spaced  name  ", "spacedname"},
		{"2960-sw", "sw"},  // leading digits/hyphens stripped until a letter
		{"---", ""},        // no usable characters
		{"123", ""},        // digits only -> empty (must start with a letter)
		{"Rσ1", "R1"},      // non-ASCII dropped, digit kept after leading letter
		{"a1-b2", "a1-b2"}, // legal token preserved
		{"", ""},           // empty in -> empty out
	}
	for _, c := range cases {
		if got := sanitizeHostname(c.in); got != c.want {
			t.Errorf("sanitizeHostname(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDefaultStartupConfig(t *testing.T) {
	got := defaultStartupConfig("R1", 3)
	want := "hostname R1\n" +
		"no ip domain lookup\n" +
		"line con 0\n" +
		" exec-timeout 0 0\n" +
		" logging synchronous\n" +
		"end\n"
	if got != want {
		t.Errorf("defaultStartupConfig(R1) =\n%q\nwant\n%q", got, want)
	}

	// A name that sanitizes to empty falls back to Node<id>.
	got = defaultStartupConfig("---", 7)
	if !strings.HasPrefix(got, "hostname Node7\n") {
		t.Errorf("fallback hostname = %q, want prefix %q", got, "hostname Node7\n")
	}
}

func TestEffectiveStartupConfig(t *testing.T) {
	// Non-empty StartupConfig is used verbatim (unchanged behavior).
	custom := "hostname CUSTOM\n!\nend\n"
	n := &lab.Node{ID: 1, Name: "ignored", StartupConfig: custom}
	if got := effectiveStartupConfig(n); got != custom {
		t.Errorf("effectiveStartupConfig with custom config = %q, want verbatim %q", got, custom)
	}

	// Empty StartupConfig yields the generated default keyed on the node name.
	n = &lab.Node{ID: 2, Name: "Core SW"}
	got := effectiveStartupConfig(n)
	if !strings.HasPrefix(got, "hostname CoreSW\n") {
		t.Errorf("effectiveStartupConfig default = %q, want hostname CoreSW", got)
	}
}
