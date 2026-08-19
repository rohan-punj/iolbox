package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Phase 4 profile-selection layer. This sits ABOVE the generic, data-driven
// profileTable (macos_profiles.go): callers pick one of three LOGICAL
// selections, and this file resolves that selection to a concrete
// profileTable row name plus a human-readable reason, in this precedence
// order (plan section 10 item 3):
//
//  1. an explicit --profile flag (or IOLBOX_PROFILE env var) value wins;
//  2. otherwise, a previously persisted owner choice wins;
//  3. otherwise, "auto" applies -- which, until promotion, still defaults to
//     rosetta-amd64. A bare "auto" selecting native-arm64 requires an
//     explicit, test-only policy (IOLBOX_TEST_PREFER_NATIVE=1); production
//     "auto" must never silently start preferring native.
//
// nativeProfileName/rosettaAliasName are logical selections, distinct from
// profileTable row names (macos_profiles.go's "debian13"/"jammy"/"debian12"
// pick a *guest OS variant* under the rosetta-amd64 execution mode; this
// file's three values pick the *execution mode*). "rosetta-amd64" resolves
// to the profile table's DEFAULT row (currently debian13); a bare legacy
// profile-table name (e.g. "jammy") is still accepted directly for
// backward compatibility with M1-M6 callers/tests that never heard of the
// three-way selection.
const (
	selectionAuto         = "auto"
	selectionRosettaAMD64 = "rosetta-amd64"
	selectionNativeARM64  = "native-arm64"

	// nativeProfileTableName is the profileTable row (packaging/macos/lima/
	// profiles.env) that carries the native-arm64 Lima/VZ template, pin, and
	// guest asset paths. Kept as a single named constant rather than a new
	// profiles.env column: today there is exactly one native profile, and
	// the qemu-user translator identity below is likewise a property of
	// that one profile, not yet a generalized per-row field.
	nativeProfileTableName = "native-arm64"
	// nativeTranslatorName is the correctness-eligible translator Phase 3
	// selected for the native-arm64 guest (see docs/macos-m7-phase3-execution-plan.md,
	// FEX-Emu was BLOCKED, qemu-user is sole correctness-eligible). Preflight's
	// "translator" check verifies this identity is what the resolved profile
	// declares, not that a specific host-side binary is present -- the
	// translator itself runs inside the guest, installed by guest
	// provisioning, not by the macOS host.
	nativeTranslatorName = "qemu-user"

	// testPreferNativeEnv is the explicit, test-only policy hook plan section
	// 10 item 3 requires: until promotion, a bare "auto" selection must never
	// silently prefer native-arm64 on its own. Setting this to "1" is the
	// only way "auto" can resolve to native-arm64 without an explicit
	// --profile/persisted choice.
	testPreferNativeEnv = "IOLBOX_TEST_PREFER_NATIVE"
)

// profileChoiceFileName is where a successful explicit selection is
// persisted, under the same macOS user config root foldersync already uses
// (~/Library/Application Support/iolbox on a real Mac; see macos_sync.go).
const profileChoiceFileName = "profile-choice.env"

// profileSelectionResult is the fully resolved outcome: which profileTable
// row to load, which logical selection it corresponds to, why, and — when
// an automatic or forced native attempt could not be honored — the fallback
// reason status/diagnose must report truthfully.
type profileSelectionResult struct {
	// Requested is the logical selection as the caller asked for it, before
	// any fallback (e.g. "native-arm64" even if it then fell back).
	Requested string
	// Selected is the logical selection actually used after any fallback.
	Selected string
	// ProfileName is the profileTable row name to pass to loadMacOSProfile.
	ProfileName string
	// Source explains where Selected came from: explicit-flag / persisted /
	// auto-default-rosetta / auto-native-test-policy / auto-fallback-rosetta.
	Source string
	// FallbackReason is non-empty only when Requested != Selected.
	FallbackReason string
}

