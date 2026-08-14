package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

type commandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, path string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	return cmd.CombinedOutput()
}

type limaInfo struct {
	Path       string
	RawVersion string
	Version    string
	VersionErr error
}

type limaClient struct {
	info   limaInfo
	runner commandRunner
}

type machineInfo struct {
	Name  string
	State string
}

func (l *limaClient) run(ctx context.Context, args ...string) ([]byte, error) {
	return l.runner.Run(ctx, l.info.Path, args...)
}

func isExecutable(path string) bool {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return st.Mode()&0o111 != 0
}

func discoverLimactl(override, env string, lookPath func(string) (string, error)) (string, error) {
	if override != "" {
		if isExecutable(override) {
			return override, nil
		}
		return "", fmt.Errorf("configured --limactl is not executable: %s", override)
	}
	if env != "" {
		if isExecutable(env) {
			return env, nil
		}
		return "", fmt.Errorf("configured LIMACTL is not executable: %s", env)
	}
	for _, candidate := range []string{"/opt/homebrew/bin/limactl", "/usr/local/bin/limactl"} {
		if isExecutable(candidate) {
			return candidate, nil
		}
	}
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	path, err := lookPath("limactl")
	if err == nil && path != "" {
		return path, nil
	}
	return "", fmt.Errorf("Lima was not found; set --limactl or LIMACTL")
}

var limaVersionPattern = regexp.MustCompile(`\b([0-9]+\.[0-9]+\.[0-9]+)\b`)

func parseLimaVersion(raw string) string {
	match := limaVersionPattern.FindStringSubmatch(raw)
	if match == nil {
		return ""
	}
	return match[1]
}

func collectLimaInfo(ctx context.Context, path string, runner commandRunner) limaInfo {
	if runner == nil {
		runner = execRunner{}
	}
	info := limaInfo{Path: path}
	output, err := runner.Run(ctx, path, "--version")
	info.RawVersion = string(output)
	info.Version = parseLimaVersion(info.RawVersion)
	if err != nil {
		info.VersionErr = err
	}
	return info
}

func parseMachineListing(output string) ([]machineInfo, error) {
	var machines []machineInfo
	for lineNo, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "|")
		if len(fields) != 2 || fields[0] == "" || fields[1] == "" {
			return nil, fmt.Errorf("invalid Lima machine listing line %d: %q", lineNo+1, line)
		}
		machines = append(machines, machineInfo{Name: fields[0], State: fields[1]})
	}
	return machines, nil
}

