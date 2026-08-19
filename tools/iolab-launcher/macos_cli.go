package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type darwinOptions struct {
	Command          string
	Profile          string
	Machine          string
	AssetsDir        string
	Limactl          string
	Tarball          string
	BootTimeout      time.Duration
	NoBrowser        bool
	NoSync           bool
	ImagesDir        string
	LabsDir          string
	Bind             string
	GUIPort          int
	ConsoleHostStart int
	CaptureHostStart int
}

func parseDarwinArgs(args []string, stderr io.Writer) (darwinOptions, error) {
	opts := darwinOptions{
		Command:          "start",
		Profile:          os.Getenv("IOLBOX_PROFILE"),
		Machine:          os.Getenv("IOLBOX_MACHINE"),
		Limactl:          os.Getenv("LIMACTL"),
		Tarball:          os.Getenv("IOLBOX_TARBALL"),
		BootTimeout:      6 * time.Minute,
		Bind:             os.Getenv("IOLBOX_BIND"),
		GUIPort:          4001,
		ConsoleHostStart: darwinConsoleStart,
		CaptureHostStart: darwinCaptureStart,
	}
	if opts.Bind == "" {
		opts.Bind = "all"
	}
	if value := os.Getenv("IOLBOX_GUI_PORT"); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return darwinOptions{}, fmt.Errorf("invalid IOLBOX_GUI_PORT %q", value)
		}
		opts.GUIPort = port
	}
	for _, setting := range []struct {
		name string
		dest *int
	}{
		{"IOLBOX_CONSOLE_HOST_START", &opts.ConsoleHostStart},
		{"IOLBOX_CAPTURE_HOST_START", &opts.CaptureHostStart},
	} {
		if value := os.Getenv(setting.name); value != "" {
			port, err := strconv.Atoi(value)
			if err != nil || port < 1 || port > 65535 {
				return darwinOptions{}, fmt.Errorf("invalid %s %q", setting.name, value)
			}
			*setting.dest = port
		}
	}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		opts.Command = args[0]
		args = args[1:]
	}
	validCommands := map[string]bool{"start": true, "stop": true, "status": true, "diagnose": true, "upgrade": true}
	if !validCommands[opts.Command] {
		return darwinOptions{}, fmt.Errorf("unknown command %q", opts.Command)
	}
	fs := flag.NewFlagSet("iolbox-launcher", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&opts.Profile, "profile", opts.Profile, "profile name")
	fs.StringVar(&opts.Machine, "machine", opts.Machine, "Lima machine name")
	fs.StringVar(&opts.AssetsDir, "assets-dir", "", "M1 macOS asset root")
	fs.StringVar(&opts.Limactl, "limactl", opts.Limactl, "limactl executable")
	fs.StringVar(&opts.Tarball, "tarball", opts.Tarball, "payload tarball")
	fs.DurationVar(&opts.BootTimeout, "boot-timeout", opts.BootTimeout, "readiness timeout")
	fs.BoolVar(&opts.NoBrowser, "no-browser", false, "do not open the browser")
	fs.BoolVar(&opts.NoSync, "no-sync", false, "disable macOS host folder sync")
	fs.StringVar(&opts.ImagesDir, "images-dir", "", "override the macOS images folder")
	fs.StringVar(&opts.LabsDir, "labs-dir", "", "override the macOS labs folder")
	fs.IntVar(&opts.ConsoleHostStart, "console-host-start", opts.ConsoleHostStart, "host port for guest console range 9000-9049")
	fs.IntVar(&opts.CaptureHostStart, "capture-host-start", opts.CaptureHostStart, "host port for guest capture range 5500-5529")
	if err := fs.Parse(args); err != nil {
		return darwinOptions{}, err
	}
	if fs.NArg() != 0 {
		return darwinOptions{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if _, err := newDarwinPortContractWithRanges(opts.GUIPort, opts.ConsoleHostStart, opts.CaptureHostStart); err != nil {
		return darwinOptions{}, err
	}
	return opts, nil
}

func resolveAssetRoot(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(filepath.Join(explicit, "lima", "profiles.env")); err != nil {
			return "", fmt.Errorf("assets directory is missing lima/profiles.env: %s", explicit)
		}
		return filepath.Abs(explicit)
	}
	var candidates []string
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "packaging", "macos"), cwd)
	}
	if executable, err := os.Executable(); err == nil {
		dir := filepath.Dir(executable)
		candidates = append(candidates, filepath.Join(dir, "packaging", "macos"), filepath.Join(dir, "..", "..", "packaging", "macos"), dir)
	}
	for _, candidate := range candidates {
		candidate, _ = filepath.Abs(candidate)
		if _, err := os.Stat(filepath.Join(candidate, "lima", "profiles.env")); err == nil {
			return candidate, nil
		}
	}
	return "", errors.New("could not resolve macOS asset root; use --assets-dir")
}

