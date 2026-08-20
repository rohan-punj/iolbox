package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type hostFacts struct {
	System      string
	Arch        string
	Product     string
	Build       string
	FreeDiskKB  int64
	FreeDiskErr error
}

type lifecycleConfig struct {
	Bind        string
	GUIPort     int
	Ports       darwinPortContract
	BootTimeout time.Duration
	Upgrade     bool
	Sync        *darwinSyncConfig
}

func runHostFact(ctx context.Context, name string, args ...string) (string, error) {
	out, err := (execRunner{}).Run(ctx, name, args...)
	value := strings.TrimSpace(string(out))
	if err != nil {
		return value, err
	}
	return value, nil
}

func collectHostFacts(ctx context.Context) hostFacts {
	facts := hostFacts{}
	facts.System, _ = runHostFact(ctx, "uname", "-s")
	facts.Arch, _ = runHostFact(ctx, "uname", "-m")
	facts.Product, _ = runHostFact(ctx, "sw_vers", "-productVersion")
	facts.Build, _ = runHostFact(ctx, "sw_vers", "-buildVersion")
	home, err := os.UserHomeDir()
	if err != nil {
		facts.FreeDiskErr = err
		return facts
	}
	out, err := runHostFact(ctx, "df", "-Pk", home)
	if err != nil {
		facts.FreeDiskErr = err
		return facts
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		facts.FreeDiskErr = errors.New("df output has no data row")
		return facts
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		facts.FreeDiskErr = errors.New("df output has too few fields")
		return facts
	}
	facts.FreeDiskKB, err = strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		facts.FreeDiskErr = err
	}
	return facts
}

func hostMACOSString(facts hostFacts) string {
	if facts.Product == "" || facts.Build == "" {
		return "unknown"
	}
	return facts.Product + " (" + facts.Build + ")"
}

func homePath(parts ...string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{home}, parts...)...), nil
}

// limaHomeDir resolves Lima's own data directory the same way limactl
// itself does: $LIMA_HOME when set (any isolated Phase 4 LIMA_HOME
// included), else ~/.lima. Every path this launcher builds to read a
// machine's OWN Lima-managed files (its lima.yaml, its hostagent log) must
// go through this, never a hardcoded ".lima".
func limaHomeDir() (string, error) {
	if dir := os.Getenv("LIMA_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".lima"), nil
}

// limaHomePath joins limaHomeDir() with parts. Found on real hardware
// (2026-08-19, physical Mac): every caller of this used to build
// homePath(".lima", machine, ...) instead, which always resolved to the
// real default ~/.lima regardless of $LIMA_HOME. Running under Phase 4's
// own required isolated LIMA_HOME, a recovery attempt against an existing
// (half-created) machine failed with "could not inspect Lima port contract
// for running machine ...: open /Users/.../.lima/iolbox-native-arm64/
// lima.yaml: no such file or directory" -- the real file was one directory
// tree over, at $LIMA_HOME/iolbox-native-arm64/lima.yaml. M1-M6 never hit
// this because they always ran against the real default ~/.lima.
func limaHomePath(parts ...string) (string, error) {
	dir, err := limaHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{dir}, parts...)...), nil
}

func hostAttestationPath(machine string) (string, error) {
	return homePath(".iolbox", "macos", machine+"-structural-gate.json")
}

func canaryStatePath(machine string) (string, error) {
	return homePath("Library", "Application Support", "iolbox", "lima-canary-"+machine+".txt")
}

type structuralAttestation struct {
	Schema        int    `json:"schema"`
	Profile       string `json:"profile"`
	MacOSProduct  string `json:"macos_product"`
	MacOSBuild    string `json:"macos_build"`
	LimaVersion   string `json:"lima_version"`
	DropIn        string `json:"drop_in"`
	CanaryVerdict string `json:"canary_verdict"`
	Kernel        string `json:"kernel"`
}

const expectedCanaryDropIn = "/etc/systemd/system/iolbox-supervisor.service.d/10-iolbox-macos-canary.conf"

func readAttestation(path string) (structuralAttestation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return structuralAttestation{}, err
	}
	var att structuralAttestation
	if err := json.Unmarshal(data, &att); err != nil {
		return structuralAttestation{}, err
	}
	return att, nil
}

func validateAttestation(path string, att structuralAttestation, p macOSProfile, facts hostFacts, limaVersion string) error {
	if att.Schema != 1 {
		return fmt.Errorf("attestation schema is %d, want 1", att.Schema)
	}
	if att.Profile != p.Name || att.MacOSProduct != facts.Product || att.MacOSBuild != facts.Build || att.LimaVersion != limaVersion {
		return fmt.Errorf("attestation host/profile facts do not match the current selection")
	}
	if att.CanaryVerdict != "PASS" {
		return fmt.Errorf("attestation canary verdict is %q", att.CanaryVerdict)
	}
	if att.DropIn != expectedCanaryDropIn {
		return fmt.Errorf("attestation drop-in is %q, want %q", att.DropIn, expectedCanaryDropIn)
	}
	return nil
}

