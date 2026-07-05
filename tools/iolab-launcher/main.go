// iolbox-launcher — a single Windows exe that gets a user from zero to
// http://localhost:4001, picking the right Linux backend automatically.
//
// Two backends:
//   - qemu : bundled qemu-system-x86_64 (TCG software emulation), works
//     everywhere, conflicts with nothing, no admin. The fallback.
//   - wsl  : the `iolbox` WSL2 distro (imported from iolbox-rootfs.tar). Fastest,
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
	"path/filepath"
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

	// folder sync (images\/labs\ <-> guest, see foldersync.go)
	noSync    bool
	imagesDir string
	labsDir   string
}

func main() {
	var (
		// mem/smp defaults mirror runtime/resources.env (IOLBOX_RAM_MB=4096,
		// IOLBOX_VCPUS=4), the single source of truth for vCPU/RAM across every
		// iolbox deployment target; still overridable here via --mem/--smp.
		backendFlag = flag.String("backend", "auto", "backend: qemu | wsl | auto")
		memMB       = flag.Int("mem", 4096, "guest RAM in MB")
		smp         = flag.Int("smp", 4, "guest vCPU count")
		noBrowser   = flag.Bool("no-browser", false, "do not open the default browser when the GUI is up")
		portsOvr    = flag.String("ports", "", "override port ranges: gui:cStart:cCount:capStart:capCount (empty fields keep defaults)")
		bootTO      = flag.Duration("boot-timeout", 6*time.Minute, "how long to wait for the GUI (TCG boots are slow)")
		grace       = flag.Duration("shutdown-grace", 30*time.Second, "how long to wait for a clean guest powerdown")
		verbose     = flag.Bool("v", false, "verbose (print the qemu command line)")

		qemuExe    = flag.String("qemu", "", "override path to qemu-system-x86_64.exe (dev)")
		diskPath   = flag.String("disk", "", "override path to iolbox-disk.qcow2 (dev)")
		wslTar     = flag.String("wsl-tar", "", "override path to iolbox-rootfs.tar (dev)")
		wslDir     = flag.String("wsl-dir", "", "override WSL install dir (default %LOCALAPPDATA%\\iolbox)")
		detectOnly = flag.Bool("detect", false, "print backend detection and exit (no launch)")

		noSync    = flag.Bool("no-sync", false, "disable images\\/labs\\ folder sync (pure ephemeral; folders untouched)")
		imagesDir = flag.String("images-dir", "", "override the images folder (default <exeDir>\\images)")
		labsDir   = flag.String("labs-dir", "", "override the labs folder (default <exeDir>\\labs)")

		// consoleAddr / noConsole select between the two run modes (see
		// runWithConsole / runDirect below). Default ON: the console is the new
		// primary UX. Set -console-addr "" OR -no-console to get the OLD
		// behavior back verbatim (main() calls backend.run(ctx) synchronously,
		// blocking until Ctrl-C) — this keeps --detect, dev flows, and any CI
		// that expects the direct-run semantics working unchanged.
		consoleAddr = flag.String("console-addr", "127.0.0.1:4002", "address for the local control console HTTP server (empty disables it, same as -no-console)")
		noConsole   = flag.Bool("no-console", false, "disable the control console and run the chosen backend directly, blocking until Ctrl-C (legacy behavior)")
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
		noSync:        *noSync,
		imagesDir:     *imagesDir,
		labsDir:       *labsDir,
	}

	// Detect once; used for the decision and printed for transparency.
	det := detectBackends()
	chosen := chooseBackend(sel, &det)

	printDetection(det, chosen)
	if *detectOnly {
		return
	}

	// Ctrl-C -> cancel ctx -> backend (and, in console mode, the console
	// server too) does a clean shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Explicit --backend wsl on an unusable machine is a hard, immediate
	// error in BOTH run modes — surfacing it before we ever bind the console
	// port keeps the failure mode identical to the pre-console CLI.
	if sel == backendWSL && chosen == backendWSL && !det.wslUsable() {
		fmt.Fprintf(os.Stderr, "\nERROR: %s\n", det.reason)
		fmt.Fprintln(os.Stderr, "Use --backend qemu (or --backend auto) on this machine.")
		os.Exit(1)
	}

	// Two run modes, chosen by -console-addr / -no-console:
	//
	//   - runDirect (legacy, default OFF): main() builds the one chosen
	//     backend and calls b.run(ctx) synchronously, exactly as before this
	//     feature existed. Selected by -no-console or -console-addr "".
	//     Existing --detect/dev flows and any CI that runs the backend
	//     directly and expects it to occupy the foreground keep working
	//     unchanged — nothing about that path was touched.
	//
	//   - runWithConsole (new default): main() instead starts the :4002
	//     control-console HTTP server, which owns backend lifecycle behind a
	//     lifecycleController (start/stop from the HTTP handlers, mutex-
	//     guarded, one context.CancelFunc per running backend — see
	//     lifecycle.go). main() opens the browser to the console (not
	//     straight to :4001 — the console links through once the GUI is
	//     reachable) and blocks on ctx.Done(), then stops the console server
	//     AND any backend it had started.
	if *noConsole || strings.TrimSpace(*consoleAddr) == "" {
		runDirect(ctx, chosen, sel, det, opts, ranges)
		return
	}
	runWithConsole(ctx, *consoleAddr, chosen, det, opts, ranges, *noBrowser)
}