func exitCode(err error) int {
	if err == nil {
		return exitOK
	}
	var coded *launcherError
	if errors.As(err, &coded) {
		return coded.code
	}
	return exitUsage
}

func runDarwinCLI(args []string) int {
	opts, err := parseDarwinArgs(args, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "iolbox-launcher:", err)
		return exitUsage
	}
	assetRoot, err := resolveAssetRoot(opts.AssetsDir)
	if err != nil {
		if opts.Command == "diagnose" {
			fmt.Println("iolbox diagnose")
			fmt.Println("  assets:", err)
			return exitOK
		}
		fmt.Fprintln(os.Stderr, "iolbox-launcher:", err)
		return exitUsage
	}
	// Resolve the logical profile SELECTION (auto/rosetta-amd64/native-arm64,
	// or a legacy direct profile-table row name) before loading the concrete
	// profileTable row. This is where the plan's explicit-flag > persisted >
	// auto precedence and native-arm64's fail-closed preflight/fallback
	// live; loadMacOSProfile below only ever sees a concrete row name.
	earlyTable, err := loadProfileTableOnly(assetRoot)
	if err != nil {
		if opts.Command == "diagnose" {
			fmt.Println("iolbox diagnose")
			fmt.Println("  profile:", err)
			return exitOK
		}
		fmt.Fprintln(os.Stderr, "iolbox-launcher:", err)
		return exitCode(err)
	}
	preflightFacts := collectHostFacts(context.Background())
	preflightLimactl, _ := discoverLimactl(opts.Limactl, os.Getenv("LIMACTL"), nil)
	selection, err := resolveProfileSelection(context.Background(), opts.Profile, earlyTable, preflightFacts, preflightLimactl, assetRoot, nil, testPreferNativeFromEnv(nil))
	if err != nil {
		if opts.Command == "diagnose" {
			fmt.Println("iolbox diagnose")
			fmt.Println("  profile selection:", err)
			return exitOK
		}
		fmt.Fprintln(os.Stderr, "iolbox-launcher:", err)
		return exitPreflight
	}
	if selection.FallbackReason != "" {
		fmt.Fprintf(os.Stderr, "iolbox-launcher: requested profile %q fell back to %q (%s): %s\n", selection.Requested, selection.Selected, selection.Source, selection.FallbackReason)
	}
	table, profile, err := loadMacOSProfile(assetRoot, selection.ProfileName)
	if err != nil {
		if opts.Command == "diagnose" {
			fmt.Println("iolbox diagnose")
			fmt.Println("  profile:", err)
			return exitOK
		}
		fmt.Fprintln(os.Stderr, "iolbox-launcher:", err)
		return exitCode(err)
	}
	if opts.Machine == "" {
		opts.Machine = "iolbox-" + profile.Name
	}
	facts := preflightFacts // same collectHostFacts() call the selection resolution already ran
	if opts.Command == "diagnose" {
		return runDiagnose(opts, table, profile, facts, selection)
	}
	if opts.Command == "start" || opts.Command == "upgrade" {
		if facts.System != "Darwin" || facts.Arch != "arm64" {
			fmt.Fprintf(os.Stderr, "Apple Silicon macOS is required; detected %s/%s\n", facts.System, facts.Arch)
			return exitPreflight
		}
		if facts.Product == "" || facts.Build == "" {
			fmt.Fprintln(os.Stderr, "could not read macOS product/build with sw_vers")
			return exitPreflight
		}
	}
	qualification := qualificationFor(table, profile.Name, facts.Product, facts.Build)
	fmt.Printf("profile=%s role=%s guest=%s qualification=%s\n", profile.Name, profile.Role, profile.GuestLabel, qualification.String())
	limactlPath, err := discoverLimactl(opts.Limactl, os.Getenv("LIMACTL"), nil)
	if err != nil {
		if opts.Command == "status" {
			fmt.Fprintln(os.Stderr, err)
		}
		return exitPreflight
	}
	client, info, err := limaClientFor(context.Background(), limactlPath, nil)
	if err != nil {
		return exitCode(err)
	}
	var syncConfig darwinSyncConfig
	if opts.Command == "start" || opts.Command == "upgrade" || opts.Command == "stop" {
		syncConfig, err = resolveDarwinSyncConfig(opts.NoSync, opts.ImagesDir, opts.LabsDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "iolbox-launcher:", err)
			return exitPreflight
		}
	}
	switch opts.Command {
	case "start", "upgrade":
		if facts.FreeDiskErr != nil || facts.FreeDiskKB < minFreeDiskGiB*1024*1024 {
			fmt.Fprintf(os.Stderr, "free disk is below the %d GiB minimum\n", minFreeDiskGiB)
			return exitPreflight
		}
		payload, err := selectPayload(opts.Tarball, assetRoot)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitCode(err)
		}
		ports, portErr := newDarwinPortContractWithRanges(opts.GUIPort, opts.ConsoleHostStart, opts.CaptureHostStart)
		if portErr != nil {
			fmt.Fprintln(os.Stderr, portErr)
			return exitUsage
		}
		err = runProvision(context.Background(), client, opts.Machine, profile, facts, payload, lifecycleConfig{
			Bind: opts.Bind, GUIPort: opts.GUIPort, Ports: ports, BootTimeout: opts.BootTimeout, Upgrade: opts.Command == "upgrade", Sync: &syncConfig,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "iolbox-launcher:", err)
			return exitCode(err)
		}
		if opts.Command == "start" && !opts.NoBrowser {
			openBrowser(fmt.Sprintf("http://127.0.0.1:%d/", opts.GUIPort))
		}
		return exitOK
	case "stop":
		machines, err := client.list(context.Background())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitPreflight
		}
		state, _ := findMachine(machines, opts.Machine)
		if err := runDarwinStop(context.Background(), client, opts.Machine, state, syncConfig, opts.BootTimeout, func() (controlClient, func(), error) {
			control, err := dialControlWS(fmt.Sprintf("127.0.0.1:%d", opts.GUIPort))
			if err != nil {
				return nil, nil, err
			}
			return &wsControlClient{ws: control}, func() {
				_ = control.Close()
			}, nil
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitCode(err)
		}
		return exitOK
	case "status":
		return runStatus(client, opts, table, profile, facts, info, selection)
	default:
		return exitUsage
	}
}