func requireAttestation(path string, p macOSProfile, facts hostFacts, limaVersion string) error {
	att, err := readAttestation(path)
	if err != nil {
		return err
	}
	return validateAttestation(path, att, p, facts, limaVersion)
}

func recordCanary(machine string, facts hostFacts, limaVersion, result string, code int) error {
	path, err := canaryStatePath(machine)
	if err != nil {
		return err
	}
	data := fmt.Sprintf("result=%s\nexit=%d\nhost_macos=%s\nhost_lima=%s\nrecorded_utc=%s\n", result, code, hostMACOSString(facts), limaVersion, time.Now().UTC().Format(time.RFC3339))
	return atomicWriteFile(path, []byte(data), 0o644)
}

func readCanaryState(machine string) string {
	path, err := canaryStatePath(machine)
	if err != nil {
		return "unavailable (" + err.Error() + ")"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "unknown (no host-side canary result recorded)"
	}
	return strings.TrimSpace(string(data))
}

func guestEnvironment(p macOSProfile, facts hostFacts, limaVersion, machine, payloadBase string, config lifecycleConfig) map[string]string {
	bind := config.Bind
	if bind == "" {
		bind = "all"
	}
	guiPort := config.GUIPort
	if guiPort == 0 {
		guiPort = 4001
	}
	return map[string]string{
		"IOLBOX_HOST_MACOS":         hostMACOSString(facts),
		"IOLBOX_HOST_MACOS_PRODUCT": facts.Product,
		"IOLBOX_HOST_MACOS_BUILD":   facts.Build,
		"IOLBOX_HOST_LIMA":          limaVersion,
		"IOLBOX_MACHINE":            machine,
		"IOLBOX_PROFILE":            p.Name,
		"IOLBOX_PROFILE_STATUS":     p.Role,
		"IOLBOX_KERNEL_SERIES":      kernelRuntimeSeries(p.KernelSeries),
		"IOLBOX_EXPECTED_UNAME_R":   p.ExpectedUnameR,
		"IOLBOX_PROVISION_DIR":      "/opt/iolbox-provision",
		"IOLBOX_PAYLOAD_TARBALL":    "/opt/iolbox-provision/payload/" + payloadBase,
		"IOLBOX_BIND":               bind,
		// This is consumed inside the guest by the provisioning and verify
		// scripts.  The host-side forwarding port is allowed to differ when
		// another Lima machine owns 4001.
		"IOLBOX_GUI_PORT": strconv.Itoa(darwinGUIGuestPort),
	}
}

func kernelRuntimeSeries(series string) string {
	parts := strings.Split(series, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return series
}

func syncGuestAttestation(ctx context.Context, l *limaClient, machine string, p macOSProfile, facts hostFacts, limaVersion string) error {
	out, err := l.shell(ctx, machine, "sudo", "-n", "cat", "/var/lib/iolbox/macos-structural-gate.json")
	if err != nil {
		return codedError(exitVerify, "retrieve guest structural attestation: %v", err)
	}
	var att structuralAttestation
	if err := json.Unmarshal(out, &att); err != nil {
		return codedError(exitVerify, "decode guest structural attestation: %v", err)
	}
	if err := validateAttestation("guest", att, p, facts, limaVersion); err != nil {
		return codedError(exitVerify, "invalid guest structural attestation: %v", err)
	}
	path, err := hostAttestationPath(machine)
	if err != nil {
		return codedError(exitVerify, "resolve host attestation path: %v", err)
	}
	if err := atomicWriteFile(path, out, 0o600); err != nil {
		return codedError(exitVerify, "install host structural attestation: %v", err)
	}
	return nil
}

func guestValue(ctx context.Context, l *limaClient, machine string, args ...string) string {
	out, err := l.shell(ctx, machine, args...)
	if err != nil {
		return "unavailable (" + strings.TrimSpace(string(out)) + ")"
	}
	return strings.TrimSpace(string(out))
}

func hostAgentWarningText(machine string) (string, bool) {
	path, err := limaHomePath(machine, "ha.stderr.log")
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var matches []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "Unable to configure Rosetta") || strings.Contains(line, "unsupported build target macOS version") {
			matches = append(matches, line)
		}
	}
	return strings.Join(matches, "\n"), len(matches) > 0
}

func limaClientFor(ctx context.Context, path string, runner commandRunner) (*limaClient, limaInfo, error) {
	if path == "" {
		return nil, limaInfo{}, codedError(exitPreflight, "Lima was not found")
	}
	info := collectLimaInfo(ctx, path, runner)
	version := info.Version
	if version == "" {
		// D11: a found executable is never represented as unknown in the guest
		// environment. Preserve the raw diagnostic separately.
		version = "unparsed"
		info.Version = version
	}
	return &limaClient{info: info, runner: runnerOrExec(runner)}, info, nil
}

func runnerOrExec(runner commandRunner) commandRunner {
	if runner == nil {
		return execRunner{}
	}
	return runner
}

