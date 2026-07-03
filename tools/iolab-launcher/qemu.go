package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// qemuBackend launches the bundled qemu-system-x86_64 (TCG, no hardware accel)
// with the iolab qcow2 disk, forwards the GUI/console/capture ports, waits for
// the GUI to come up on 127.0.0.1:<gui>, and manages clean shutdown via QMP.
type qemuBackend struct {
	opts   launchOpts
	ranges portRanges
	det    detection

	qemuExe  string
	diskPath string
	qmpAddr  string // 127.0.0.1:<port> for the QMP monitor socket
}

// locateQemu finds the bundled qemu binary + disk relative to the launcher exe.
// Layout next to the exe:
//
//	iolab-launcher.exe
//	iolab-disk.qcow2
//	qemu/qemu-system-x86_64.exe   (+ its DLLs, bios/, keymaps/ ...)
func (q *qemuBackend) locate() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate own exe path: %w", err)
	}
	exeDir := filepath.Dir(exePath)

	// qemu binary: qemu/qemu-system-x86_64.exe next to the launcher. Allow a
	// --qemu override for dev.
	q.qemuExe = q.opts.qemuExe
	if q.qemuExe == "" {
		q.qemuExe = filepath.Join(exeDir, "qemu", "qemu-system-x86_64.exe")
	}
	if _, err := os.Stat(q.qemuExe); err != nil {
		return fmt.Errorf("bundled qemu not found at %s\n"+
			"  (expected a 'qemu' folder with qemu-system-x86_64.exe next to the launcher;\n"+
			"   download the release bundle or see THIRD_PARTY.md to stage it): %w", q.qemuExe, err)
	}

	// disk: iolab-disk.qcow2 next to the launcher. Allow a --disk override.
	q.diskPath = q.opts.diskPath
	if q.diskPath == "" {
		q.diskPath = filepath.Join(exeDir, "iolab-disk.qcow2")
	}
	if _, err := os.Stat(q.diskPath); err != nil {
		return fmt.Errorf("iolab disk image not found at %s\n"+
			"  (download iolab-disk.qcow2 from the release and place it next to the launcher): %w",
			q.diskPath, err)
	}
	return nil
}

// buildArgs assembles the qemu-system-x86_64 command line.
//
// Accel chain: -accel whpx (only when a hypervisor is present — WHPX needs the
// Windows Hypervisor Platform, which only exists when hypervisorPresent) then
// -accel tcg as the guaranteed fallback. qemu tries them left-to-right and uses
// the first that initializes, so on a VMware box (no hypervisor) it lands on
// TCG. WHPX conflicts with nothing and needs no admin. When no hypervisor is
// present we omit whpx entirely (offering it would just print an init-failure
// warning every boot).
//
// Disk is attached if=virtio: Debian's generic linux-image-amd64 builds in
// virtio_blk (and the initramfs MODULES=most carries it), so the stock kernel
// boots off virtio with no extra build. virtio is the faster path under TCG.
func (q *qemuBackend) buildArgs(fwdPorts []int) []string {
	args := []string{
		"-machine", "pc",
	}
	if q.det.hypervisorKnown && q.det.hypervisorPresent {
		// Prefer WHPX when the platform is there, fall back to TCG.
		args = append(args, "-accel", "whpx", "-accel", "tcg")
	} else {
		// No hypervisor: TCG only (this is the compatibility provider's whole
		// reason to exist — see runtime/qemu-compat.md).
		args = append(args, "-accel", "tcg")
	}
	args = append(args,
		"-m", strconv.Itoa(q.opts.memMB),
		"-smp", strconv.Itoa(q.opts.smp),
		"-drive", "file="+q.diskPath+",format=qcow2,if=virtio",
		"-netdev", qemuNetdevArgFor(fwdPorts),
		"-device", "virtio-net-pci,netdev=net0",
		"-display", "none",
		"-serial", "none",
		"-no-reboot",
		// QMP over TCP so we can issue a graceful system_powerdown on Ctrl-C.
		// nowait = don't block boot waiting for a monitor client to attach.
		"-qmp", "tcp:"+q.qmpAddr+",server,nowait",
	)
	return args
}

