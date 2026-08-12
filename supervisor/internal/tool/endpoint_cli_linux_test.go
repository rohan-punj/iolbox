//go:build linux

package tool

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEndpointLaunchEnvPCOnly(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		e := &Endpoint{endpointCfg: Config{NodeID: 7, RunDir: "/run", CLISocket: enabled}, endpointSocketPath: filepath.Join("/run", "tool", "7", "gui.sock")}
		env := e.endpointLaunchSpec().Env
		want := "IOLBOX_PC_CLI_SOCK=" + CLISocketFile("/run", 7)
		found := false
		for _, item := range env {
			name := strings.SplitN(item, "=", 2)[0]
			if !containsString(ScrubbedEnvAllowlist, name) {
				t.Fatalf("env %q is outside allowlist", item)
			}
			if item == want {
				found = true
			}
		}
		if found != enabled {
			t.Fatalf("CLISocket=%t env contains CLI path=%t: %#v", enabled, found, env)
		}
	}
}

// TestEndpointLaunchSpecAmbientCapsFromManifest covers the actual netprobe
// bug: endpointLaunchSpec used to hardcode AmbientCaps to ["NET_RAW"] for
// EVERY pack regardless of what its own manifest declared, so (a) the "pc"
// pack's `ip addr replace` failed with "Operation not permitted" (it needs
// NET_ADMIN, never granted), and (b) every pack declaring caps:[] (most of
// them) got an undeclared NET_RAW anyway. AmbientCaps must now be exactly
// Pack.Manifest.Caps, nothing more, nothing less.
func TestEndpointLaunchSpecAmbientCapsFromManifest(t *testing.T) {
	cases := []struct {
		name string
		caps []string
	}{
		{"declares nothing", nil},
		{"declares NET_RAW only", []string{"NET_RAW"}},
		{"declares NET_RAW + NET_ADMIN (netprobe)", []string{"NET_RAW", "NET_ADMIN"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &Endpoint{
				endpointCfg: Config{
					NodeID: 7,
					RunDir: "/run",
					Pack:   Pack{Manifest: Manifest{Caps: tc.caps}},
				},
				endpointSocketPath: filepath.Join("/run", "tool", "7", "gui.sock"),
			}
			got := e.endpointLaunchSpec().AmbientCaps
			if len(got) != len(tc.caps) {
				t.Fatalf("AmbientCaps = %#v, want %#v", got, tc.caps)
			}
			for i := range tc.caps {
				if got[i] != tc.caps[i] {
					t.Fatalf("AmbientCaps = %#v, want %#v", got, tc.caps)
				}
			}
		})
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
