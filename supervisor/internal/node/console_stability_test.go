package node

import (
	"bytes"
	"context"
	"github.com/rohanpunj/iolbox/supervisor/internal/telnet"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

type batchReadResult struct {
	data []byte
	err  error
}

// Deterministic reads prove cap/deadline/error contracts without scheduler
// timing assumptions. net.Pipe below exercises actual poller timeouts.
type batchDeadlineReader struct {
	reads       []batchReadResult
	deadlines   []time.Time
	readCalls   int
	unsupported bool
}

func (r *batchDeadlineReader) SetReadDeadline(at time.Time) error {
	r.deadlines = append(r.deadlines, at)
	if r.unsupported {
		return os.ErrNoDeadline
	}
	return nil
}

func (r *batchDeadlineReader) Read(p []byte) (int, error) {
	r.readCalls++
	if len(r.reads) == 0 {
		return 0, io.EOF
	}
	result := &r.reads[0]
	n := copy(p, result.data)
	result.data = result.data[n:]
	if len(result.data) > 0 {
		return n, nil
	}
	err := result.err
	r.reads = r.reads[1:]
	return n, err
}

func TestConsoleReadBatchFixedDeadlineAndTimeoutRecovery(t *testing.T) {
	r := &batchDeadlineReader{reads: []batchReadResult{
		{data: []byte("one")}, {data: []byte("two")}, {err: os.ErrDeadlineExceeded},
		{data: []byte("next"), err: io.EOF},
	}}
	buf := make([]byte, consoleReadBatchLimit)
	n, at, err := readConsoleBatch(r, r, buf)
	if err != nil || string(buf[:n]) != "onetwo" {
		t.Fatalf("first batch %q, %v", buf[:n], err)
	}
	if len(r.deadlines) != 2 || !r.deadlines[0].IsZero() || !r.deadlines[1].Equal(at.Add(consoleReadBatchWindow)) {
		t.Fatalf("deadline must be set once from first read: %v, first=%v", r.deadlines, at)
	}
	n, _, err = readConsoleBatch(r, r, buf)
	if err != io.EOF || string(buf[:n]) != "next" {
		t.Fatalf("EOF batch %q, %v", buf[:n], err)
	}
	if len(r.deadlines) != 3 || !r.deadlines[2].IsZero() {
		t.Fatalf("next blocking read inherited deadline: %v", r.deadlines)
	}
}

func TestConsoleReadBatchCapAndEOFKeepByteOrder(t *testing.T) {
	want := bytes.Repeat([]byte("0123456789abcdef"), consoleReadBatchLimit/16+3)
	r := &batchDeadlineReader{reads: []batchReadResult{{data: want[:7]}, {data: want[7:], err: io.EOF}}}
	buf := make([]byte, consoleReadBatchLimit)
	n, _, err := readConsoleBatch(r, r, buf)
	if err != nil || n != consoleReadBatchLimit || !bytes.Equal(buf[:n], want[:n]) {
		t.Fatalf("cap batch bytes=%d err=%v", n, err)
	}
	got := append([]byte(nil), buf[:n]...)
	n, _, err = readConsoleBatch(r, r, buf)
	got = append(got, buf[:n]...)
	if err != io.EOF || !bytes.Equal(got, want) {
		t.Fatalf("EOF flush bytes=%d want=%d err=%v", len(got), len(want), err)
	}
}

func TestConsoleReadBatchNoDeadlineKeepsIndividualReads(t *testing.T) {
	r := &batchDeadlineReader{reads: []batchReadResult{{data: []byte("first")}, {data: []byte("second")}}}
	buf := make([]byte, 4096)
	n, _, err := readConsoleBatch(r, nil, buf)
	if err != nil || string(buf[:n]) != "first" || r.readCalls != 1 || len(r.deadlines) != 0 {
		t.Fatalf("unsupported reader must keep individual reads: bytes=%q calls=%d err=%v", buf[:n], r.readCalls, err)
	}
}

func TestConsoleReadBatchPipeIdleTimeoutAndNextRead(t *testing.T) {
	reader, writer := net.Pipe()
	defer reader.Close()
	defer writer.Close()
	read := func() <-chan batchReadResult {
		result := make(chan batchReadResult, 1)
		go func() {
			buf := make([]byte, consoleReadBatchLimit)
			n, _, err := readConsoleBatch(reader, reader, buf)
			result <- batchReadResult{data: append([]byte(nil), buf[:n]...), err: err}
		}()
		return result
	}
	first := read()
	if _, err := writer.Write([]byte("prompt#")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-first:
		if got.err != nil || string(got.data) != "prompt#" {
			t.Fatalf("idle flush %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("batch failed to flush at idle deadline")
	}
	next := read()
	select {
	case got := <-next:
		t.Fatalf("initial read did not block after clearing deadline: %+v", got)
	case <-time.After(10 * time.Millisecond):
	}
	if _, err := writer.Write([]byte("next#")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-next:
		if got.err != nil || string(got.data) != "next#" {
			t.Fatalf("next batch %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("next batch failed")
	}
}

func TestFragmentedPTYBurstDoesNotEvictHealthySubscriber(t *testing.T) {
	reader, writer := net.Pipe()
	defer reader.Close()
	defer writer.Close()
	h := newConsoleHub(reader, "")
	defer h.shutdown()
	sub := h.Subscribe()
	defer sub.Unsubscribe()
	want := bytes.Repeat([]byte("abcd"), 256)
	written := make(chan error, 1)
	go func() {
		for i := range want {
			if _, err := writer.Write(want[i : i+1]); err != nil {
				written <- err
				return
			}
		}
		written <- nil
	}()
	// Deliberately let the short burst fill the queue before draining it. Raw
	// single-byte reads would require 1024 queue slots and evict this client.
	select {
	case err := <-written:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("fragmented writer blocked")
	}
	var got []byte
	chunks := 0
	for len(got) < len(want) {
		select {
		case p, open := <-sub.Out:
			if !open {
				t.Fatalf("subscriber evicted after %d bytes", len(got))
			}
			if len(p) > consoleReadBatchLimit {
				t.Fatalf("oversized batch %d", len(p))
			}
			got = append(got, p...)
			chunks++
		case <-time.After(time.Second):
			t.Fatal("incomplete burst")
		}
	}
	if !bytes.Equal(got, want) {
		t.Fatal("fragmented bytes lost or reordered")
	}
	if chunks >= hubClientQueue {
		t.Fatalf("burst still used %d queue slots", chunks)
	}
}

func TestClientEnqueueConcurrentClose(t *testing.T) {
	for i := 0; i < 200; i++ {
		c := &hubClient{out: make(chan []byte, hubClientQueue), stop: make(chan struct{})}
		var wg sync.WaitGroup
		for n := 0; n < 4; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					c.enqueue([]byte("reply"))
				}
			}()
		}
		c.close()
		wg.Wait()
		if c.enqueue([]byte("late")) {
			t.Fatal("accepted after close")
		}
	}
}

func TestNativeFragmentedNegotiationDoesNotConsumePeerInput(t *testing.T) {
	pty, out, in := newFakePty()
	defer out.Close()
	defer in.Close()
	h := newConsoleHub(pty, "")
	defer h.shutdown()
	a, b := attachPipe(h), attachPipe(h)
	defer a.Close()
	defer b.Close()
	readWithDeadline(t, a, 20*time.Millisecond)
	readWithDeadline(t, b, 20*time.Millisecond)
	a.Write([]byte{telnet.IAC}) // leave A's parser waiting for a verb
	got := make(chan []byte, 1)
	go func() { p := make([]byte, 5); io.ReadFull(in, p); got <- p }()
	b.Write([]byte("hello"))
	select {
	case p := <-got:
		if string(p) != "hello" {
			t.Fatalf("peer input %q", p)
		}
	case <-time.After(time.Second):
		t.Fatal("peer input consumed by A's parser")
	}
	a.Write([]byte{telnet.WILL, telnet.OptSGA})
	collectUntil(t, a, []byte{telnet.IAC, telnet.DO, telnet.OptSGA})
}

type drainPty struct {
	mu      sync.Mutex
	bytes   bytes.Buffer
	started chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (p *drainPty) Read(b []byte) (int, error) { return 0, io.EOF }
func (p *drainPty) Write(b []byte) (int, error) {
	if string(b) == "queued" {
		p.once.Do(func() { close(p.started) })
		<-p.resume
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.bytes.Write(b)
}

func TestTurnDrainCannotBeOvertakenAndExpiredWriterRejected(t *testing.T) {
	p := &drainPty{started: make(chan struct{}), resume: make(chan struct{})}
	h := &consoleHub{pty: p, done: make(chan struct{})}
	release, id, err := h.claimTurn(context.Background(), "first")
	if err != nil {
		t.Fatal(err)
	}
	if err = h.gatedWrite([]byte("queued")); err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() { release(); close(released) }()
	<-p.started
	written := make(chan struct{})
	go func() { h.gatedWrite([]byte("new")); close(written) }()
	select {
	case <-written:
		t.Fatal("new input overtook queued drain")
	case <-time.After(20 * time.Millisecond):
	}
	claimed := make(chan func(), 1)
	go func() {
		r, _, e := h.claimTurn(context.Background(), "second")
		if e == nil {
			claimed <- r
		}
	}()
	select {
	case <-claimed:
		t.Fatal("claim overtook queued drain")
	case <-time.After(20 * time.Millisecond):
	}
	close(p.resume)
	<-released
	r := <-claimed
	r()
	<-written
	if err = h.writeTurn(context.Background(), id, []byte("stale")); err == nil {
		t.Fatal("expired holder wrote")
	}
	if got := p.bytes.String(); got != "queuednew" {
		t.Fatalf("write order %q", got)
	}
}

func TestLiveSubscriptionSkipsReplayAndSignalsEviction(t *testing.T) {
	pty, out, in := newFakePty()
	defer out.Close()
	defer in.Close()
	h := newConsoleHub(pty, "R1")
	defer h.shutdown()
	h.broadcast([]byte("cached\r\nR1#"))
	live := h.subscribe(false)
	defer live.Unsubscribe()
	select {
	case p := <-live.Out:
		t.Fatalf("live subscriber got replay/title %q", p)
	default:
	}
	normal := h.Subscribe()
	defer normal.Unsubscribe()
	<-normal.Out
	if p := <-normal.Out; !bytes.Contains(p, []byte("cached")) {
		t.Fatalf("normal replay %q", p)
	}
	for i := 0; i <= hubClientQueue; i++ {
		h.broadcast([]byte("x"))
	}
	select {
	case <-live.Done:
	case <-time.After(time.Second):
		t.Fatal("eviction not signaled")
	}
}

func TestRunExecDoesNotUseCachedUserPrompt(t *testing.T) {
	pty := newIOSPty("R1")
	defer pty.r.Close()
	defer pty.w.Close()
	pty.priv = true
	h := newConsoleHub(pty, "R1")
	defer h.shutdown()
	h.broadcast([]byte("cached wrong mode\r\nR1>"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := h.RunExec(ctx, "test", "show clock"); err != nil {
		t.Fatal(err)
	}
	pty.mu.Lock()
	defer pty.mu.Unlock()
	for _, p := range pty.writes {
		if string(p) == "enable\r" {
			t.Fatal("cached unprivileged prompt caused enable")
		}
	}
}

func TestCancelledTurnRejectsOldWriterDuringSuccessor(t *testing.T) {
	p := &drainPty{started: make(chan struct{}), resume: make(chan struct{})}
	close(p.resume)
	h := &consoleHub{pty: p, done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	oldRelease, oldID, err := h.claimTurn(ctx, "same-label")
	if err != nil {
		t.Fatal(err)
	}
	defer oldRelease()
	if err = h.gatedWrite([]byte("queued")); err != nil {
		t.Fatal(err)
	}
	cancel()
	nextCtx, nextCancel := context.WithTimeout(context.Background(), time.Second)
	defer nextCancel()
	release, id, err := h.claimTurn(nextCtx, "same-label")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if id == oldID {
		t.Fatal("turn generation reused")
	}
	if err = h.writeTurn(context.Background(), oldID, []byte("stale")); err == nil {
		t.Fatal("expired writer entered active successor turn")
	}
	if err = h.writeTurn(nextCtx, id, []byte("current")); err != nil {
		t.Fatal(err)
	}
	if got := p.bytes.String(); got != "queuedcurrent" {
		t.Fatalf("writes %q", got)
	}
}

func TestClaimContextCanCancelWhileWriteMutexHeld(t *testing.T) {
	h := &consoleHub{done: make(chan struct{})}
	h.wmu.Lock()
	defer h.wmu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := h.ClaimTurn(ctx, "blocked"); err != context.DeadlineExceeded {
		t.Fatalf("claim error %v", err)
	}
}

func TestEvictingSlowSubscriberPreservesHealthySubscriber(t *testing.T) {
	pty, out, in := newFakePty()
	defer out.Close()
	defer in.Close()
	h := newConsoleHub(pty, "")
	defer h.shutdown()
	slow, healthy := h.Subscribe(), h.Subscribe()
	defer slow.Unsubscribe()
	defer healthy.Unsubscribe()
	for i := 0; i < hubClientQueue+10; i++ {
		h.broadcast([]byte("data"))
		if got := <-healthy.Out; string(got) != "data" {
			t.Fatalf("healthy output %q", got)
		}
	}
	select {
	case <-slow.Done:
	default:
		t.Fatal("slow subscriber not evicted")
	}
	select {
	case <-healthy.Done:
		t.Fatal("healthy subscriber evicted")
	default:
	}
}
