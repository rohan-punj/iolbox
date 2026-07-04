package server

import (
	"fmt"
	"strings"

	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
	"github.com/rohanpunj/iolbox/supervisor/internal/netmap"
)

// nvramFilename returns the NVRAM filename IOL reads/writes in its cwd for the
// given lab node id, e.g. "nvram_00002" for node id 1 (IOL instance 2). IOL
// names the file after its *instance* id, so this maps through
// netmap.InstanceID to stay in sync with the argv positional and NETMAP node
// id. Both the injector and the extractor call this one helper.
func nvramFilename(nodeID int) string {
	return fmt.Sprintf("nvram_%05d", netmap.InstanceID(nodeID))
}

// defaultStartupConfig returns a minimal IOS startup-config for a node that
// carries no author-supplied StartupConfig, so IOL skips autoinstall and the
// initial-configuration dialog and boots straight to a usable exec prompt. The
// hostname is derived from the node name (sanitized to an IOS-legal token) so
// the console prompt matches the GUI, falling back to "Node<id>" when the name
// has no usable characters. Called by injectNVRAM (nvram_linux.go) when
// StartupConfig is empty; a non-empty StartupConfig is always used verbatim.
func defaultStartupConfig(name string, id int) string {
	host := sanitizeHostname(name)
	if host == "" {
		host = fmt.Sprintf("Node%d", id)
	}
	return "hostname " + host + "\n" +
		"no ip domain lookup\n" +
		"line con 0\n" +
		" exec-timeout 0 0\n" +
		" logging synchronous\n" +
		"end\n"
}

// effectiveStartupConfig is the config actually injected into a node's NVRAM:
// the author-supplied StartupConfig verbatim when present, otherwise the
// generated minimal default. It is the single source of truth for both the
// NVRAM write (injectNVRAM) and the -n sizing (buildSpec), so they never
// disagree on how many bytes the config occupies.
func effectiveStartupConfig(n *lab.Node) string {
	if n.StartupConfig != "" {
		return n.StartupConfig
	}
	return defaultStartupConfig(n.Name, n.ID)
}

// sanitizeHostname reduces name to an IOS-legal hostname: letters, digits and
// hyphens only, and it must start with a letter (IOS rejects hostnames that
// begin with a digit or hyphen). Any other character is dropped, and leading
// non-letter characters are stripped. Returns "" if nothing usable remains, so
// the caller can substitute a deterministic fallback.
func sanitizeHostname(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9', r == '-':
			// Digits/hyphens are legal only after a leading letter.
			if b.Len() > 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}