// resolveProfileSelectionName maps a logical selection or a legacy direct
// profileTable row name to the row name loadMacOSProfile should load. Empty
// stays empty (loadMacOSProfile's own default-row behavior applies).
func resolveProfileSelectionName(selection string, table profileTable) (string, error) {
	switch selection {
	case "", selectionAuto:
		return "", fmt.Errorf("resolveProfileSelectionName: auto/empty must be resolved by resolveProfileSelection, not looked up directly")
	case selectionRosettaAMD64:
		if table.Default == "" {
			return "", fmt.Errorf("profile table has no DEFAULT row to alias rosetta-amd64 to")
		}
		return table.Default, nil
	case selectionNativeARM64:
		if _, ok := table.Profiles[nativeProfileTableName]; !ok {
			return "", fmt.Errorf("native-arm64 profile is not present in this asset root's profiles.env (row %q missing)", nativeProfileTableName)
		}
		return nativeProfileTableName, nil
	default:
		// Legacy direct profile-table name (debian13/jammy/debian12/...):
		// accept it unchanged so pre-Phase-4 callers keep working.
		if _, ok := table.Profiles[selection]; ok {
			return selection, nil
		}
		return "", fmt.Errorf("unknown --profile value %q (want auto, rosetta-amd64, native-arm64, or a known profile-table row)", selection)
	}
}

// readPersistedProfileChoice reads a previously persisted owner choice.
// Returns "" with a nil error when nothing has ever been persisted (not an
// error condition — "auto" with no history is the common first run).
func readPersistedProfileChoice(configDir func() (string, error)) (string, error) {
	if configDir == nil {
		configDir = darwinUserConfigDir
	}
	root, err := configDir()
	if err != nil {
		return "", fmt.Errorf("resolve macOS user config directory: %w", err)
	}
	path := filepath.Join(root, "iolbox", profileChoiceFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read persisted profile choice: %w", err)
	}
	values, err := parsePinEnv(string(data))
	if err != nil {
		return "", fmt.Errorf("parse persisted profile choice %s: %w", path, err)
	}
	return values["IOLBOX_PROFILE_SELECTION"], nil
}

// persistProfileChoice writes the owner's explicit selection so a future
// bare "auto" run honors it. Only ever called with an explicit, successfully
// preflighted selection — never with "auto" itself and never with a
// selection that failed preflight/fell back, so the persisted file always
// names a choice that was proven usable at the time it was written.
func persistProfileChoice(selection string, configDir func() (string, error)) error {
	if configDir == nil {
		configDir = darwinUserConfigDir
	}
	root, err := configDir()
	if err != nil {
		return fmt.Errorf("resolve macOS user config directory: %w", err)
	}
	dir := filepath.Join(root, "iolbox")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	path := filepath.Join(dir, profileChoiceFileName)
	tmp := path + ".tmp"
	content := fmt.Sprintf("IOLBOX_PROFILE_SELECTION=%s\n", selection)
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	return os.Rename(tmp, path)
}

// nativePreflightResult is the non-mutating, fail-closed readiness verdict
// for native-arm64. Every check is read-only: no VM is created, started, or
// modified, and no host state is written by running preflight itself.
type nativePreflightResult struct {
	OK      bool
	Reason  string // first failing check's explanation, empty when OK
	Checks  map[string]string
	// order keeps Checks' human-readable rendering deterministic.
	order []string
}

func (r *nativePreflightResult) record(name string, ok bool, detail string) {
	if r.Checks == nil {
		r.Checks = make(map[string]string)
	}
	status := "PASS"
	if !ok {
		status = "FAIL"
		if r.OK { // first failure sets Reason/OK; keep evaluating remaining checks for a complete report
			r.Reason = name + ": " + detail
		}
		r.OK = false
	}
	r.Checks[name] = status + " (" + detail + ")"
	r.order = append(r.order, name)
}

// String renders every check in evaluation order, PASS or FAIL, for
// status/diagnose truthfulness (plan section 10 item 5).
func (r nativePreflightResult) String() string {
	var b strings.Builder
	for _, name := range r.order {
		fmt.Fprintf(&b, "%s=%s\n", name, r.Checks[name])
	}
	return b.String()
}

// limaVZInfo is the minimal shape this reads out of `limactl info --json` (a
// read-only query; it starts/stops nothing).
type limaVZInfo struct {
	VMTypes []string `json:"vmTypes"`
}

// limaSupportsVZ shells the read-only `limactl info` and reports whether the
// vz vmType is available. Fails closed: any error, or a response that omits
// "vz" from a real vmTypes list, is treated as "not proven usable" rather
// than assumed present.
func limaSupportsVZ(ctx context.Context, limactlPath string) (bool, string) {
	if limactlPath == "" {
		return false, "no limactl executable resolved"
	}
	out, err := exec.CommandContext(ctx, limactlPath, "info").Output()
	if err != nil {
		return false, "limactl info failed: " + err.Error()
	}
	return parseLimaVZSupport(out)
}

