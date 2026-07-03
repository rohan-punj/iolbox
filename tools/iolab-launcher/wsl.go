package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// wslBackend imports (first run) and starts the `iolab` WSL2 distro, then waits
// for the supervisor's GUI on 127.0.0.1:4001 (WSL2 localhost forwarding makes
// the guest's :4001 reachable directly).
//
// NOTE on systemd: runtime/files/wsl.conf sets [boot] systemd=true, so the
// rootfs boots real systemd and iolab-supervisor.service autostarts. The
// PRIMARY start path here is therefore just "start the distro and wait for
// :4001". A direct-exec fallback (mirroring the unit's ExecStart) is retained
// in case a distro was imported from a tar WITHOUT systemd=true.
type wslBackend struct {
	opts   launchOpts
	ranges portRanges

	distro     string // "iolab"
	installDir string
}

const wslDistroName = "iolab"

// run brings the WSL2 backend up end to end.
func (w *wslBackend) run(ctx context.Context) error {
	w.distro = wslDistroName

	if _, err := exec.LookPath("wsl.exe"); err != nil {
		return fmt.Errorf("wsl.exe not found — WSL is not installed. Install WSL2, or use --backend qemu")
	}

	present, err := w.distroExists()
	if err != nil {
		return err
	}
	if !present {
		if err := w.importDistro(); err != nil {
			return err
		}
	} else {
		logf("WSL distro %q already present.", w.distro)
	}

	// Start the distro. With systemd=true, `wsl -d iolab true` triggers boot;
	// systemd then starts iolab-supervisor.service. If the distro lacks
	// systemd, fall back to launching the supervisor directly.
	if err := w.startDistro(ctx); err != nil {
		return err
	}

	guiURL := fmt.Sprintf("http://localhost:%d", w.ranges.guiPort)
	logf("Waiting for the GUI on :%d ...", w.ranges.guiPort)
	up := waitForPort(ctx, "127.0.0.1", w.ranges.guiPort, w.opts.bootTimeout, nil)
	if !up {
		return fmt.Errorf("timed out waiting for the GUI on %s.\n"+
			"  Check the supervisor: wsl -d %s -- systemctl status iolab-supervisor.service\n"+
			"  (if systemctl reports 'System has not been booted with systemd', the imported\n"+
			"   tar lacks [boot] systemd=true in /etc/wsl.conf — see the orchestrator note)",
			guiURL, w.distro)
	}
	logf("GUI is up: %s", guiURL)
	if !w.opts.noBrowser {
		openBrowser(guiURL)
	}
	logf("iolab is running in WSL2. Press Ctrl-C to shut down cleanly.")

	<-ctx.Done()
	logf("Shutdown requested — terminating the %q distro...", w.distro)
	w.terminate()
	return nil
}

// distroExists checks `wsl --list --quiet` for the iolab distro. wsl.exe emits
// UTF-16LE, so we strip NULs before matching.
func (w *wslBackend) distroExists() (bool, error) {
	out, err := exec.Command("wsl.exe", "--list", "--quiet").Output()
	if err != nil {
		return false, fmt.Errorf("`wsl --list` failed (%w) — is WSL2 installed and working?", err)
	}
	txt := strings.ReplaceAll(string(out), "\x00", "")
	for _, line := range strings.Split(txt, "\n") {
		if strings.EqualFold(strings.TrimSpace(line), w.distro) {
			return true, nil
		}
	}
	return false, nil
}