// runDirect is the pre-console behavior: build the one chosen backend and run
// it synchronously in the foreground until ctx is cancelled or it exits on
// its own.
func runDirect(ctx context.Context, chosen, sel backend, det detection, opts launchOpts, ranges portRanges) {
	var b runnable
	switch chosen {
	case backendWSL:
		b = &wslBackend{opts: opts, ranges: ranges}
	case backendQEMU:
		b = &qemuBackend{opts: opts, ranges: ranges, det: det}
	}
	if err := b.run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "\nERROR: %v\n", err)
		os.Exit(1)
	}
}

// runWithConsole starts the control console server and blocks until ctx is
// cancelled, at which point it stops any running backend and shuts the
// console server down. The backend is NOT started automatically — the user
// presses Start on the console page (matching "start is async" in the API
// contract); this mirrors how a user would expect a control panel to behave
// (nothing happens until you ask it to).
func runWithConsole(ctx context.Context, consoleAddr string, chosen backend, det detection, opts launchOpts, ranges portRanges, noBrowser bool) {
	exeDir := ""
	if exePath, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exePath)
	}

	// newBackend is re-evaluated on every Start() call so a deployment/cpu/ram
	// change saved via PUT /api/config takes effect on the NEXT start without
	// restarting this process. cfg.Deployment overrides the CLI-detected
	// backend once the user has explicitly chosen one via the console.
	newBackend := func() (runnable, error) {
		cfg := loadLauncherConfig(exeDir)
		o := opts
		o.smp = cfg.CPUs
		o.memMB = cfg.RAMMB

		sel := backend(cfg.Deployment)
		use := chosen
		switch sel {
		case backendQEMU, backendWSL:
			use = sel
		}
		switch use {
		case backendWSL:
			return &wslBackend{opts: o, ranges: ranges}, nil
		default:
			return &qemuBackend{opts: o, ranges: ranges, det: det}, nil
		}
	}

	lc := newLifecycleController(newBackend)
	deps := consoleDeps{
		exeDir:       exeDir,
		ranges:       ranges,
		lc:           lc,
		guiProbe:     func(port int) bool { return probeGUIOnce("127.0.0.1", port) },
		guestVersion: fetchGuestVersion,
		qemuImgPath:  func() string { return defaultQemuImgPath(exeDir) },
		diskPath:     func() string { return defaultDiskPath(exeDir) },
	}
	cs := newConsoleServer(deps)

	// Seed launcher.json on first run so GET /api/config always has something
	// sane to show, and so the initial deployment matches what --backend
	// resolved to (rather than silently defaulting to "auto" the first time
	// the console page loads).
	if _, statErr := os.Stat(filepath.Join(exeDir, launcherConfigFileName)); statErr != nil {
		cfg := defaultLauncherConfig()
		cfg.CPUs = opts.smp
		cfg.RAMMB = opts.memMB
		cfg.Deployment = string(chosen)
		_ = saveLauncherConfig(exeDir, cfg)
	}

	logf("Starting the iolbox console on http://%s ...", consoleAddr)
	if !noBrowser {
		openBrowser("http://" + consoleAddr)
	}
	logf("Press Start on the console page to boot the guest. Press Ctrl-C here to shut everything down.")

	if err := runConsoleServer(ctx, consoleAddr, cs); err != nil {
		fmt.Fprintf(os.Stderr, "\nERROR: console server: %v\n", err)
	}

	// ctx is already cancelled by the time runConsoleServer returns (Ctrl-C);
	// make sure any backend the user started is also stopped cleanly rather
	// than left to be hard-killed when the process exits.
	if err := lc.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: stopping backend: %v\n", err)
	}
}

func printDetection(d detection, chosen backend) {
	logf("iolbox-launcher — backend selection")
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
	fmt.Fprintf(os.Stderr, "[iolbox] "+format+"\n", a...)
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