func runStatus(client *limaClient, opts darwinOptions, table profileTable, profile macOSProfile, facts hostFacts, info limaInfo, selection profileSelectionResult) int {
	machines, err := client.list(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitPreflight
	}
	state, exists := findMachine(machines, opts.Machine)
	fmt.Println("iolbox status")
	ports, portErr := newDarwinPortContractWithRanges(opts.GUIPort, opts.ConsoleHostStart, opts.CaptureHostStart)
	if portErr == nil {
		fmt.Printf("  GUI port: 127.0.0.1:%d\n  console ports: 127.0.0.1:%d-%d\n  capture ports: 127.0.0.1:%d-%d\n  guest control port: not forwarded (127.0.0.1:%d host check)\n", ports.GUIPort, ports.ConsoleHostStart, ports.ConsoleHostStart+darwinConsoleEnd-darwinConsoleStart, ports.CaptureHostStart, ports.CaptureHostStart+darwinCaptureEnd-darwinCaptureStart, darwinControlPort)
	}
	printDarwinSyncFacts(opts)
	q := qualificationFor(table, profile.Name, facts.Product, facts.Build)
	fmt.Printf("  profile: %s\n  role: %s\n  guest: %s\n  kernel pin: %s\n  macOS: %s\n  host_arch: %s\n  qualification: %s\n  lima: %s (%s)\n  machine: %s\n  state: %s\n", profile.Name, profile.Role, profile.GuestLabel, profile.ExpectedUnameR, hostMACOSString(facts), facts.Arch, q.String(), info.Version, info.Path, opts.Machine, map[bool]string{true: state, false: "not created"}[exists])
	fmt.Println("  last canary:")
	for _, line := range strings.Split(readCanaryState(opts.Machine), "\n") {
		fmt.Println("    " + line)
	}
	var helloFn func() (helloResult, error)
	if exists && strings.EqualFold(state, "running") {
		helloFn = func() (helloResult, error) {
			control, err := dialControlWS(fmt.Sprintf("127.0.0.1:%d", opts.GUIPort))
			if err != nil {
				return helloResult{}, err
			}
			defer control.Close()
			return control.hello()
		}
	}
	printDiagnosticSummary(os.Stdout, collectDarwinDiagnostics(context.Background(), client, opts.Machine, state, profile, facts, info, diagnosticsOptions{GUIPort: opts.GUIPort, hello: helloFn, Selection: selection}))
	if !exists || !strings.EqualFold(state, "running") {
		fmt.Println("  guest kernel: unavailable (machine is not running)")
		return exitOK
	}
	fmt.Println("  guest kernel:", guestValue(context.Background(), client, opts.Machine, "uname", "-r"))
	fmt.Println("  guest arch:", guestValue(context.Background(), client, opts.Machine, "uname", "-m"))
	fmt.Println("  Rosetta binfmt:", guestValue(context.Background(), client, opts.Machine, "sudo", "-n", "cat", "/proc/sys/fs/binfmt_misc/rosetta"))
	fmt.Println("  supervisor service:", guestValue(context.Background(), client, opts.Machine, "sudo", "-n", "systemctl", "is-active", "iolbox-supervisor.service"))
	fmt.Println("  GET /:", guestValue(context.Background(), client, opts.Machine, "sudo", "-n", "curl", "--silent", "--show-error", "--output", "/dev/null", "--write-out", "%{http_code}", fmt.Sprintf("http://127.0.0.1:%d/", darwinGUIGuestPort)))
	return exitOK
}