// parseLimaVZSupport is the pure classifier split out from limaSupportsVZ so
// it can be exercised on any host without a real limactl binary.
func parseLimaVZSupport(raw []byte) (bool, string) {
	var info limaVZInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return false, "could not parse limactl info JSON: " + err.Error()
	}
	for _, vt := range info.VMTypes {
		if strings.EqualFold(vt, "vz") {
			return true, "vz present in limactl info vmTypes " + strings.Join(info.VMTypes, ",")
		}
	}
	return false, "vz not present in limactl info vmTypes " + strings.Join(info.VMTypes, ",")
}

// nativePreflight runs every non-mutating readiness check plan section 10
// item 3 requires before a native-arm64 attempt: Apple Silicon, Lima/VZ,
// digests, translator, and resources. It never creates, starts, or mutates
// a Lima machine or any host file.
//
// assetRoot is needed for the digests check ONLY when table came from
// loadProfileTableOnly (the real production path: resolveProfileSelection
// must decide the logical selection BEFORE it knows which concrete row to
// hand loadMacOSProfile, so it necessarily only has the shallow,
// pin-file-unaware row shape). Found on real hardware (2026-08-19,
// physical Mac, first live "forced native-arm64" attempt): with a real
// profiles.env row present, this check failed every single time with
// "native-arm64 profile is missing a pinned image URL/digest" — table.
// Profiles[native-arm64].ImageDigest/ImageURL are ONLY ever populated by
// loadMacOSProfile (which reads the pin file), never by
// parseProfilesEnv/loadProfileTableOnly alone, so forced native-arm64 could
// never structurally pass this check. Unit tests never caught it because
// testProfileTable() pre-populates ImageURL/ImageDigest directly on a
// synthetic table, bypassing the real loadProfileTableOnly code path
// entirely. Fix: when the table's own row is unpopulated, read and
// validate the pin file directly instead of trusting fields that are
// simply never set at this call site.
func nativePreflight(ctx context.Context, facts hostFacts, table profileTable, limactlPath, assetRoot string) nativePreflightResult {
	var r nativePreflightResult
	r.OK = true // flipped false by the first failing record() call

	appleSilicon := facts.System == "Darwin" && facts.Arch == "arm64"
	r.record("apple_silicon", appleSilicon, fmt.Sprintf("host reports %s/%s", facts.System, facts.Arch))

	vzOK, vzDetail := limaSupportsVZ(ctx, limactlPath)
	r.record("lima_vz", vzOK, vzDetail)

	profile, hasProfile := table.Profiles[nativeProfileTableName]
	switch {
	case !hasProfile:
		r.record("digests", false, "native-arm64 row missing from profiles.env")
	case profile.ImageDigest != "" && profile.ImageURL != "":
		r.record("digests", true, "image digest "+profile.ImageDigest)
	case assetRoot == "" || profile.PinEnv == "":
		r.record("digests", false, "native-arm64 profile is missing a pinned image URL/digest")
	default:
		pinPath := filepath.Join(assetRoot, "lima", profile.PinEnv)
		pinData, err := os.ReadFile(pinPath)
		if err != nil {
			r.record("digests", false, "could not read native-arm64 pin file "+pinPath+": "+err.Error())
		} else if values, verr := validatePinMustKeepValues(string(pinData), pinPath); verr != nil {
			r.record("digests", false, "native-arm64 pin file "+pinPath+" is invalid: "+verr.Error())
		} else {
			r.record("digests", true, "image digest "+values["IOLBOX_IMAGE_DIGEST"]+" (read from "+pinPath+")")
		}
	}

	r.record("translator", nativeTranslatorName != "", "declared translator: "+nativeTranslatorName)

	minFreeKB := int64(minFreeDiskGiB) * 1024 * 1024
	if facts.FreeDiskErr != nil {
		r.record("resources", false, "could not read free disk: "+facts.FreeDiskErr.Error())
	} else {
		r.record("resources", facts.FreeDiskKB >= minFreeKB, fmt.Sprintf("%.2f GiB free (need >= %d GiB)", float64(facts.FreeDiskKB)/1048576, minFreeDiskGiB))
	}

	return r
}

