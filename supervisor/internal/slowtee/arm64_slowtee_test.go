//go:build linux

package slowtee

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

const slowteeHelperEnv = "IOLBOX_ARM64_SLOWTEE_HELPER"

// TestArm64SlowteeStream uses the actual test binary as an exec'd helper so
// the stream path crosses a process boundary. The helper reads a real FIFO and
// tees deterministic multi-chunk input to two real files; the parent observes
// both byte-for-byte outputs and the helper's zero exit status, then removes
// every temporary object.
func TestArm64SlowteeStream(t *testing.T) {
	if os.Getenv(slowteeHelperEnv) == "1" {
		slowteeStreamHelper()
		return
	}

	dir := t.TempDir()
	fifo := dir + "/input.fifo"
	sinkA := dir + "/sink-a"
	sinkB := dir + "/sink-b"
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("FIFO unavailable: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run", "^TestArm64SlowteeStream$", "-test.v")
	cmd.Env = append(os.Environ(),
		slowteeHelperEnv+"=1",
		"IOLBOX_ARM64_SLOWTEE_FIFO="+fifo,
		"IOLBOX_ARM64_SLOWTEE_SINK_A="+sinkA,
		"IOLBOX_ARM64_SLOWTEE_SINK_B="+sinkB,
	)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot exec stream helper: %v", err)
	}
	finished := false
	t.Cleanup(func() {
		if !finished {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	writer, err := os.OpenFile(fifo, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open FIFO writer: %v", err)
	}
	chunks := [][]byte{
		[]byte("arm64/phase1:"),
		[]byte{0x00, 0x01, 0x02, 0x7f, 0x80, 0xff},
		[]byte("chunk-three\n"),
	}
	want := bytes.Join(chunks, nil)
	for i, chunk := range chunks {
		n, err := writer.Write(chunk)
		if err != nil {
			_ = writer.Close()
			t.Fatalf("write chunk %d: %v", i, err)
		}
		if n != len(chunk) {
			_ = writer.Close()
			t.Fatalf("write chunk %d wrote %d/%d bytes", i, n, len(chunk))
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close FIFO writer: %v", err)
	}

	if err := cmd.Wait(); err != nil {
		t.Fatalf("stream helper exit = %v, want zero", err)
	}
	finished = true
	for _, path := range []string{sinkA, sinkB} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s = %x, want %x", path, got, want)
		}
	}

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove stream temp dir: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("stream temp dir remains after cleanup: %v", err)
	}
}

func slowteeStreamHelper() {
	fifo := os.Getenv("IOLBOX_ARM64_SLOWTEE_FIFO")
	sinkA := os.Getenv("IOLBOX_ARM64_SLOWTEE_SINK_A")
	sinkB := os.Getenv("IOLBOX_ARM64_SLOWTEE_SINK_B")
	in, err := os.Open(fifo)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer in.Close()
	outA, err := os.Create(sinkA)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer outA.Close()
	outB, err := os.Create(sinkB)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer outB.Close()
	if _, err := io.Copy(io.MultiWriter(outA, outB), in); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
