// Command supervisor is the iolbox control + data plane daemon. It runs inside
// the Linux runtime (WSL2 / VMware helper VM / remote / qemu) and speaks the
// NDJSON control protocol (see docs/protocol.md) to the Windows GUI over a
// loopback TCP connection.
package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/rohanpunj/iolbox/supervisor/internal/iourc"
	"github.com/rohanpunj/iolbox/supervisor/internal/server"
	"github.com/rohanpunj/iolbox/supervisor/internal/wsbridge"
)

// version is the supervisor build version reported in the hello handshake.
// Overridden at build time via -ldflags "-X main.version=$(git describe ...)"
// (see build-release.sh); "0.1.0" only shows up in an unstamped dev build.
var version = "0.1.0"

func disableI386FromEnv(value string) bool {
	return value == "1"
}

func main() {
	controlAddr := flag.String("control-addr", "127.0.0.1:4000", "control API bind address (loopback only)")
	wsAddr := flag.String("ws-addr", "127.0.0.1:4001", "WebSocket bridge + GUI bind address (control + console over WS and the embedded browser GUI; use 0.0.0.0:4001 for browser access from the host; empty disables it)")
	imageDir := flag.String("image-dir", "/opt/iolbox/images", "directory holding IOL image files")
	runDir := flag.String("run-dir", "/run/iolbox", "base directory for per-lab working directories")
	labsDir := flag.String("labs-dir", "/opt/iolbox/labs", "directory for the durable lab-document store (lab.saveDoc/listDocs/getDoc/deleteDoc)")
	iourcPath := flag.String("iourc", "/opt/iolbox/iourc", "IOU license file copied into each lab's shared dir (generated at firstboot by -gen-iourc)")
	consoleBind := flag.String("console-bind", "127.0.0.1", "host the per-node IOL console listeners bind; 0.0.0.0 lets a native telnet client on the GUI host dial <vm-ip>:<consolePort>")
	captureBind := flag.String("capture-bind", "127.0.0.1", "host each link's pcapng capture tee listener binds; 0.0.0.0 lets a native Wireshark on the GUI host attach with `wireshark -k -i TCP@<vm-ip>:<capturePort>`")
	egressMode := flag.String("egress", "auto", "NAT internet-egress capability advertised in hello: auto (detect QEMU slirp signature), slirp (force ICMP-limited: DHCP/TCP only), or routed (force full ICMP/traceroute)")
	genIourc := flag.Bool("gen-iourc", false, "generate the IOU license file to stdout from this host's hostid+hostname, then exit (used by the runtime firstboot script)")
	showVersion := flag.Bool("version", false, "print the build version and exit (used by the console banner generator)")
	flag.Parse()

	// Version-only mode: print the build version and exit without starting the
	// server. The runtime's console-banner generator (iolbox-issue.sh) reads
	// this; keep it side-effect-free so it's safe to call on a live appliance.
	if *showVersion {
		fmt.Println(version)
		return
	}

	// Keygen-only mode: print ~/.iourc content and exit without starting the
	// server. The runtime's firstboot-iourc.sh relies on this exact flag.
	if *genIourc {
		if err := generateIourc(os.Stdout); err != nil {
			log.Fatalf("supervisor -gen-iourc: %v", err)
		}
		return
	}

	srv := server.New(server.Config{
		ControlAddr: *controlAddr,
		ImageDir:    *imageDir,
		RunDir:      *runDir,
		LabsDir:     *labsDir,
		IourcPath:   *iourcPath,
		ConsoleBind: *consoleBind,
		CaptureBind: *captureBind,
		Version:     version,
		DisableI386: disableI386FromEnv(os.Getenv("IOLBOX_DISABLE_I386")),
		Egress:      *egressMode,
	})
	if err := srv.InitRuntime(); err != nil {
		log.Printf("supervisor: tool runtime unavailable: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("iolbox supervisor %s listening on %s (images=%s run=%s)", version, *controlAddr, *imageDir, *runDir)
		if err := srv.ListenAndServe(ctx); err != nil {
			errCh <- fmt.Errorf("control listener: %w", err)
		}
	}()

	if *wsAddr != "" {
		bridge := wsbridge.New(wsbridge.Config{Addr: *wsAddr, ImageDir: *imageDir}, srv)
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("iolbox supervisor ws bridge listening on %s (/control, /console/{nodeId})", *wsAddr)
			if err := bridge.ListenAndServe(ctx); err != nil {
				errCh <- fmt.Errorf("ws bridge: %w", err)
			}
		}()
	} else {
		log.Printf("iolbox supervisor ws bridge disabled (-ws-addr empty)")
	}

	wg.Wait()
	// Stop immediately: a defer would be skipped if the error drain below calls log.Fatalf.
	srv.StopRuntime()
	close(errCh)
	for err := range errCh {
		if err != nil {
			log.Fatalf("supervisor: %v", err)
		}
	}
	log.Printf("iolbox supervisor shut down cleanly")
}

// generateIourc writes the ~/.iourc license file content for this host to w.
func generateIourc(w io.Writer) error {
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("hostname: %w", err)
	}
	hostid, err := hostID()
	if err != nil {
		return err
	}
	content, err := iourc.File(hostid, hostname)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, content)
	return err
}

// hostID returns the 8-hex-digit host id used as the keygen input, matching the
// output of the Linux `hostid` command (the community keygen's expected input).
// Falls back to reading /etc/hostid (4 bytes, host-endian) if the command is
// unavailable.
func hostID() (string, error) {
	if out, err := exec.Command("hostid").Output(); err == nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			return s, nil
		}
	}
	if b, err := os.ReadFile("/etc/hostid"); err == nil && len(b) >= 4 {
		return fmt.Sprintf("%08x", binary.LittleEndian.Uint32(b[:4])), nil
	}
	return "", fmt.Errorf("could not determine hostid (no `hostid` command and no /etc/hostid)")
}