func runGuestSequence(ctx context.Context, l *limaClient, machine string, p macOSProfile, facts hostFacts, limaVersion, payloadBase string, config lifecycleConfig) error {
	env := guestEnvironment(p, facts, limaVersion, machine, payloadBase, config)
	canaryStep := p.canaryStep()
	steps := []string{p.MultiarchStep, p.KernelHoldStep, canaryStep, p.installStep(), p.verifyStep()}
	for _, step := range steps {
		err := guestStep(ctx, l, machine, step, env)
		if step == canaryStep {
			if err != nil {
				_ = recordCanary(machine, facts, limaVersion, "fail", exitCodeFor(err))
				return err
			}
			_ = recordCanary(machine, facts, limaVersion, "pass", 0)
		}
		if err != nil {
			return err
		}
	}
	return syncGuestAttestation(ctx, l, machine, p, facts, limaVersion)
}

func exitCodeFor(err error) int {
	var coded *launcherError
	if errors.As(err, &coded) {
		return coded.code
	}
	return childExitCode(err)
}

func runProvision(ctx context.Context, l *limaClient, machine string, p macOSProfile, facts hostFacts, payloadPath string, config lifecycleConfig) error {
	ports := config.Ports
	if ports.GUIPort == 0 {
		var err error
		ports, err = newDarwinPortContract(config.GUIPort)
		if err != nil {
			return codedError(exitUsage, "%v", err)
		}
	}
	templatePath := ""
	var err error
	if _, statErr := os.Stat(p.TemplatePath); statErr != nil {
		return codedError(exitUsage, "profile template is missing: %v", statErr)
	}
	// Render and validate before querying or mutating Lima, matching the
	// documented start sequence. Existing machines do not consume the file,
	// but rendering still proves the shipped profile is internally coherent.
	templatePath, err = writeRenderedTemplateForPort("", p, ports.GUIPort)
	if err != nil {
		return codedError(exitUsage, "render Lima template: %v", err)
	}
	defer os.Remove(templatePath)

	machines, err := l.list(ctx)
	if err != nil {
		return codedError(exitPreflight, "%v", err)
	}
	state, exists := findMachine(machines, machine)
	if config.Upgrade && !exists {
		return codedError(exitPreflight, "upgrade requires existing machine %q", machine)
	}
	attestationPath, err := hostAttestationPath(machine)
	if err != nil {
		return codedError(exitPreflight, "resolve host attestation path: %v", err)
	}
	validAttestation := func() error {
		return requireAttestation(attestationPath, p, facts, l.info.Version)
	}
	created, err := ensureMachineWithPorts(ctx, l, machine, state, templatePath, attestationPath, validAttestation, &ports)
	if err != nil {
		return err
	}
	if created {
		// The returned state is now live; the template is removed by the defer
		// above after the create command has consumed it.
	}
	if warning, ok := hostAgentWarningText(machine); ok {
		fmt.Fprintf(os.Stderr, "WARNING: Lima hostagent Rosetta warning for %s:\n%s\n", machine, warning)
		fmt.Fprintln(os.Stderr, "Remediation: brew reinstall lima (brew upgrade may be a no-op when Lima is already current).")
	}
	payloadBase := filepath.Base(payloadPath)
	if err := stageFiles(ctx, l, machine, p, payloadPath, payloadBase); err != nil {
		return err
	}
	if err := runGuestSequence(ctx, l, machine, p, facts, l.info.Version, payloadBase, config); err != nil {
		return err
	}
	guiPort := ports.GUIPort
	if _, err := waitHTTPReady(ctx, nil, fmt.Sprintf("http://127.0.0.1:%d/", guiPort), config.BootTimeout); err != nil {
		return codedError(exitVerify, "GUI readiness failed: %v", err)
	}
	if err := verifyDarwinHostContract(ports, 2*time.Second); err != nil {
		return err
	}
	control, err := dialControlWS(fmt.Sprintf("127.0.0.1:%d", guiPort))
	if err != nil {
		return codedError(exitVerify, "control-plane connection failed: %v", err)
	}
	defer control.Close()
	if _, err := control.hello(); err != nil {
		return codedError(exitVerify, "control-plane hello failed: %v", err)
	}
	if config.Sync != nil {
		if err := syncDarwinStartup(control, fmt.Sprintf("http://127.0.0.1:%d", guiPort), *config.Sync); err != nil {
			return codedError(exitVerify, "%v", err)
		}
	}
	return nil
}

type darwinStopControlFactory func() (controlClient, func(), error)

// runDarwinStop performs the mandatory host sync while the guest is still
// running. A sync failure returns before stopMachine, leaving the Lima
// instance available for recovery and retry.
func runDarwinStop(ctx context.Context, l *limaClient, machine, state string, config darwinSyncConfig, timeout time.Duration, connect darwinStopControlFactory) error {
	if strings.EqualFold(state, "running") && !config.NoSync {
		if connect == nil {
			return codedError(exitVerify, "control-plane connection for stop sync is unavailable")
		}
		control, closeControl, err := connect()
		if err != nil {
			return codedError(exitVerify, "control-plane connection for stop sync failed: %v", err)
		}
		syncErr := syncDarwinBeforeStop(control, config)
		if closeControl != nil {
			closeControl()
		}
		if syncErr != nil {
			return codedError(exitVerify, "%v", syncErr)
		}
	}
	return stopMachine(ctx, l, machine, state, timeout)
}