func (l *limaClient) list(ctx context.Context) ([]machineInfo, error) {
	out, err := l.run(ctx, "list", "--format", "{{.Name}}|{{.Status}}")
	if err != nil {
		return nil, fmt.Errorf("limactl list: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return parseMachineListing(string(out))
}

func findMachine(machines []machineInfo, name string) (string, bool) {
	for _, machine := range machines {
		if machine.Name == name {
			return machine.State, true
		}
	}
	return "", false
}

func renderTemplate(template []byte, p macOSProfile) ([]byte, error) {
	rendered := string(template)
	replacements := map[string]string{
		"@IOLBOX_IMAGE_URL@":    p.ImageURL,
		"@IOLBOX_IMAGE_DIGEST@": p.ImageDigest,
		"@CPUS@":                p.CPUs,
		"@MEMORY@":              p.Memory,
		"@DISK@":                p.Disk,
	}
	for token, value := range replacements {
		rendered = strings.ReplaceAll(rendered, token, value)
	}
	if strings.Contains(rendered, "@") {
		return nil, fmt.Errorf("rendered Lima template still contains an unresolved placeholder")
	}
	return []byte(rendered), nil
}

func writeRenderedTemplate(dir string, p macOSProfile) (string, error) {
	data, err := os.ReadFile(p.TemplatePath)
	if err != nil {
		return "", err
	}
	rendered, err := renderTemplate(data, p)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "iolbox-*.yaml")
	if err != nil {
		return "", err
	}
	name := f.Name()
	defer func() {
		_ = f.Close()
	}()
	if err := f.Chmod(0o600); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	if _, err := f.Write(rendered); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func (l *limaClient) create(ctx context.Context, machine, template string) error {
	out, err := l.run(ctx, "create", "--name="+machine, template)
	if err != nil {
		return fmt.Errorf("limactl create: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (l *limaClient) start(ctx context.Context, machine string) error {
	out, err := l.run(ctx, "start", machine, "--tty=false")
	if err != nil {
		return fmt.Errorf("limactl start: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (l *limaClient) stop(ctx context.Context, machine string) error {
	out, err := l.run(ctx, "stop", machine)
	if err != nil {
		return fmt.Errorf("limactl stop: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (l *limaClient) shell(ctx context.Context, machine string, args ...string) ([]byte, error) {
	return l.run(ctx, append([]string{"shell", machine}, args...)...)
}

func (l *limaClient) copy(ctx context.Context, source, destination string) error {
	out, err := l.run(ctx, "copy", source, destination)
	if err != nil {
		return fmt.Errorf("limactl copy: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ensureMachine(ctx context.Context, l *limaClient, machine, state, template, attestationPath string, validAttestation func() error) (bool, error) {
	created := false
	if state == "" {
		if attestationPath != "" {
			if err := os.Remove(attestationPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return false, fmt.Errorf("remove stale attestation: %w", err)
			}
		}
		if err := l.create(ctx, machine, template); err != nil {
			return false, err
		}
		created = true
		state = "Stopped"
	}
	switch strings.ToLower(state) {
	case "running":
		return created, nil
	case "stopped":
		if !created && validAttestation != nil {
			if err := validAttestation(); err != nil {
				return false, codedError(exitPreflight, "refusing to start stopped machine %q: %v", machine, err)
			}
		}
		if err := l.start(ctx, machine); err != nil {
			return false, err
		}
		return created, nil
	default:
		return false, codedError(exitPreflight, "refusing to start machine %q in unrecognized state %q", machine, state)
	}
}

func stopMachine(ctx context.Context, l *limaClient, machine, state string, timeout time.Duration) error {
	switch strings.ToLower(state) {
	case "", "stopped":
		return nil
	case "running":
		if err := l.stop(ctx, machine); err != nil {
			return codedError(exitPreflight, "%v", err)
		}
		return waitForMachineState(ctx, l, machine, "stopped", timeout)
	default:
		return codedError(exitPreflight, "cannot stop machine %q in unrecognized state %q", machine, state)
	}
}

func waitForMachineState(ctx context.Context, l *limaClient, machine, wanted string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		machines, err := l.list(ctx)
		if err == nil {
			if state, ok := findMachine(machines, machine); ok && strings.EqualFold(state, wanted) {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return codedError(exitPreflight, "timed out waiting for machine %q to become %s", machine, wanted)
		case <-ticker.C:
		}
	}
}

func stageFiles(ctx context.Context, l *limaClient, machine string, p macOSProfile, payloadPath, payloadBase string) error {
	const stageDir = "/tmp/iolbox-provision-stage"
	const stagePayload = "/tmp/iolbox-payload.tar.gz"
	const newDir = "/tmp/iolbox-provision-new"
	const provisionDir = "/opt/iolbox-provision"

	// Build and validate a full replacement tree under newDir before touching
	// provisionDir, so a failed copy/flatten never leaves the live directory
	// the structural canary gate depends on empty or partial.
	if _, err := l.shell(ctx, machine, "sudo", "-n", "rm", "-rf", stageDir, newDir); err != nil {
		return codedError(exitPreflight, "clear guest staging: %v", err)
	}
	if err := l.copy(ctx, p.GuestDir, machine+":"+stageDir); err != nil {
		return codedError(exitPreflight, "copy guest scripts: %v", err)
	}
	if err := l.copy(ctx, payloadPath, machine+":"+stagePayload); err != nil {
		return codedError(exitPreflight, "copy payload: %v", err)
	}
	if _, err := l.shell(ctx, machine, "sudo", "-n", "mkdir", "-p", newDir+"/payload"); err != nil {
		return codedError(exitPreflight, "create guest staging tree: %v", err)
	}
	flatten := "set -e; src=\"$(find '" + stageDir + "' -name 'lib.sh' -print -quit)\"; [ -n \"$src\" ]; src=\"$(dirname \"$src\")\"; cp -f \"$src\"/*.sh '" + newDir + "/'; chmod 0755 '" + newDir + "'/*.sh; rm -rf '" + stageDir + "'"
	if _, err := l.shell(ctx, machine, "sudo", "-n", "sh", "-c", flatten); err != nil {
		return codedError(exitPreflight, "flatten guest scripts: %v", err)
	}
	if _, err := l.shell(ctx, machine, "sudo", "-n", "mv", stagePayload, newDir+"/payload/"+payloadBase); err != nil {
		return codedError(exitPreflight, "install guest payload: %v", err)
	}
	if _, err := l.shell(ctx, machine, "sudo", "-n", "test", "-f", newDir+"/30-canary.sh"); err != nil {
		return codedError(exitUsage, "staging failed: %s/30-canary.sh is missing", newDir)
	}

	// Only now, with a fully validated replacement in hand, swap it into
	// place. rm+mv run as a single guest command to minimize the window
	// during which provisionDir is absent.
	swap := "set -e; rm -rf '" + provisionDir + "'; mv '" + newDir + "' '" + provisionDir + "'"
	if _, err := l.shell(ctx, machine, "sudo", "-n", "sh", "-c", swap); err != nil {
		return codedError(exitPreflight, "install guest provision directory: %v", err)
	}
	if _, err := l.shell(ctx, machine, "sudo", "-n", "test", "-f", provisionDir+"/30-canary.sh"); err != nil {
		return codedError(exitUsage, "staging failed: %s/30-canary.sh is missing after install", provisionDir)
	}
	return nil
}

func guestStep(ctx context.Context, l *limaClient, machine, step string, env map[string]string) error {
	args := guestStepArgs(step, env)
	out, err := l.shell(ctx, machine, args...)
	if err != nil {
		code := childExitCode(err)
		if code >= 1 && code <= 5 {
			return &launcherError{code: code, err: fmt.Errorf("guest step %s exited %d: %s", step, code, strings.TrimSpace(string(out)))}
		}
		return codedError(exitUsage, "guest step %s failed: %v: %s", step, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func guestStepArgs(step string, env map[string]string, extra ...string) []string {
	args := []string{"sudo", "-E", "env"}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, key+"="+env[key])
	}
	args = append(args, "bash", "/opt/iolbox-provision/"+step)
	return append(args, extra...)
}

func childExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".iolbox-atomic-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := io.Copy(f, bytes.NewReader(data)); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

type payloadCandidate struct {
	Path  string
	MTime time.Time
}

func selectPayload(explicit, assetRoot string) (string, error) {
	if explicit != "" {
		if st, err := os.Stat(explicit); err == nil && st.Mode().IsRegular() {
			return explicit, nil
		}
		return "", codedError(exitPreflight, "payload is not a regular file: %s", explicit)
	}
	roots := []string{assetRoot}
	if cwd, err := os.Getwd(); err == nil && cwd != assetRoot {
		roots = append(roots, cwd)
	}
	var candidates []payloadCandidate
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), "iolbox-server-") || !strings.HasSuffix(entry.Name(), ".tar.gz") {
				continue
			}
			info, err := entry.Info()
			if err == nil && info.Mode().IsRegular() {
				candidates = append(candidates, payloadCandidate{Path: filepath.Join(root, entry.Name()), MTime: info.ModTime()})
			}
		}
	}
	if len(candidates) == 0 {
		return "", codedError(exitPreflight, "no iolbox-server-*.tar.gz payload found")
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].MTime.Equal(candidates[j].MTime) {
			return candidates[i].Path < candidates[j].Path
		}
		return candidates[i].MTime.After(candidates[j].MTime)
	})
	return candidates[0].Path, nil
}