// resolveProfileSelection is the single entry point macos_cli.go calls. It
// applies the explicit-flag > persisted > auto precedence, runs native
// preflight whenever native-arm64 is in play (forced or auto-under-test-
// policy), and fails closed: a FORCED native-arm64 selection that fails
// preflight is returned as an error (never silently downgraded), while an
// AUTO selection that fails native preflight falls back to rosetta-amd64
// with FallbackReason set.
func resolveProfileSelection(ctx context.Context, explicitFlag string, table profileTable, facts hostFacts, limactlPath, assetRoot string, configDir func() (string, error), testPreferNative bool) (profileSelectionResult, error) {
	if explicitFlag != "" && explicitFlag != selectionAuto {
		// Direct legacy profile-table name or one of the two logical modes.
		name, err := resolveProfileSelectionName(explicitFlag, table)
		if err != nil {
			return profileSelectionResult{}, err
		}
		if explicitFlag == selectionNativeARM64 {
			pf := nativePreflight(ctx, facts, table, limactlPath, assetRoot)
			if !pf.OK {
				return profileSelectionResult{}, fmt.Errorf("forced native-arm64 failed preflight, refusing to fall back (fail-closed): %s\n%s", pf.Reason, pf.String())
			}
		}
		if err := persistProfileChoice(explicitFlag, configDir); err != nil {
			// Persistence failure must not block an otherwise-successful
			// explicit selection; the launcher just won't remember it.
			logf("iolbox-launcher: could not persist profile choice: %v", err)
		}
		return profileSelectionResult{Requested: explicitFlag, Selected: explicitFlag, ProfileName: name, Source: "explicit-flag"}, nil
	}

	// No explicit flag (or explicit "auto"): try a persisted owner choice.
	persisted, err := readPersistedProfileChoice(configDir)
	if err != nil {
		logf("iolbox-launcher: could not read persisted profile choice: %v", err)
		persisted = ""
	}
	if persisted != "" {
		name, err := resolveProfileSelectionName(persisted, table)
		if err == nil {
			if persisted == selectionNativeARM64 {
				pf := nativePreflight(ctx, facts, table, limactlPath, assetRoot)
				if !pf.OK {
					rosettaName, rerr := resolveProfileSelectionName(selectionRosettaAMD64, table)
					if rerr != nil {
						return profileSelectionResult{}, fmt.Errorf("persisted native-arm64 failed preflight and rosetta-amd64 fallback is unavailable: %v (preflight: %s)", rerr, pf.Reason)
					}
					return profileSelectionResult{Requested: persisted, Selected: selectionRosettaAMD64, ProfileName: rosettaName, Source: "persisted-fallback-rosetta", FallbackReason: pf.Reason}, nil
				}
			}
			return profileSelectionResult{Requested: persisted, Selected: persisted, ProfileName: name, Source: "persisted"}, nil
		}
		logf("iolbox-launcher: persisted profile choice %q is no longer valid (%v); falling back to auto", persisted, err)
	}

	// Bare auto, no valid persisted choice. Until promotion, auto defaults
	// to rosetta-amd64 UNLESS the explicit test-only policy hook says to
	// prefer native — and even then, only when native preflight actually
	// passes; otherwise auto falls back to rosetta-amd64 with a reason.
	rosettaName, err := resolveProfileSelectionName(selectionRosettaAMD64, table)
	if err != nil {
		return profileSelectionResult{}, err
	}
	if !testPreferNative {
		return profileSelectionResult{Requested: selectionAuto, Selected: selectionRosettaAMD64, ProfileName: rosettaName, Source: "auto-default-rosetta"}, nil
	}
	nativeName, nerr := resolveProfileSelectionName(selectionNativeARM64, table)
	if nerr != nil {
		return profileSelectionResult{Requested: selectionAuto, Selected: selectionRosettaAMD64, ProfileName: rosettaName, Source: "auto-fallback-rosetta", FallbackReason: nerr.Error()}, nil
	}
	pf := nativePreflight(ctx, facts, table, limactlPath, assetRoot)
	if !pf.OK {
		return profileSelectionResult{Requested: selectionAuto, Selected: selectionRosettaAMD64, ProfileName: rosettaName, Source: "auto-fallback-rosetta", FallbackReason: pf.Reason}, nil
	}
	return profileSelectionResult{Requested: selectionAuto, Selected: selectionNativeARM64, ProfileName: nativeName, Source: "auto-native-test-policy"}, nil
}

// testPreferNativeFromEnv reads the explicit, test-only auto-native-
// preference hook. Split out for unit testing without mutating the real
// process environment from every test case.
func testPreferNativeFromEnv(getenv func(string) string) bool {
	if getenv == nil {
		getenv = os.Getenv
	}
	return getenv(testPreferNativeEnv) == "1"
}
