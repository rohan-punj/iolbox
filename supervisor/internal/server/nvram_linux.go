//go:build linux

package server

import (
	"os"
	"path/filepath"

	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
	"github.com/rohanpunj/iolbox/supervisor/internal/node"
	"github.com/rohanpunj/iolbox/supervisor/internal/nvram"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
)

// prepareLabDir creates the shared lab directory and writes the whole-lab
// artifacts every co-located IOL instance reads from its cwd:
//
//   - NETMAP  — a static-tap line for every IOL interface (see netmapFor).
//   - iourc   — the runtime's IOU license, so all instances share one license.
//   - nvram_<id> — per-IOL-node NVRAM with the node's startupConfig injected, so
//     nodes boot pre-configured and IOS-XE PnP never engages (P0 correction #3).
//
// It is idempotent and safe to call before every (re)start; it only (re)writes
// the shared files, never touching a node's runtime state. Called by startNodes
// on Linux before spawning any node (after refreshFabric).
func (s *Server) prepareLabDir(ll *loadedLab) error {
	dir := ll.labDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return protocol.Errorf(protocol.CodeNodeSpawnFailed, "lab dir %s: %v", dir, err)
	}

	// Whole-lab static-tap NETMAP (topology-independent; already computed by
	// startNodes -> refreshFabric).
	netmapPath := filepath.Join(dir, "NETMAP")
	if err := os.WriteFile(netmapPath, []byte(s.netmapFor(ll)), 0o644); err != nil {
		return protocol.Errorf(protocol.CodeNodeSpawnFailed, "write NETMAP: %v", err)
	}

	// Realise the static-tap fabric: pre-create every IOL interface's tap +
	// netio<->tap iouyap (so its socket exists before IOL connects), and attach
	// each fabric link's member taps to its bridge. Must run before any IOL spawns.
	if err := s.startFabric(ll); err != nil {
		return err
	}

	// Shared iourc license.
	if err := s.writeIourc(dir); err != nil {
		return err
	}

	// Per-node NVRAM startup-config injection.
	for i := range ll.doc.Nodes {
		n := &ll.doc.Nodes[i]
		if n.Kind != lab.KindIOL {
			continue
		}
		if err := s.injectNVRAM(ll, n); err != nil {
			return err
		}
	}
	return nil
}

// writeIourc copies the runtime's IOU license into the shared lab dir as
// "iourc" so every co-located IOL instance finds it (IOL reads ./iourc from cwd,
// and Spec.Environ also points IOURC at this path). A missing source is not
// fatal here — a node with a bad/missing license fails visibly at boot, and P0
// generates the license via `supervisor -gen-iourc` at firstboot.
func (s *Server) writeIourc(dir string) error {
	src := s.iourcSource()
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // license placed elsewhere / generated at firstboot
		}
		return protocol.Errorf(protocol.CodeIourcFailed, "read iourc %s: %v", src, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "iourc"), data, 0o600); err != nil {
		return protocol.Errorf(protocol.CodeIourcFailed, "write iourc: %v", err)
	}
	return nil
}

// iourcSource resolves where the runtime's IOU license lives.
func (s *Server) iourcSource() string {
	if s.cfg.IourcPath != "" {
		return s.cfg.IourcPath
	}
	if s.cfg.ImageDir != "" {
		return filepath.Join(filepath.Dir(s.cfg.ImageDir), "iourc")
	}
	return "/opt/iolbox/iourc"
}

// injectNVRAM encodes a node's startupConfig into its nvram_<id> file in the
// shared lab dir before the node spawns, so IOL boots already configured.
//
// A node with no author-supplied startupConfig gets a generated minimal config
// (see defaultStartupConfig) so IOS still skips autoinstall / the initial
// setup dialog rather than booting into it. The NVRAM file is sized to fit;
// Spec.NVRAMKiB (see buildSpec / NVRAMKiBFor / effectiveStartupConfig) makes
// IOL's -n match, so IOL accepts the injected image without truncation.
func (s *Server) injectNVRAM(ll *loadedLab, n *lab.Node) error {
	cfg := effectiveStartupConfig(n)
	// Size the injected image to the same KiB the node's -n flag advertises,
	// so IOL accepts it without truncation (single source of truth in node).
	sizeKiB := node.NVRAMKiBFor(len(cfg))
	data, err := nvram.Encode(cfg, nvram.Options{Size: sizeKiB * 1024})
	if err != nil {
		return protocol.Errorf(protocol.CodeNvramCodecFailed, "encode nvram node %d: %v", n.ID, err)
	}
	path := filepath.Join(ll.labDir(), nvramFilename(n.ID))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return protocol.Errorf(protocol.CodeNvramCodecFailed, "write nvram node %d: %v", n.ID, err)
	}
	return nil
}

// extractNVRAM reads the node's NVRAM file from the shared lab dir and decodes
// the startup-config text. IOL writes NVRAM as "nvram_<5-digit-id>" in its cwd
// (the shared lab dir).
func (s *Server) extractNVRAM(ll *loadedLab, id int) (string, error) {
	path := filepath.Join(ll.labDir(), nvramFilename(id))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // no NVRAM yet = empty config, not an error
		}
		return "", protocol.Errorf(protocol.CodeNvramCodecFailed, "read nvram node %d: %v", id, err)
	}
	cfg, err := nvram.Decode(data)
	if err != nil {
		return "", protocol.Errorf(protocol.CodeNvramCodecFailed, "decode nvram node %d: %v", id, err)
	}
	return cfg, nil
}
