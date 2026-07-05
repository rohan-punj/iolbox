package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// config.go — launcher.json, a small persisted settings file that sits beside
// the exe and is owned by the control console (console.go). It captures the
// handful of settings a user can change from the :4002 page without a CLI
// flag: vCPU/RAM (applied on the NEXT boot — qemu can't be reconfigured while
// running), which backend deployment to drive, and the images\/labs\ folder
// overrides (mirrors --images-dir/--labs-dir so the page and the flags agree
// on where the file lives).
//
// launcher.json is deliberately NOT the source of truth for a running
// process — CLI flags always win at process start (see resolveConsoleOpts in
// console.go): this file only pre-seeds defaults for the next launch and is
// what the console page reads/writes via GET/PUT /api/config.

// launcherConfig is the on-disk shape of launcher.json.
type launcherConfig struct {
	CPUs       int    `json:"cpus"`
	RAMMB      int    `json:"ramMB"`
	Deployment string `json:"deployment"` // "qemu" | "wsl"
	ImagesDir  string `json:"imagesDir,omitempty"`
	LabsDir    string `json:"labsDir,omitempty"`
}

// defaultLauncherConfig mirrors runtime/resources.env (IOLBOX_VCPUS=4,
// IOLBOX_RAM_MB=4096) and main.go's own flag defaults, so a launcher.json
// that has never been touched behaves identically to today's CLI defaults.
func defaultLauncherConfig() launcherConfig {
	return launcherConfig{
		CPUs:       4,
		RAMMB:      4096,
		Deployment: string(backendAuto),
	}
}

const launcherConfigFileName = "launcher.json"

// loadLauncherConfig reads launcher.json from dir. A missing file is not an
// error — it just returns the defaults (first run). A corrupt file also
// falls back to defaults rather than failing the launch over a settings
// file; the console page will happily overwrite it on the next save.
func loadLauncherConfig(dir string) launcherConfig {
	cfg := defaultLauncherConfig()
	data, err := os.ReadFile(filepath.Join(dir, launcherConfigFileName))
	if err != nil {
		return cfg
	}
	// Unmarshal onto the defaults so a partial/older file (missing fields)
	// still yields sane values for whatever it doesn't specify.
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultLauncherConfig()
	}
	return cfg
}

// saveLauncherConfig writes cfg to dir via temp-file + rename (same pattern
// as foldersync.go's writeFileAtomic) so a crash mid-write never corrupts the
// file a later launch would otherwise silently fall back from.
func saveLauncherConfig(dir string, cfg launcherConfig) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	final := filepath.Join(dir, launcherConfigFileName)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// normalizeDeployment validates the deployment string, defaulting an empty
// or unrecognized value to "qemu" (the always-works fallback) rather than
// erroring — the config endpoint is meant to be forgiving.
func normalizeDeployment(s string) string {
	switch backend(s) {
	case backendQEMU, backendWSL, backendAuto:
		return s
	default:
		return string(backendQEMU)
	}
}
