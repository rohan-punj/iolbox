package tool

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LoadPack reads and validates the supervisor-side pack.json contract. The
// manifest is validated metadata ONLY: it is used for node-config validation
// and palette display, the supervisor never executes from pack.json's
// modules list, and the pack GUI's compiled module definitions remain
// authoritative for what actually runs.
func LoadPack(root string) (Pack, error) {
	packRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return Pack{}, fmt.Errorf("tool: resolve pack root %q: %w", root, err)
	}
	manifestPath, containedManifest := contained(packRoot, filepath.Join(packRoot, "pack.json"))
	if !containedManifest {
		return Pack{}, fmt.Errorf("tool: resolve pack manifest %q: missing or escapes pack root", filepath.Join(packRoot, "pack.json"))
	}
	packRoot = filepath.Dir(manifestPath)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return Pack{}, fmt.Errorf("tool: read pack manifest %q: %w", manifestPath, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Pack{}, fmt.Errorf("tool: decode pack manifest %q: %w", manifestPath, err)
	}
	if err := manifest.Validate(); err != nil {
		return Pack{}, fmt.Errorf("tool: validate pack %q: %w", packRoot, err)
	}
	if manifest.GUI.Transport == "" {
		manifest.GUI.Transport = "unix"
	}

	guiBin, err := manifestResolve(packRoot, manifest.GUI.Bin)
	if err != nil {
		return Pack{}, fmt.Errorf("tool: resolve gui.bin for pack %q: %w", manifest.ID, err)
	}
	scripts := make(map[string]string, len(manifest.Modules))
	for _, module := range manifest.Modules {
		script, resolveErr := manifestResolve(packRoot, module.Script)
		if resolveErr != nil {
			return Pack{}, fmt.Errorf("tool: resolve script for module %q in pack %q: %w", module.Key, manifest.ID, resolveErr)
		}
		scripts[module.Key] = script
	}

	limits := manifest.Limits
	if limits == nil {
		defaultLimits := DefaultLimits()
		limits = &defaultLimits
	}
	manifest.Limits = limits
	return Pack{
		ID:       manifest.ID,
		Root:     filepath.Clean(packRoot),
		Manifest: manifest,
		GUIBin:   guiBin,
		Scripts:  scripts,
	}, nil
}

// LoadPacks enumerates immediate subdirectories of dir and loads each pack.
// The returned slice always contains every pack that loaded successfully,
// sorted by pack ID, even when the returned error is non-nil. The error is an
// aggregate warning of the per-pack failures, never a fatal all-or-nothing
// result; one malformed pack must not prevent the supervisor from offering
// the others. A missing dir returns an empty slice and nil error. The caller
// (Server.InitRuntime) must cache the returned slice regardless of the error
// and log that error at warn level.
func LoadPacks(dir string) ([]Pack, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Pack{}, nil
		}
		return []Pack{}, fmt.Errorf("tool: enumerate pack directory %q: %w", dir, err)
	}

	packs := make([]Pack, 0, len(entries))
	packErrors := make([]error, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		packRoot := filepath.Join(dir, entry.Name())
		pack, loadErr := LoadPack(packRoot)
		if loadErr != nil {
			packErrors = append(packErrors, fmt.Errorf("tool: load pack %q: %w", entry.Name(), loadErr))
			continue
		}
		packs = append(packs, pack)
	}
	sort.Slice(packs, func(i, j int) bool {
		if packs[i].ID == packs[j].ID {
			return packs[i].Root < packs[j].Root
		}
		return packs[i].ID < packs[j].ID
	})
	return packs, errors.Join(packErrors...)
}

// Validate checks the supervisor-side metadata contract. In particular,
// gui.health is required because liveness and readiness probes hit exactly
// that path: 200 means the GUI is serving, while 404 or connection refusal
// means it is wedged or its health route is missing, without conflating those
// cases with an arbitrary GET /.
func (m Manifest) Validate() error {
	if m.ManifestVersion != ManifestVersion {
		return fmt.Errorf("tool: unsupported manifestVersion %d, want %d", m.ManifestVersion, ManifestVersion)
	}
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("tool: manifest id is required")
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("tool: manifest name is required")
	}
	if strings.TrimSpace(m.GUI.Bin) == "" {
		return errors.New("tool: manifest gui.bin is required")
	}
	if strings.TrimSpace(m.GUI.Health) == "" {
		return errors.New("tool: manifest gui.health is required")
	}
	if !strings.HasPrefix(m.GUI.Health, "/") {
		return fmt.Errorf("tool: manifest gui.health %q must begin with /", m.GUI.Health)
	}
	if m.GUI.Transport != "" && m.GUI.Transport != "unix" && m.GUI.Transport != "tcp" {
		return fmt.Errorf("tool: manifest gui.transport %q must be unix or tcp", m.GUI.Transport)
	}
	if err := manifestCheckCaps(m.Caps); err != nil {
		return err
	}

	groups := make(map[string]struct{}, len(m.Groups))
	for _, group := range m.Groups {
		groups[group] = struct{}{}
	}
	keys := make(map[string]struct{}, len(m.Modules))
	for index, module := range m.Modules {
		if strings.TrimSpace(module.Key) == "" {
			return fmt.Errorf("tool: manifest module %d key is required", index)
		}
		if _, exists := keys[module.Key]; exists {
			return fmt.Errorf("tool: manifest module key %q is duplicated", module.Key)
		}
		keys[module.Key] = struct{}{}
		if _, exists := groups[module.Group]; !exists {
			return fmt.Errorf("tool: manifest module %q has unknown group %q", module.Key, module.Group)
		}
		if strings.TrimSpace(module.Script) == "" {
			return fmt.Errorf("tool: manifest module %q script is required", module.Key)
		}
	}
	return nil
}

// manifestResolve applies the pack-relative path rule and delegates the
// canonical containment check to the package helper shared with the runtime.
func manifestResolve(packRoot, relative string) (string, error) {
	if strings.TrimSpace(relative) == "" {
		return "", errors.New("tool: pack path is required")
	}
	if filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" ||
		strings.HasPrefix(relative, "/") || strings.HasPrefix(relative, "\\") {
		return "", fmt.Errorf("tool: pack path %q must be relative", relative)
	}
	for _, part := range strings.FieldsFunc(relative, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return "", fmt.Errorf("tool: pack path %q contains ..", relative)
		}
	}
	resolved, ok := contained(packRoot, filepath.Join(packRoot, relative))
	if !ok {
		return "", fmt.Errorf("tool: pack path %q is missing or escapes pack root", relative)
	}
	return filepath.Clean(resolved), nil
}

// manifestCheckCaps rejects capability requests outside the supervisor's
// explicit allowlist before any endpoint can turn the metadata into a launch.
func manifestCheckCaps(caps []string) error {
	allowed := make(map[string]struct{}, len(AllowedCaps))
	for _, capName := range AllowedCaps {
		allowed[capName] = struct{}{}
	}
	for _, capName := range caps {
		if _, ok := allowed[capName]; !ok {
			return fmt.Errorf("tool: manifest capability %q is not allowed", capName)
		}
	}
	return nil
}