// importDistro runs `wsl --import iolab <dir> <tar> --version 2`. The tar is
// iolab-rootfs.tar located next to the exe (or via --wsl-tar). The install dir
// defaults to %LOCALAPPDATA%\iolab.
func (w *wslBackend) importDistro() error {
	tar := w.opts.wslTar
	if tar == "" {
		exePath, err := os.Executable()
		if err != nil {
			return err
		}
		tar = filepath.Join(filepath.Dir(exePath), "iolab-rootfs.tar")
	}
	if _, err := os.Stat(tar); err != nil {
		return fmt.Errorf("WSL rootfs tarball not found at %s\n"+
			"  (download iolab-rootfs.tar from the release and place it next to the launcher,\n"+
			"   or pass --wsl-tar <path>): %w", tar, err)
	}

	dir := w.opts.wslDir
	if dir == "" {
		la := os.Getenv("LOCALAPPDATA")
		if la == "" {
			la = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
		}
		dir = filepath.Join(la, "iolab")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create WSL install dir %s: %w", dir, err)
	}
	w.installDir = dir

	logf("Importing WSL distro %q from %s into %s ...", w.distro, tar, dir)
	cmd := exec.Command("wsl.exe", "--import", w.distro, dir, tar, "--version", "2")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("`wsl --import` failed: %w", err)
	}
	logf("Import complete.")
	return nil
}

// startDistro boots the distro. With systemd=true a no-op command triggers the
// boot; we then verify systemd is actually PID 1. If not, we exec the
// supervisor directly (detached) mirroring iolab-supervisor.service's ExecStart.
func (w *wslBackend) startDistro(ctx context.Context) error {
	logf("Starting WSL distro %q ...", w.distro)
	// Trigger boot (also validates the distro runs at all).
	boot := exec.Command("wsl.exe", "-d", w.distro, "--", "true")
	if err := boot.Run(); err != nil {
		return fmt.Errorf("failed to start distro %q: %w", w.distro, err)
	}

	if w.systemdActive() {
		logf("  systemd is PID 1 — iolab-supervisor.service will autostart.")
		return nil
	}

	// Fallback: no systemd (tar imported without [boot] systemd=true). Launch
	// the supervisor directly with the same flags as the unit file, backgrounded
	// inside the distro so this call returns.
	logf("  systemd not active in the distro — launching the supervisor directly (unit-mirrored flags).")
	return w.startSupervisorDirect(ctx)
}

// systemdActive returns true if PID 1 in the distro is systemd.
func (w *wslBackend) systemdActive() bool {
	out, err := exec.Command("wsl.exe", "-d", w.distro, "--",
		"sh", "-c", "cat /proc/1/comm 2>/dev/null").Output()
	if err != nil {
		return false
	}
	return bytes.Contains(bytes.ToLower(out), []byte("systemd"))
}

// startSupervisorDirect runs the firstboot iourc step (if the iourc is absent)
// then launches the supervisor detached, mirroring
// runtime/files/iolab-supervisor.service ExecStart exactly.
func (w *wslBackend) startSupervisorDirect(_ context.Context) error {
	// Mirror the unit: prestart clean, firstboot iourc (idempotent), then the
	// supervisor with the full proven flag set, nohup'd so wsl.exe returns.
	script := strings.Join([]string{
		`[ -x /opt/iolab/prestart-clean.sh ] && /opt/iolab/prestart-clean.sh || true`,
		`if [ ! -f /opt/iolab/iourc ] && [ -x /opt/iolab/firstboot-iourc.sh ]; then /opt/iolab/firstboot-iourc.sh || true; fi`,
		`nohup /opt/iolab/supervisor ` +
			`-control-addr 127.0.0.1:4000 ` +
			`-ws-addr 0.0.0.0:4001 ` +
			`-console-bind 0.0.0.0 ` +
			`-capture-bind 0.0.0.0 ` +
			`-image-dir /opt/iolab/images ` +
			`-run-dir /opt/iolab/run ` +
			`-labs-dir /opt/iolab/labs ` +
			`-iourc /opt/iolab/iourc ` +
			`>/var/log/iolab-supervisor.log 2>&1 &`,
	}, "; ")

	cmd := exec.Command("wsl.exe", "-d", w.distro, "-u", "root", "--", "sh", "-c", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to launch supervisor directly: %w", err)
	}
	// Give the background process a moment to bind.
	time.Sleep(500 * time.Millisecond)
	return nil
}

// terminate cleanly stops the distro (kills systemd + all children).
func (w *wslBackend) terminate() {
	cmd := exec.Command("wsl.exe", "--terminate", w.distro)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		logf("  `wsl --terminate %s` returned: %v", w.distro, err)
	} else {
		logf("  distro %q terminated.", w.distro)
	}
}
