package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
//  3. otherwise, "auto" applies -- which, since the owner's promotion ruling
//     (docs/macos-m7-phase6-handoff.md, "Owner promotion ruling"; plan
//     section 13 PROMOTE), now runs the same non-mutating nativePreflight
//     used for forced/persisted native selections and PREFERS native-arm64
//     whenever that preflight passes. When it does not pass, auto falls back
//     to rosetta-amd64 with a FallbackReason -- the explicit Rosetta
//     fallback the PROMOTE clause requires be retained. There is no
//     test-only preference hook any more; auto's behavior is the same in
//     tests and in production.
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
	// persisted-fallback-rosetta / auto-native / auto-fallback-rosetta /
	// auto-existing-rosetta-machine.
	//
	// auto-existing-rosetta-machine is applied AFTER resolveProfileSelection
	// returns, by adjustAutoSelectionForExistingInstall (see below) — it is
	// the only Source this file's resolver cannot produce on its own,
	// because it depends on host Lima inventory the resolver does not read.
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

// resolveProfileSelection is the first of two steps macos_cli.go calls: it
// decides the selection from flags, persisted state, and host facts. When it
// returns Source "auto-native", macos_cli.go then calls
// adjustAutoSelectionForExistingInstall to finalize that decision against
// the host's actual Lima inventory (it is not the "single entry point" it
// once was — auto's native preference is not safe to apply without knowing
// whether the user already has an install).
//
// It applies the explicit-flag > persisted > auto precedence, runs native
// preflight whenever native-arm64 is in play (forced, persisted, or auto),
// and fails closed: a FORCED native-arm64 selection that fails preflight is
// returned as an error (never silently downgraded), while a PERSISTED or
// AUTO selection that fails native preflight falls back to rosetta-amd64
// with FallbackReason set.
func resolveProfileSelection(ctx context.Context, explicitFlag string, table profileTable, facts hostFacts, limactlPath, assetRoot string, configDir func() (string, error)) (profileSelectionResult, error) {
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

	// Bare auto, no valid persisted choice. Post-promotion, auto PREFERS
	// native-arm64: run the same non-mutating preflight the forced and
	// persisted native paths run, and use native whenever it passes.
	// Anything short of a clean pass (native row absent, or any failing
	// check) falls back to rosetta-amd64 with a FallbackReason -- auto never
	// errors out, it degrades to the retained Rosetta path.
	rosettaName, err := resolveProfileSelectionName(selectionRosettaAMD64, table)
	if err != nil {
		return profileSelectionResult{}, err
	}
	nativeName, nerr := resolveProfileSelectionName(selectionNativeARM64, table)
	if nerr != nil {
		return profileSelectionResult{Requested: selectionAuto, Selected: selectionRosettaAMD64, ProfileName: rosettaName, Source: "auto-fallback-rosetta", FallbackReason: nerr.Error()}, nil
	}
	pf := nativePreflight(ctx, facts, table, limactlPath, assetRoot)
	if !pf.OK {
		return profileSelectionResult{Requested: selectionAuto, Selected: selectionRosettaAMD64, ProfileName: rosettaName, Source: "auto-fallback-rosetta", FallbackReason: pf.Reason}, nil
	}
	return profileSelectionResult{Requested: selectionAuto, Selected: selectionNativeARM64, ProfileName: nativeName, Source: "auto-native"}, nil
}

// sourceAutoExistingRosettaMachine is the Source value for the continuity
// fallback below. Distinct from "auto-fallback-rosetta" on purpose: that one
// means "native was not usable on this host", this one means "native was
// usable but the user already has an install we must not abandon".
// status/diagnose must be able to tell those apart.
const sourceAutoExistingRosettaMachine = "auto-existing-rosetta-machine"

// machineNameForProfileRow mirrors macos_cli.go's derivation
// (`opts.Machine = "iolbox-" + profile.Name`). The two must change together;
// this fix exists precisely because that derivation makes the resolved
// profile decide which VM the launcher talks to.
func machineNameForProfileRow(row string) string {
	return "iolbox-" + row
}

