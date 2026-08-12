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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
