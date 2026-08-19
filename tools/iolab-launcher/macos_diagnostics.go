package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

var allowedCanaryVerdicts = map[string]bool{
	"PASS": true, "FAIL_AUXV": true, "FAIL_MISSING": true, "FAIL_NOEXEC": true, "FAIL_OTHER": true,
}

// macOS canary JSON is deliberately distinct from the structural attestation:
// this is the real /var/lib/iolbox/macos-canary.json shape and its verdict
// vocabulary. Do not read canary_verdict from the attestation as a live canary.
type macOSCanaryRecord struct {
	Schema       int    `json:"schema"`
	MacOSProduct string `json:"macos_product"`
	MacOSBuild   string `json:"macos_build"`
	LimaVersion  string `json:"lima_version"`
	Profile      string `json:"profile"`
	Kernel       string `json:"kernel"`
	Binfmt       string `json:"binfmt"`
	Verdict      string `json:"verdict"`
	Timestamp    string `json:"timestamp"`
	Version      string `json:"version"`
	Error        string `json:"error"`
}

func parseMacOSCanary(data []byte) (macOSCanaryRecord, error) {
	var record macOSCanaryRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return macOSCanaryRecord{}, fmt.Errorf("decode macOS canary JSON: %w", err)
	}
	if record.Schema != 1 {
		return macOSCanaryRecord{}, fmt.Errorf("macOS canary schema is %d, want 1", record.Schema)
	}
	if !allowedCanaryVerdicts[record.Verdict] {
		return macOSCanaryRecord{}, fmt.Errorf("unknown macOS canary verdict %q", record.Verdict)
	}
	return record, nil
}

func rosettaBinfmtEnabled(raw string) bool {
	normalized := strings.ToLower(raw)
	return strings.Contains(normalized, "enabled") && strings.Contains(raw, "/mnt/lima-rosetta/rosetta")
}

// rosettaPresenceString renders the raw /proc/sys/fs/binfmt_misc/rosetta read
// as a truthful tri-state string for status/diagnose (plan section 10 item
// 5's "rosetta_present"). Distinct from rosettaBinfmtEnabled, which requires
// the exact Lima interpreter path and is used to decide the execution mode;
// this is a plain presence/absence report of the binfmt entry itself.
func rosettaPresenceString(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch {
	// Checked BEFORE the generic "unavailable" fallback: unavailableValue()
	// wraps a failed `cat` as "unavailable (cat: ... No such file or
	// directory)", and on a genuine native-arm64 guest that specific
	// failure IS the truthful answer (the file plainly does not exist,
	// i.e. Rosetta is absent) — not an unknown result. Found on real
	// hardware (2026-08-19): the unqualified "unavailable" prefix check
	// used to run first and reported "unknown (unavailable (cat: ...No
	// such file or directory)))" for a perfectly healthy, correctly
	// Rosetta-less native-arm64 guest.
	case strings.Contains(normalized, "no such file") || strings.Contains(normalized, "cannot open"):
		return "false"
	case strings.HasPrefix(normalized, "unavailable"):
		return "unknown (" + raw + ")"
	case strings.Contains(normalized, "enabled"):
		return "true"
	case strings.Contains(normalized, "disabled"):
		return "true (registered but disabled)"
	case raw == "":
		return "false"
	default:
		return "unknown (" + raw + ")"
	}
}

type darwinDiagnostics struct {
	GuestArch             string
	GuestKernel           string
	Execution             string
	CanaryVerdict         string
	CanarySource          string
	Service               string
	HTTP                  string
	Hello                 string
	CapabilityPolicy      string
	RosettaBinfmt         string
	StructuralAttestation string
	EffectiveDropIn       string
	// Phase 4: profile-selection and translator truthfulness (plan section
	// 10 item 5). Requested/Selected/Source/FallbackReason mirror
	// profileSelectionResult; Backend/Translator/SupervisorArch/
	// RosettaPresent report what the resolved profile and live guest probe
	// actually are, independent of what was requested.
	RequestedProfile string
	SelectedProfile  string
	ProfileSource    string
	FallbackReason   string
	Backend          string
	Translator       string
	SupervisorArch   string
	RosettaPresent   string
}

type diagnosticsOptions struct {
	GUIPort int
	// hello is injectable for tests and keeps the Lima-response test independent
	// of a real WebSocket listener. The CLI supplies a live closure.
	hello func() (helloResult, error)
	// Selection is the already-resolved profileSelectionResult (empty zero
	// value is fine — callers that never wired the Phase 4 selection layer,
	// e.g. existing tests, simply get empty Requested/Selected/Source
	// fields instead of a panic).
	Selection profileSelectionResult
}

func probeGuest(ctx context.Context, l *limaClient, machine string, args ...string) (string, error) {
	out, err := l.shell(ctx, machine, args...)
	return strings.TrimSpace(string(out)), err
}