func runDiagnose(opts darwinOptions, table profileTable, profile macOSProfile, facts hostFacts, selection profileSelectionResult) int {
	fmt.Println("iolbox diagnose")
	if ports, err := newDarwinPortContractWithRanges(opts.GUIPort, opts.ConsoleHostStart, opts.CaptureHostStart); err == nil {
		fmt.Printf("port contract: GUI=127.0.0.1:%d consoles=127.0.0.1:%d-%d captures=127.0.0.1:%d-%d guest-control=not-forwarded\n", ports.GUIPort, ports.ConsoleHostStart, ports.ConsoleHostStart+darwinConsoleEnd-darwinConsoleStart, ports.CaptureHostStart, ports.CaptureHostStart+darwinCaptureEnd-darwinCaptureStart)
	}
	printDarwinSyncFacts(opts)
	q := qualificationFor(table, profile.Name, facts.Product, facts.Build)
	fmt.Printf("profile: %s (%s)\nguest: %s\nkernel pin: %s\nqualification: %s\nmacOS product/build: %s\nhost arch: %s\n", profile.Name, profile.Role, profile.GuestLabel, profile.ExpectedUnameR, q.String(), hostMACOSString(facts), facts.Arch)
	if facts.FreeDiskErr != nil {
		fmt.Println("free disk: unavailable (", facts.FreeDiskErr, ")")
	} else {
		fmt.Printf("free disk: %.2f GiB\n", float64(facts.FreeDiskKB)/1048576)
	}
	path, err := discoverLimactl(opts.Limactl, os.Getenv("LIMACTL"), nil)
	if err != nil {
		fmt.Println("lima executable: unavailable (", err, ")")
		printDiagnosticRemediation(opts.Machine)
		return exitOK
	}
	client, info, err := limaClientFor(context.Background(), path, nil)
	if err != nil {
		fmt.Println("lima:", err)
		return exitOK
	}
	fmt.Printf("lima executable: %s\nlima raw version: %s\nlima version: %s\nIOLBOX_HOST_LIMA=%s\n", info.Path, strings.TrimSpace(info.RawVersion), info.Version, info.Version)
	machines, err := client.list(context.Background())
	if err != nil {
		fmt.Println("machine listing: unavailable (", err, ")")
	} else {
		fmt.Println("machine listing:")
		for _, machine := range machines {
			fmt.Printf("  %s|%s\n", machine.Name, machine.State)
		}
		state, exists := findMachine(machines, opts.Machine)
		if !exists {
			fmt.Println("selected machine: not created")
		} else {
			fmt.Println("selected machine state:", state)
			if strings.EqualFold(state, "running") {
				fmt.Println("guest kernel:", guestValue(context.Background(), client, opts.Machine, "uname", "-r"))
				fmt.Println("guest arch:", guestValue(context.Background(), client, opts.Machine, "uname", "-m"))
				fmt.Println("Rosetta binfmt:", guestValue(context.Background(), client, opts.Machine, "sudo", "-n", "cat", "/proc/sys/fs/binfmt_misc/rosetta"))
				fmt.Println("supervisor service:", guestValue(context.Background(), client, opts.Machine, "sudo", "-n", "systemctl", "is-active", "iolbox-supervisor.service"))
				fmt.Println("GET /:", guestValue(context.Background(), client, opts.Machine, "sudo", "-n", "curl", "--silent", "--show-error", "--output", "/dev/null", "--write-out", "%{http_code}", fmt.Sprintf("http://127.0.0.1:%d/", darwinGUIGuestPort)))
				attestationPath, _ := hostAttestationPath(opts.Machine)
				if err := requireAttestation(attestationPath, profile, facts, info.Version); err != nil {
					fmt.Println("host attestation: INVALID (", err, ")")
				} else {
					fmt.Println("host attestation: valid")
				}
				fmt.Println("guest structural-gate drop-in:", guestValue(context.Background(), client, opts.Machine, "sudo", "-n", "cat", expectedCanaryDropIn))
				env := guestEnvironment(profile, facts, info.Version, opts.Machine, "unknown.tar.gz", lifecycleConfig{Bind: opts.Bind, GUIPort: opts.GUIPort})
				canaryOut, canaryErr := client.shell(context.Background(), opts.Machine, guestStepArgs(profile.canaryStep(), env, "--quiet")...)
				if canaryErr != nil {
					fmt.Printf("live canary: FAIL (%v): %s\n", canaryErr, strings.TrimSpace(string(canaryOut)))
				} else {
					fmt.Println("live canary: PASS")
				}
				control, helloErr := dialControlWS(fmt.Sprintf("127.0.0.1:%d", opts.GUIPort))
				if helloErr != nil {
					fmt.Println("supervisor hello: unavailable (", helloErr, ")")
				} else {
					defer control.Close()
					hello, helloErr := control.hello()
					if helloErr != nil {
						fmt.Println("supervisor hello: unavailable (", helloErr, ")")
					} else {
						fmt.Printf("supervisor=%s runtime=%s arch=%s features=%v egress=%s\n", hello.Supervisor, hello.Runtime, hello.Arch, hello.Features, hello.Egress)
					}
				}
			}
		}
	}
	state, exists := findMachine(machines, opts.Machine)
	var helloFn func() (helloResult, error)
	if exists && strings.EqualFold(state, "running") {
		helloFn = func() (helloResult, error) {
			control, err := dialControlWS(fmt.Sprintf("127.0.0.1:%d", opts.GUIPort))
			if err != nil {
				return helloResult{}, err
			}
			defer control.Close()
			return control.hello()
		}
	}
	printDiagnosticSummary(os.Stdout, collectDarwinDiagnostics(context.Background(), client, opts.Machine, state, profile, facts, info, diagnosticsOptions{GUIPort: opts.GUIPort, hello: helloFn, Selection: selection}))
	printDiagnosticRemediation(opts.Machine)
	return exitOK
}

func printDarwinSyncFacts(opts darwinOptions) {
	if opts.NoSync {
		fmt.Println("sync: disabled")
		return
	}
	config, err := resolveDarwinSyncConfig(false, opts.ImagesDir, opts.LabsDir)
	if err != nil {
		fmt.Println("sync: unavailable (", err, ")")
		return
	}
	fmt.Printf("sync images: %s\nsync labs: %s\n", config.ImagesDir, config.LabsDir)
}

func printDiagnosticRemediation(machine string) {
	fmt.Println("last canary:")
	fmt.Println("  " + readCanaryState(machine))
	if warning, ok := hostAgentWarningText(machine); ok {
		fmt.Println("hostagent Rosetta warning:")
		fmt.Println(warning)
	} else {
		fmt.Println("hostagent Rosetta warning: none found or log unavailable")
	}
	fmt.Println("remediation: brew reinstall lima")
	fmt.Println("brew upgrade lima may be a no-op when the current version is already installed; Lima READY is not proof Rosetta was mounted")
}