// existingNonNativeProfileRow reports the profile-table row whose derived
// Lima machine already exists on this host, preferring the table's DEFAULT
// row.
//
// The remaining rows are considered in sorted name order because
// profileTable.Profiles is an unordered map: without the sort, a host with
// two equally-eligible non-default rows would pick a different one from run
// to run, which would be a genuinely nondeterministic profile selection.
func existingNonNativeProfileRow(machines []machineInfo, table profileTable) (string, bool) {
	exists := func(row string) bool {
		_, ok := findMachine(machines, machineNameForProfileRow(row))
		return ok
	}
	if table.Default != "" && table.Default != nativeProfileTableName && exists(table.Default) {
		return table.Default, true
	}
	rows := make([]string, 0, len(table.Profiles))
	for row := range table.Profiles {
		if row == nativeProfileTableName || row == table.Default {
			continue
		}
		rows = append(rows, row)
	}
	sort.Strings(rows)
	for _, row := range rows {
		if exists(row) {
			return row, true
		}
	}
	return "", false
}

// attestedProfileRowFunc reports the profile row a machine was actually
// provisioned as, according to its host structural-gate attestation, and
// whether that is known. Injected so the decision function stays pure and
// testable on a host with no Lima and no attestation files.
type attestedProfileRowFunc func(machine string) (string, bool)

// attestedProfileRowFor returns the production attestedProfileRowFunc: it
// reads <machine>-structural-gate.json and reports its recorded profile,
// but only when that profile is a row this asset root actually knows.
//
// This matters because a MACHINE NAME IS NOT PROOF OF ITS PROFILE. The name
// is merely the default derivation; a user can pair any --machine with any
// --profile. The attestation is written by the guest install step and
// records what the machine was really provisioned as, so it outranks the
// name whenever both are available.
func attestedProfileRowFor(table profileTable) attestedProfileRowFunc {
	return func(machine string) (string, bool) {
		path, err := hostAttestationPath(machine)
		if err != nil {
			return "", false
		}
		att, err := readAttestation(path)
		if err != nil {
			return "", false
		}
		if att.Profile == "" {
			return "", false
		}
		if _, known := table.Profiles[att.Profile]; !known {
			return "", false
		}
		return att.Profile, true
	}
}