func unavailableValue(out []byte, err error) string {
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	message := strings.TrimSpace(string(out))
	if message == "" {
		message = err.Error()
	}
	return "unavailable (" + message + ")"
}

func collectDarwinDiagnostics(ctx context.Context, l *limaClient, machine, state string, p macOSProfile, facts hostFacts, info limaInfo, opts diagnosticsOptions) darwinDiagnostics {
	d := darwinDiagnostics{}
	d.RequestedProfile = opts.Selection.Requested
	d.SelectedProfile = opts.Selection.Selected
	d.ProfileSource = opts.Selection.Source
	d.FallbackReason = opts.Selection.FallbackReason
	d.Backend = "lima-vz"
	isNativeProfile := p.Name == nativeProfileTableName
	if isNativeProfile {
		d.Translator = nativeTranslatorName
	} else {
		d.Translator = "rosetta"
	}
	attested, attestationErr := readAttestationForMachine(machine)
	if attestationErr == nil {
		d.StructuralAttestation = "last attested canary_verdict=" + attested.CanaryVerdict
	} else {
		d.StructuralAttestation = "last attested unavailable (" + attestationErr.Error() + ")"
	}
	if !strings.EqualFold(state, "running") {
		d.GuestArch = "unavailable (machine is not running)"
		d.GuestKernel = "unavailable (machine is not running)"
		d.Execution = "unavailable (machine is not running)"
		d.CanaryVerdict = d.StructuralAttestation
		d.CanarySource = "last attested"
		d.Service = "unavailable (machine is not running)"
		d.HTTP = "unavailable (machine is not running)"
		d.Hello = "unavailable (machine is not running)"
		d.CapabilityPolicy = "unavailable (machine is not running; last attested only)"
		d.RosettaBinfmt = "unavailable (machine is not running)"
		d.RosettaPresent = "unavailable (machine is not running)"
		d.SupervisorArch = "unavailable (machine is not running)"
		d.EffectiveDropIn = "unavailable (machine is not running; last attested only)"
		return d
	}

	archOut, archErr := probeGuest(ctx, l, machine, "uname", "-m")
	kernelOut, kernelErr := probeGuest(ctx, l, machine, "uname", "-r")
	binfmtOut, binfmtErr := probeGuest(ctx, l, machine, "sudo", "-n", "cat", "/proc/sys/fs/binfmt_misc/rosetta")
	canaryOut, canaryErr := probeGuest(ctx, l, machine, "sudo", "-n", "cat", "/var/lib/iolbox/macos-canary.json")
	dropInOut, dropInErr := probeGuest(ctx, l, machine, "sudo", "-n", "cat", expectedCanaryDropIn)
	serviceOut, serviceErr := probeGuest(ctx, l, machine, "sudo", "-n", "systemctl", "is-active", "iolbox-supervisor.service")
	httpOut, httpErr := probeGuest(ctx, l, machine, "sudo", "-n", "curl", "--silent", "--show-error", "--output", "/dev/null", "--write-out", "%{http_code}", fmt.Sprintf("http://127.0.0.1:%d/", darwinGUIGuestPort))

	d.GuestArch = unavailableValue([]byte(archOut), archErr)
	d.GuestKernel = unavailableValue([]byte(kernelOut), kernelErr)
	d.RosettaBinfmt = unavailableValue([]byte(binfmtOut), binfmtErr)
	if serviceErr != nil {
		d.Service = "FAIL (" + unavailableValue([]byte(serviceOut), serviceErr) + ")"
	} else if strings.TrimSpace(serviceOut) == "active" {
		d.Service = "PASS (active)"
	} else {
		d.Service = "FAIL (" + strings.TrimSpace(serviceOut) + ")"
	}
	if httpErr != nil {
		d.HTTP = "FAIL (" + unavailableValue([]byte(httpOut), httpErr) + ")"
	} else if strings.HasPrefix(strings.TrimSpace(httpOut), "2") {
		d.HTTP = "PASS (HTTP " + strings.TrimSpace(httpOut) + ")"
	} else {
		d.HTTP = "FAIL (HTTP " + strings.TrimSpace(httpOut) + ")"
	}
	d.EffectiveDropIn = unavailableValue([]byte(dropInOut), dropInErr)

	var record macOSCanaryRecord
	if canaryErr != nil {
		d.CanaryVerdict = unavailableValue([]byte(canaryOut), canaryErr) + " (live read failed)"
		d.CanarySource = "live read failed"
	} else if parsed, err := parseMacOSCanary([]byte(canaryOut)); err != nil {
		d.CanaryVerdict = "invalid live record (" + err.Error() + ")"
		d.CanarySource = "live record invalid"
	} else {
		record = parsed
		d.CanaryVerdict = parsed.Verdict
		if parsed.Timestamp != "" {
			d.CanaryVerdict += " (live record " + parsed.Timestamp + ")"
		}
		d.CanarySource = "live guest JSON"
	}

	// Run the canary now as the independent execution probe. Its JSON record is
	// parsed separately above, so a stale/malformed record cannot masquerade as
	// a passing live loader.
	d.RosettaPresent = rosettaPresenceString(d.RosettaBinfmt)
	env := guestEnvironment(p, facts, info.Version, machine, "diagnostic.tar.gz", lifecycleConfig{GUIPort: opts.GUIPort})
	liveOut, liveErr := l.shell(ctx, machine, guestStepArgs(p.canaryStep(), env, "--quiet")...)
	loaderPassed := liveErr == nil && strings.TrimSpace(string(liveOut)) == "PASS" && record.Verdict == "PASS"
	if liveErr != nil {
		d.CanaryVerdict += "; live execution " + unavailableValue(liveOut, liveErr)
	}
	// Truthfulness note: rosetta-amd64 execution REQUIRES the Lima Rosetta
	// binfmt interpreter to be enabled (the amd64 payload is only runnable
	// translated through it); native-arm64 execution REQUIRES it to be
	// absent (a native profile whose guest still has host Rosetta wired in
	// is not proven to be running untranslated, even if its own canary
	// happens to pass).
	switch {
	case isNativeProfile && d.GuestArch == "aarch64" && !rosettaBinfmtEnabled(d.RosettaBinfmt) && loaderPassed:
		d.Execution = "native-arm64"
	case !isNativeProfile && d.GuestArch == "aarch64" && rosettaBinfmtEnabled(d.RosettaBinfmt) && loaderPassed:
		d.Execution = "rosetta-amd64"
	default:
		d.Execution = "not qualified (guest_arch=" + d.GuestArch + ", live_canary=" + d.CanaryVerdict + ")"
	}

	if opts.hello == nil {
		d.Hello = "unavailable (not probed)"
		d.CapabilityPolicy = "unavailable (hello not probed)"
		d.SupervisorArch = "unavailable (hello not probed)"
	} else if hello, err := opts.hello(); err != nil {
		d.Hello = "unavailable (" + err.Error() + ")"
		d.CapabilityPolicy = "FAIL (hello unavailable; capability drift not ruled out)"
		d.SupervisorArch = "unavailable (" + err.Error() + ")"
	} else {
		d.Hello = fmt.Sprintf("supervisor=%s runtime=%s arch=%s features=%v egress=%s", hello.Supervisor, hello.Runtime, hello.Arch, hello.Features, hello.Egress)
		d.SupervisorArch = hello.Arch
		if strings.Contains(strings.Join(hello.Features, ","), "i386") && strings.Contains(d.EffectiveDropIn, "IOLBOX_DISABLE_I386=1") {
			d.CapabilityPolicy = "FAIL (drop-in disables i386 but hello still advertises it; drift)"
		} else if !strings.Contains(strings.Join(hello.Features, ","), "i386") && strings.Contains(d.EffectiveDropIn, "IOLBOX_DISABLE_I386=1") {
			d.CapabilityPolicy = "PASS (drop-in and hello agree)"
		} else {
			d.CapabilityPolicy = "FAIL (effective drop-in/hello policy mismatch or fail-open)"
		}
	}
	return d
}

