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
}

type diagnosticsOptions struct {
	GUIPort int
	// hello is injectable for tests and keeps the Lima-response test independent
	// of a real WebSocket listener. The CLI supplies a live closure.
	hello func() (helloResult, error)
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
	env := guestEnvironment(p, facts, info.Version, machine, "diagnostic.tar.gz", lifecycleConfig{GUIPort: opts.GUIPort})
	liveOut, liveErr := l.shell(ctx, machine, guestStepArgs("30-canary.sh", env, "--quiet")...)
	loaderPassed := liveErr == nil && strings.TrimSpace(string(liveOut)) == "PASS" && record.Verdict == "PASS"
	if liveErr != nil {
		d.CanaryVerdict += "; live execution " + unavailableValue(liveOut, liveErr)
	}
	if d.GuestArch == "aarch64" && rosettaBinfmtEnabled(d.RosettaBinfmt) && loaderPassed {
		d.Execution = "rosetta-amd64"
	} else {
		d.Execution = "not qualified (guest_arch=" + d.GuestArch + ", live_canary=" + d.CanaryVerdict + ")"
	}

	if opts.hello == nil {
		d.Hello = "unavailable (not probed)"
		d.CapabilityPolicy = "unavailable (hello not probed)"
	} else if hello, err := opts.hello(); err != nil {
		d.Hello = "unavailable (" + err.Error() + ")"
		d.CapabilityPolicy = "FAIL (hello unavailable; capability drift not ruled out)"
	} else {
		d.Hello = fmt.Sprintf("supervisor=%s runtime=%s arch=%s features=%v egress=%s", hello.Supervisor, hello.Runtime, hello.Arch, hello.Features, hello.Egress)
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
	fmt.Fprintf(w, "guest_arch=%s\nexecution=%s\nguest_kernel=%s\ncanary_verdict=%s\nservice=%s\nhttp=%s\nhello=%s\ncapability_policy=%s\nrosetta_binfmt=%s\nstructural_attestation=%s\neffective_drop_in=%s\n", singleLine(d.GuestArch), singleLine(d.Execution), singleLine(d.GuestKernel), singleLine(d.CanaryVerdict), singleLine(d.Service), singleLine(d.HTTP), singleLine(d.Hello), singleLine(d.CapabilityPolicy), singleLine(d.RosettaBinfmt), singleLine(d.StructuralAttestation), singleLine(d.EffectiveDropIn))
}
