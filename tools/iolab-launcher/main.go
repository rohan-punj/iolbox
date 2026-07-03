// iolab-launcher — a single Windows exe that gets a user from zero to
// http://localhost:4001, picking the right Linux backend automatically.
//
// Two backends:
//   - qemu : bundled qemu-system-x86_64 (TCG software emulation), works
//     everywhere, conflicts with nothing, no admin. The fallback.
//   - wsl  : the `iolab` WSL2 distro (imported from iolab-rootfs.tar). Fastest,
//     but only usable when an active Windows hypervisor is present.
//
// Backend selection implements docs/providers.md: WSL2 is preferred ONLY when
// actually usable (wsl.exe + `wsl --version` OK + an active hypervisor);
// otherwise QEMU. The launcher NEVER enables a Windows feature.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// launchOpts holds the resolved CLI options passed to a backend.
type launchOpts struct {
	memMB int
	smp   int

	noBrowser bool
	verbose   bool

	bootTimeout   time.Duration
	shutdownGrace time.Duration

	// dev / override paths
	qemuExe  string
	diskPath string
	wslTar   string
	wslDir   string
}

func main() {
	var (
		backendFlag = flag.String("backend", "auto", "backend: qemu | wsl | auto")
		memMB       = flag.Int("mem", 3072, "guest RAM in MB")
		smp         = flag.Int("smp", 2, "guest vCPU count")
		noBrowser   = flag.Bool("no-browser", false, "do not open the default browser when the GUI is up")
		portsOvr    = flag.String("ports", "", "override port ranges: gui:cStart:cCount:capStart:capCount (empty fields keep defaults)")
		bootTO      = flag.Duration("boot-timeout", 6*time.Minute, "how long to wait for the GUI (TCG boots are slow)")
		grace       = flag.Duration("shutdown-grace", 30*time.Second, "how long to wait for a clean guest powerdown")
		verbose     = flag.Bool("v", false, "verbose (print the qemu command line)")

		qemuExe    = flag.String("qemu", "", "override path to qemu-system-x86_64.exe (dev)")
		diskPath   = flag.String("disk", "", "override path to iolab-disk.qcow2 (dev)")
		wslTar     = flag.String("wsl-tar", "", "override path to iolab-rootfs.tar (dev)")
		wslDir     = flag.String("wsl-dir", "", "override WSL install dir (default %LOCALAPPDATA%\\iolab)")
		detectOnly = flag.Bool("detect", false, "print backend detection and exit (no launch)")
	)
	flag.Parse()

	sel := backend(strings.ToLower(strings.TrimSpace(*backendFlag)))
	switch sel {
	case backendQEMU, backendWSL, backendAuto:
	default:
		fmt.Fprintf(os.Stderr, "invalid --backend %q (want qemu|wsl|auto)\n", *backendFlag)
		os.Exit(2)
	}

	ranges := defaultPortRanges()
	if r, err := parsePortsOverride(*portsOvr, ranges); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	} else {
		ranges = r
	}

	opts := launchOpts{
		memMB:         *memMB,
		smp:           *smp,
		noBrowser:     *noBrowser,
		verbose:       *verbose,
		bootTimeout:   *bootTO,
		shutdownGrace: *grace,
		qemuExe:       *qemuExe,
		diskPath:      *diskPath,
		wslTar:        *wslTar,
		wslDir:        *wslDir,
	}

	// Detect once; used for the decision and printed for transparency.
	det := detectBackends()
	chosen := chooseBackend(sel, &det)

	printDetection(det, chosen)
	if *detectOnly {
		return
	}

	// Ctrl-C -> cancel ctx -> backend does a clean shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch chosen {
	case backendWSL:
		if sel == backendWSL && !det.wslUsable() {
			// Explicit --backend wsl on an unusable machine: clear error, non-zero exit.
			fmt.Fprintf(os.Stderr, "\nERROR: %s\n", det.reason)
			fmt.Fprintln(os.Stderr, "Use --backend qemu (or --backend auto) on this machine.")
			os.Exit(1)
		}
		b := &wslBackend{opts: opts, ranges: ranges}
		err = b.run(ctx)
	case backendQEMU:
		b := &qemuBackend{opts: opts, ranges: ranges, det: det}
		err = b.run(ctx)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "\nERROR: %v\n", err)
		os.Exit(1)
	}
}

func printDetection(d detection, chosen backend) {
	logf("iolab-launcher — backend selection")
	logf("  wsl.exe present:       %v", d.wslExePresent)
	logf("  wsl --version OK:      %v", d.wslVersionOK)
	if d.hypervisorKnown {
		logf("  hypervisor present:    %v", d.hypervisorPresent)
	} else {
		logf("  hypervisor present:    unknown (CIM query failed)")
	}
	logf("  WSL2 usable:           %v", d.wslUsable())
	logf("  -> chosen backend:     %s", chosen)
	logf("  %s", d.reason)
	logf("  NOTE: the GUI has no auth; the launcher forwards it to 127.0.0.1 only (localhost-only).")
}

// logf writes a timestamped line to stderr (plain console UX).
func logf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "[iolab] "+format+"\n", a...)
}

// openBrowser opens the default browser at url via cmd /c start. Non-fatal.
func openBrowser(url string) {
	logf("Opening %s in your browser...", url)
	// `cmd /c start "" <url>` — the empty "" is the window title arg so a URL
	// with special chars isn't mistaken for the title.
	cmd := exec.Command("cmd", "/c", "start", "", url)
	if err := cmd.Start(); err != nil {
		logf("  (could not auto-open the browser: %v — open %s manually)", err, url)
	}
}

// quoteArgs renders an argv for display (verbose mode), quoting args with spaces.
func quoteArgs(args []string) string {
	var b strings.Builder
	for i, a := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		if strings.ContainsAny(a, " \t") {
			b.WriteByte('"')
			b.WriteString(a)
			b.WriteByte('"')
		} else {
			b.WriteString(a)
		}
	}
	return b.String()
}