func (q *qemuBackend) run(ctx context.Context) error {
	if err := q.locate(); err != nil {
		return err
	}
	q.qmpAddr = "127.0.0.1:" + strconv.Itoa(pickQMPPort(q.ranges.guiPort))

	// Pre-probe the forward ports: qemu aborts the entire launch if ANY
	// hostfwd port can't bind, so filter busy ones out up front. A busy GUI
	// port is fatal; busy console/capture ports are skipped with a warning
	// (those consoles/captures just won't be reachable from Windows).
	fwdPorts, busy := probeFreePorts(q.ranges.forwardedPorts())
	for _, p := range busy {
		if p == q.ranges.guiPort {
			return fmt.Errorf("port %d (the GUI port) is already in use on 127.0.0.1 — "+
				"close the conflicting application or pick another GUI port via --ports", p)
		}
	}
	if len(busy) > 0 {
		logf("WARNING: skipping %d busy port(s) already in use on this machine: %v", len(busy), busy)
		logf("         (another app holds them; consoles/captures on those exact ports won't be reachable)")
	}

	args := q.buildArgs(fwdPorts)
	logf("Starting QEMU (%s backend)", accelDescr(q.det))
	logf("  qemu:  %s", q.qemuExe)
	logf("  disk:  %s", q.diskPath)
	logf("  mem:   %d MB   smp: %d", q.opts.memMB, q.opts.smp)
	logf("  GUI:   http://localhost:%d", q.ranges.guiPort)
	if q.opts.verbose {
		logf("  args:  %s %s", q.qemuExe, quoteArgs(args))
	}

	cmd := exec.Command(q.qemuExe, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// CREATE_NEW_PROCESS_GROUP: detach qemu from console Ctrl-C delivery
	// (Windows disables CTRL_C_EVENT for processes started in a new group).
	// Without this, the user's Ctrl-C kills qemu directly and abruptly —
	// bypassing the graceful QMP system_powerdown path below and leaving the
	// guest fs to journal-recover on next boot.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start qemu: %w", err)
	}

	// Watch the process; if it dies while we're waiting for :4001, surface it.
	// procDone is CLOSED on exit (not sent to) so multiple receivers — the
	// port waiter, the main select, powerdown — can all observe it; procErr
	// carries the exit error (safe to read only after procDone is closed).
	var procErr error
	procDone := make(chan struct{})
	go func() { procErr = cmd.Wait(); close(procDone) }()

	// Wait for the GUI port with progress. Under TCG the kernel boot is
	// minutes-slow — that's expected; keep a generous timeout.
	guiURL := fmt.Sprintf("http://localhost:%d", q.ranges.guiPort)
	logf("Waiting for the GUI on :%d (TCG boot is slow — this can take a few minutes)...", q.ranges.guiPort)
	up := waitForGUI(ctx, "127.0.0.1", q.ranges.guiPort, q.opts.bootTimeout, procDone)
	select {
	case <-procDone:
		return fmt.Errorf("qemu exited before the GUI came up (exit: %v) — check the qemu output above", procErr)
	default:
	}
	if !up {
		logf("GUI did not come up within %s; terminating qemu.", q.opts.bootTimeout)
		q.powerdown(cmd, procDone)
		return fmt.Errorf("timed out waiting for the GUI on %s", guiURL)
	}

	logf("GUI is up: %s", guiURL)
	if !q.opts.noBrowser {
		openBrowser(guiURL)
	}
	logf("iolab is running. Press Ctrl-C to shut down cleanly.")

	// Block until Ctrl-C (ctx cancelled) or qemu exits on its own.
	select {
	case <-ctx.Done():
		logf("Shutdown requested — asking the guest to power down (QMP system_powerdown)...")
		q.powerdown(cmd, procDone)
		return nil
	case <-procDone:
		if procErr != nil {
			return fmt.Errorf("qemu exited: %w", procErr)
		}
		logf("qemu exited.")
		return nil
	}
}

// powerdown attempts a graceful QMP system_powerdown, waits for the process to
// exit, and hard-kills as a last resort. done is the process-exit channel from
// run(); it is CLOSED when qemu exits (multi-receiver safe). powerdown must
// NOT call cmd.Wait() itself (Go forbids a second Wait on the same Cmd).
func (q *qemuBackend) powerdown(cmd *exec.Cmd, done <-chan struct{}) {
	// Even when the QMP reply read fails, the command may well have been
	// DELIVERED (a fast guest shutdown closes the QMP socket mid-reply), so
	// wait the grace period for a clean exit in both cases before escalating.
	if err := qmpPowerdown(q.qmpAddr); err != nil {
		logf("  QMP powerdown reply uncertain (%v); waiting up to %s in case it landed...",
			err, q.opts.shutdownGrace)
	} else {
		logf("  ACPI powerdown sent; waiting up to %s for a clean stop...", q.opts.shutdownGrace)
	}
	select {
	case <-done:
		logf("  qemu exited cleanly.")
		return
	case <-time.After(q.opts.shutdownGrace):
		logf("  grace period elapsed; forcing qemu quit via QMP.")
	}

	// Try QMP quit (clean qemu process exit) before an OS kill.
	_ = qmpQuit(q.qmpAddr)
	select {
	case <-done:
		logf("  qemu stopped.")
		return
	case <-time.After(5 * time.Second):
		logf("  forcing kill.")
		_ = cmd.Process.Kill()
		<-done
	}
}

func accelDescr(d detection) string {
	if d.hypervisorKnown && d.hypervisorPresent {
		return "qemu, WHPX-accelerated with TCG fallback"
	}
	return "qemu, software emulation (TCG)"
}

// pickQMPPort derives a QMP monitor port unlikely to clash with forwarded
// ranges: gui+1000 (e.g. 5001 for gui 4001). Bounded to a valid port; if that
// port is taken we let qemu error and the user can retry — QMP binding is on
// 127.0.0.1 only.
func pickQMPPort(gui int) int {
	p := gui + 1000
	if p > 65535 {
		p = 45001
	}
	return p
}

// waitForGUI polls an HTTP GET of http://host:port/ until it returns a real
// HTTP response (any status < 500 — the GUI serves 200; <500 tolerates a
// transient 4xx during startup), ctx is cancelled, the timeout elapses, or the
// watched process exits (procDone closed; pass nil for no process to watch).
// Returns true once the GUI actually responds. This replaces a bare TCP-connect
// probe, which false-positives instantly under QEMU user-mode networking
// because slirp accepts the host-side hostfwd connection before the guest is up.
func waitForGUI(ctx context.Context, host string, port int, timeout time.Duration, procDone <-chan struct{}) bool {
	deadline := time.Now().Add(timeout)
	target := fmt.Sprintf("http://%s:%d/", host, port)
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	lastLog := time.Now()
	for {
		resp, err := client.Get(target)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return true
			}
		}
		select {
		case <-ctx.Done():
			return false
		case <-procDone:
			return false
		case <-ticker.C:
		}
		if time.Now().After(deadline) {
			return false
		}
		if time.Since(lastLog) > 15*time.Second {
			logf("  ...still booting (%s elapsed of %s budget)", time.Until(deadline).Round(time.Second), timeout)
			lastLog = time.Now()
		}
	}
}