// adjustAutoSelectionForExistingInstall keeps a pre-existing install on its
// own profile when `auto` would otherwise migrate it to native-arm64.
//
// Why this exists: the Lima machine name is derived from the resolved
// profile, so letting `auto` flip an existing install to native-arm64 points
// the launcher at a DIFFERENT machine that does not exist. `upgrade` then
// hard-fails ("upgrade requires existing machine ..."), and `start` quietly
// creates a second VM while the user's real one is orphaned along with its
// guest-local state (installed payload, host identity/attestation, image
// cache). Only EXPLICIT selections are ever persisted — and even those only
// on a best-effort basis, since persistence failure is deliberately
// nonfatal — so a user who has only ever run `./iolbox start` has no
// persisted choice to protect them. That is the default population, not an
// edge case.
//
// "auto" here means bare auto AND an explicit --profile auto /
// IOLBOX_PROFILE=auto, because the resolver deliberately treats those
// identically. All three reach Source "auto-native".
//
// Deliberately narrow. It fires only for Source == "auto-native", leaving
// untouched: explicit non-auto selections (the fail-closed migration
// opt-in), persisted selections, auto runs that already fell back for
// another reason, hosts that already have a native machine, and — most
// importantly — fresh hosts with no iolbox install at all, which still get
// the promoted native default. This does not revert that promotion; it
// scopes it to installs that have nothing to lose.
//
// machineOverride is --machine/IOLBOX_MACHINE. When set, no name is derived,
// so the derived-name reasoning does not apply — but the override target is
// NOT simply exempt: if it already exists and its attestation says it is a
// non-native install, provisioning it as native would stage native guest
// scripts into a Rosetta VM (a running machine skips attestation validation
// entirely in ensureMachineWithPorts, so nothing downstream would catch it).
// In that case we keep its attested profile.
func adjustAutoSelectionForExistingInstall(sel profileSelectionResult, table profileTable, machines []machineInfo, machineOverride string, attested attestedProfileRowFunc) profileSelectionResult {
	if sel.Source != "auto-native" {
		return sel
	}
	if attested == nil {
		attested = func(string) (string, bool) { return "", false }
	}

	keep := func(row, machine, why string) profileSelectionResult {
		return profileSelectionResult{
			Requested:   sel.Requested,
			Selected:    selectionRosettaAMD64,
			ProfileName: row,
			Source:      sourceAutoExistingRosettaMachine,
			FallbackReason: fmt.Sprintf(
				"existing Lima machine %q %s; keeping this install on profile %q (run with --profile native-arm64 to migrate to a new native guest)",
				machine, why, row),
		}
	}

	if machineOverride != "" {
		if _, exists := findMachine(machines, machineOverride); !exists {
			// Nothing to preserve: the override names a machine that will be
			// created fresh. Every M4-M7 hardware harness lands here, because
			// they pass disposable per-run machine names.
			return sel
		}
		row, known := attested(machineOverride)
		if !known || row == nativeProfileTableName {
			// Either provenance is unknown (do not guess from a name the
			// user chose) or it is genuinely native already.
			return sel
		}
		return keep(row, machineOverride, "is attested as a non-native install")
	}

	if _, migrated := findMachine(machines, machineNameForProfileRow(nativeProfileTableName)); migrated {
		return sel
	}
	row, found := existingNonNativeProfileRow(machines, table)
	if !found {
		return sel
	}
	machine := machineNameForProfileRow(row)
	// The name says non-native; if the attestation disagrees, believe the
	// attestation and let native proceed.
	if attestedRow, known := attested(machine); known {
		if attestedRow == nativeProfileTableName {
			return sel
		}
		return keep(attestedRow, machine, "is attested as profile "+attestedRow)
	}
	return keep(row, machine, "predates native-arm64")
}

// finalizeAutoSelection is step two of profile resolution: it settles auto's
// native preference against the host's real Lima inventory. Split out of
// runDarwinCLI so the orchestration — not just the pure decision — is
// testable on a host with no Lima (runDarwinCLI itself refuses to run
// anywhere but Darwin/arm64).
//
// Fail-closed on inventory error for MUTATING commands. This listing and
// runProvision's own listing are separate calls, so a transient failure here
// followed by a success there would create exactly the second VM this check
// exists to prevent. status/diagnose mutate nothing, so they report the
// uncertainty and continue rather than denying the user their diagnostics.
func finalizeAutoSelection(ctx context.Context, sel profileSelectionResult, table profileTable, command, machineOverride string, list func(context.Context) ([]machineInfo, error)) (profileSelectionResult, error) {
	if sel.Source != "auto-native" {
		return sel, nil
	}
	machines, err := list(ctx)
	if err == nil {
		return adjustAutoSelectionForExistingInstall(sel, table, machines, machineOverride, attestedProfileRowFor(table)), nil
	}
	if command == "start" || command == "upgrade" {
		return profileSelectionResult{}, codedError(exitPreflight,
			"could not list Lima machines to check for an existing install: %v; refusing to provision native-arm64 without confirming no existing install would be abandoned (fail-closed). Re-run once limactl is reachable, or select a profile explicitly with --profile", err)
	}
	logf("iolbox-launcher: could not list Lima machines to check for an existing install: %v", err)
	return sel, nil
}

// listLimaMachines shells the read-only `limactl list`, in the same style as
// limaSupportsVZ above: it creates, starts, stops, and mutates nothing. The
// format string matches limaClient.list so both share parseMachineListing.
func listLimaMachines(ctx context.Context, limactlPath string) ([]machineInfo, error) {
	if limactlPath == "" {
		return nil, fmt.Errorf("no limactl executable resolved")
	}
	out, err := exec.CommandContext(ctx, limactlPath, "list", "--format", "{{.Name}}|{{.Status}}").Output()
	if err != nil {
		return nil, fmt.Errorf("limactl list: %w", err)
	}
	return parseMachineListing(string(out))
}
