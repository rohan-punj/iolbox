//go:build linux

package bcap

import (
	"bytes"
	"io"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// newTestCapture constructs a Capture around an already-started cmd,
// bypassing Start() so these tests don't need real sudo/tcpdump — just the
// same process-group relationship Start's `sudo -n tcpdump ...` has: a
// direct child (here `sh`) that forks a longer-lived grandchild sharing its
// stdout pipe, then exits quickly itself.
func newTestCapture(t *testing.T, cmd *exec.Cmd) *Capture {
	t.Helper()
	srv, err := newPcapngServer("127.0.0.1", 0)
	if err != nil {
		t.Fatalf("newPcapngServer: %v", err)
	}
	return &Capture{bridge: "test0", server: srv, cmd: cmd, protos: map[string]uint64{}}
}

// TestClose_KillsWholeProcessGroupNotJustDirectChild is the regression test
// for finding #13: Close() used to kill only cmd.Process (the direct
// child — sudo, in production) and then block in cmd.Wait() forever,
// because a grandchild (tcpdump) inherited the stdout pipe and kept it
// open after being orphaned. `sh -c "sleep 30 & exit 0"` reproduces the
// same shape: sh exits immediately, sleep lives on sharing sh's stdout pipe
// and (via Setpgid) its process group. If Close() kills only sh, this test
// would hang for the full closeWaitTimeout; the group kill should let it
// return almost immediately instead.
func TestClose_KillsWholeProcessGroupNotJustDirectChild(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30 & exit 0")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	// The real bug's trigger: cmd.Stderr set to a non-*os.File (a
	// *bytes.Buffer, same as Start's production cmd.Stderr = &stderr) forces
	// Go's os/exec to fork an internal stderr-copy goroutine backed by an OS
	// pipe. Cmd.Wait() blocks on that goroutine finishing, which requires
	// EVERY process holding the pipe's write end to close it — including any
	// orphaned grandchild that inherited it. Without this, Wait() has
	// nothing to wait for and this test can't reproduce the hang at all.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	go func() { _, _ = io.Copy(io.Discard, stdout) }()

	c := newTestCapture(t, cmd)

	done := make(chan error, 1)
	go func() { done <- c.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Close did not return promptly — the sleep grandchild was not killed with the group (fix regressed)")
	}
}

// TestClose_TimesOutRatherThanHangingForever is the self-heal backstop test:
// even when the group kill can't reach every descendant (here simulated by
// escaping into a new session via setsid, the same trick a misbehaving or
// unusual tcpdump build could use), Close() must still return within
// closeWaitTimeout instead of blocking its caller — and therefore the
// server's serializedHandler lock — forever.
func TestClose_TimesOutRatherThanHangingForever(t *testing.T) {
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("setsid not available on this host")
	}
	cmd := exec.Command("sh", "-c", "setsid sleep 30 & exit 0")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	// The real bug's trigger: cmd.Stderr set to a non-*os.File (a
	// *bytes.Buffer, same as Start's production cmd.Stderr = &stderr) forces
	// Go's os/exec to fork an internal stderr-copy goroutine backed by an OS
	// pipe. Cmd.Wait() blocks on that goroutine finishing, which requires
	// EVERY process holding the pipe's write end to close it — including any
	// orphaned grandchild that inherited it. Without this, Wait() has
	// nothing to wait for and this test can't reproduce the hang at all.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	go func() { _, _ = io.Copy(io.Discard, stdout) }()

	c := newTestCapture(t, cmd)

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- c.Close() }()

	select {
	case <-done:
		elapsed := time.Since(start)
		if elapsed > closeWaitTimeout+2*time.Second {
			t.Fatalf("Close took %s, want at most ~%s (timeout backstop)", elapsed, closeWaitTimeout)
		}
	case <-time.After(closeWaitTimeout + 5*time.Second):
		t.Fatal("Close hung well past closeWaitTimeout — the self-heal backstop regressed")
	}
}
