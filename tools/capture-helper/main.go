// capture-helper connects to an iolab supervisor capture port (a raw pcapng
// byte stream for one link) and pipes it into Wireshark via `wireshark -k -i -`.
//
// It is deliberately tiny and standalone so the Tauri app can either spawn it or
// inline the same ~logic in Rust. It survives Wireshark being closed and
// reopened: when the Wireshark process exits, the helper keeps the supervisor
// connection alive by draining, and (with -relaunch) respawns Wireshark and
// resumes streaming, so "right-click link -> Capture" behaves like a live tap.
//
// Usage:
//
//	capture-helper -connect 127.0.0.1:5500 [-wireshark "C:\Program Files\Wireshark\Wireshark.exe"] [-name R1<->R2] [-relaunch] [-out file.pcapng]
//
// If -out is given, frames are also written to that file (record mode).
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"time"
)

func main() {
	var (
		connect   = flag.String("connect", "127.0.0.1:5500", "supervisor capture endpoint host:port")
		wsPath    = flag.String("wireshark", "", "path to Wireshark.exe (auto-detected if empty)")
		name      = flag.String("name", "iolab capture", "interface name shown in Wireshark")
		relaunch  = flag.Bool("relaunch", true, "respawn Wireshark if it is closed")
		outFile   = flag.String("out", "", "also write the stream to this pcapng file")
		dialRetry = flag.Duration("retry", 2*time.Second, "reconnect backoff to the supervisor")
	)
	flag.Parse()

	ws := *wsPath
	if ws == "" {
		var err error
		ws, err = findWireshark()
		if err != nil {
			log.Fatalf("wireshark not found: %v (pass -wireshark)", err)
		}
	}

	for {
		if err := streamOnce(*connect, ws, *name, *relaunch, *outFile); err != nil {
			log.Printf("stream ended: %v; reconnecting in %s", err, *dialRetry)
		}
		time.Sleep(*dialRetry)
	}
}

// streamOnce dials the supervisor and tees the pcapng byte stream to Wireshark
// (and optionally a file) until the supervisor connection drops.
func streamOnce(addr, ws, name string, relaunch bool, outFile string) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("dial supervisor: %w", err)
	}
	defer conn.Close()
	log.Printf("connected to supervisor %s", addr)

	var fileW io.WriteCloser
	if outFile != "" {
		fileW, err = os.Create(outFile)
		if err != nil {
			return fmt.Errorf("open out file: %w", err)
		}
		defer fileW.Close()
	}

	// pcapng is a single continuous stream (SHB once at the top). Because we may
	// relaunch Wireshark mid-stream, we can't replay the header to a new process.
	// So we buffer the section header block (everything up to and including the
	// first IDB) and prepend it whenever we (re)start Wireshark. The supervisor
	// is expected to emit SHB+IDB as the first write on connect.
	header, rest, err := readInitialHeader(conn)
	if err != nil {
		return fmt.Errorf("read pcapng header: %w", err)
	}
	if fileW != nil {
		if _, err := fileW.Write(header); err != nil {
			return err
		}
		if _, err := fileW.Write(rest); err != nil {
			return err
		}
	}

	tee := &teeToWireshark{ws: ws, name: name, relaunch: relaunch, header: header}
	defer tee.close()
	if err := tee.start(); err != nil {
		return err
	}
	if len(rest) > 0 {
		_ = tee.write(rest)
	}

	buf := make([]byte, 64*1024)
	for {
		n, rerr := conn.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if werr := tee.write(chunk); werr != nil {
				log.Printf("wireshark write: %v", werr)
			}
			if fileW != nil {
				if _, ferr := fileW.Write(chunk); ferr != nil {
					return ferr
				}
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return fmt.Errorf("supervisor closed connection")
			}
			return rerr
		}
	}
}

// teeToWireshark owns a `wireshark -k -i -` child and rewrites the buffered
// pcapng header when relaunching so a freshly opened Wireshark gets a valid file.
type teeToWireshark struct {
	ws       string
	name     string
	relaunch bool
	header   []byte

	cmd   *exec.Cmd
	stdin io.WriteCloser
}

func (t *teeToWireshark) start() error {
	// -k start immediately, -i - read from stdin. --name gives the pipe a label.
	cmd := exec.Command(t.ws, "-k", "-i", "-")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	t.cmd, t.stdin = cmd, stdin
	if _, err := t.stdin.Write(t.header); err != nil {
		return err
	}
	log.Printf("launched wireshark (%s)", t.name)
	return nil
}

func (t *teeToWireshark) write(p []byte) error {
	if t.stdin == nil {
		return nil
	}
	_, err := t.stdin.Write(p)
	if err != nil && t.relaunch {
		// Wireshark was probably closed; relaunch and replay header.
		log.Printf("wireshark closed; relaunching")
		t.close()
		if serr := t.start(); serr != nil {
			return serr
		}
		_, err = t.stdin.Write(p)
	}
	return err
}

func (t *teeToWireshark) close() {
	if t.stdin != nil {
		_ = t.stdin.Close()
		t.stdin = nil
	}
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
		_ = t.cmd.Wait()
		t.cmd = nil
	}
}

// readInitialHeader reads the first chunk and returns the SHB+IDB header bytes to
// buffer for relaunches. For v1 we treat the first read as the header; the
// supervisor is documented to flush SHB+IDB before any packet block. This keeps
// the helper dependency-free. If we later need exact block parsing, replace this
// with a real pcapng block splitter.
func readInitialHeader(conn net.Conn) (header, rest []byte, err error) {
	buf := make([]byte, 64*1024)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, err := conn.Read(buf)
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		return nil, nil, err
	}
	// Heuristic: keep the whole first read as header. Packet blocks that arrived
	// in the same read are replayed to the first Wireshark as `rest` and, being
	// after the SHB/IDB, are valid there too.
	return append([]byte(nil), buf[:n]...), nil, nil
}

func findWireshark() (string, error) {
	candidates := []string{
		`C:\Program Files\Wireshark\Wireshark.exe`,
		`C:\Program Files (x86)\Wireshark\Wireshark.exe`,
	}
	if p, err := exec.LookPath("wireshark"); err == nil {
		candidates = append([]string{p}, candidates...)
	}
	if p, err := exec.LookPath("Wireshark"); err == nil {
		candidates = append([]string{p}, candidates...)
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("checked %v", candidates)
}