func readAttestationForMachine(machine string) (structuralAttestation, error) {
	path, err := hostAttestationPath(machine)
	if err != nil {
		return structuralAttestation{}, err
	}
	return readAttestation(path)
}

func singleLine(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\r", "\\r"), "\n", "\\n")
}

func printDiagnosticSummary(w io.Writer, d darwinDiagnostics) {
	fmt.Fprintf(w, "requested_profile=%s\nselected_profile=%s\nprofile_source=%s\nfallback_reason=%s\nbackend=%s\ntranslator=%s\nguest_arch=%s\nsupervisor_arch=%s\nexecution=%s\nguest_kernel=%s\ncanary_verdict=%s\nservice=%s\nhttp=%s\nhello=%s\ncapability_policy=%s\nrosetta_binfmt=%s\nrosetta_present=%s\nstructural_attestation=%s\neffective_drop_in=%s\n",
		singleLine(d.RequestedProfile), singleLine(d.SelectedProfile), singleLine(d.ProfileSource), singleLine(d.FallbackReason),
		singleLine(d.Backend), singleLine(d.Translator),
		singleLine(d.GuestArch), singleLine(d.SupervisorArch), singleLine(d.Execution), singleLine(d.GuestKernel),
		singleLine(d.CanaryVerdict), singleLine(d.Service), singleLine(d.HTTP), singleLine(d.Hello), singleLine(d.CapabilityPolicy),
		singleLine(d.RosettaBinfmt), singleLine(d.RosettaPresent), singleLine(d.StructuralAttestation), singleLine(d.EffectiveDropIn))
}
