// tool-stubgui is the tiny AF_UNIX HTTP server used by the P0 spike and later
// lifecycle tests. It deliberately has no pack behavior: it only proves that
// a tool GUI can bind its socket, read/write its options file, and answer a
// health request from the root namespace.
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--grandchild" {
		for {
			select {}
		}
	}

	socketPath := os.Getenv("IOLBOX_TOOL_SOCK")
	optionsPath := os.Getenv("IOLBOX_TOOL_OPTIONS")
	if socketPath == "" || optionsPath == "" {
		fail("IOLBOX_TOOL_SOCK and IOLBOX_TOOL_OPTIONS are required")
	}

	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		fail("create socket parent: %v", err)
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		fail("remove stale socket: %v", err)
	}

	options, err := os.ReadFile(optionsPath)
	if err != nil {
		fail("read options file: %v", err)
	}
	// The append is intentional. It proves that the unprivileged GUI can both
	// read and write the same 0600 file owned by ioltool.
	if err := os.WriteFile(optionsPath, append(options, []byte("stubgui-started\n")...), 0o600); err != nil {
		fail("write options file: %v", err)
	}

	if statusPath := os.Getenv("IOLBOX_TOOL_STATUS_FILE"); statusPath != "" {
		status, err := os.ReadFile("/proc/self/status")
		if err != nil {
			fail("read process status: %v", err)
		}
		status = append(status, []byte(fmt.Sprintf("Uid=%d\nGid=%d\n", os.Getuid(), os.Getgid()))...)
		if err := os.WriteFile(statusPath, status, 0o644); err != nil {
			fail("write status file: %v", err)
		}
	}

	if grandchildPath := os.Getenv("IOLBOX_STUB_GRANDCHILD_PID_FILE"); grandchildPath != "" {
		startGrandchild(grandchildPath)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		fail("listen on %s: %v", socketPath, err)
	}
	defer listener.Close()
	if err := os.Chmod(socketPath, 0o600); err != nil {
		fail("chmod socket: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("iolbox tool stub gui\n"))
	})
	mux.HandleFunc("/send-arp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		iface := os.Getenv("IOLBOX_TOOL_IFACE")
		if iface == "" {
			iface = "eth1"
		}
		// The Python process inherits the GUI's netns and ambient NET_RAW. This
		// endpoint exists only for T0.9's bridge/capture proof; no pack uses it.
		const source = "from scapy.all import ARP, Ether, sendp\n" +
			"import os\n" +
			"iface = os.environ.get('IOLBOX_TOOL_IFACE', 'eth1')\n" +
			"sendp(Ether(dst='ff:ff:ff:ff:ff:ff')/ARP(op=1, pdst='198.18.0.1'), iface=iface, count=1, verbose=False)"
		if err := exec.Command("python3", "-c", source).Run(); err != nil {
			http.Error(w, fmt.Sprintf("send arp: %v", err), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("sent\n"))
	})

	if err := (&http.Server{Handler: mux}).Serve(listener); err != nil && !strings.Contains(err.Error(), "closed network connection") {
		fail("serve: %v", err)
	}
}

func startGrandchild(pidPath string) {
	child, err := os.StartProcess("/proc/self/exe", []string{"tool-stubgui", "--grandchild"}, &os.ProcAttr{
		Env:   os.Environ(),
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	})
	if err != nil {
		fail("start grandchild: %v", err)
	}
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", child.Pid)), 0o644); err != nil {
		_ = child.Kill()
		fail("write grandchild pid: %v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "tool-stubgui: "+format+"\n", args...)
	os.Exit(1)
}
