package dirstat

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The finding-#11 regressions. The live failure was: Classifier.Close() called
// c.closer() (which closed the raw socket fds) and then c.wg.Wait() forever,
// because closing an fd does NOT wake a goroutine already parked inside a
// blocking recvfrom on it. Close never returned, and since it runs under the
// server's labMu (teardownFabric <- handleLabLoad), the whole control plane
// wedged. These tests pin the two properties the fix rests on, without needing
// a real AF_PACKET socket: the fds are closed only AFTER the read loops have
// returned, and Close is bounded no matter what the loops do.

func TestCloseDrainClosesFDsOnlyAfterReadLoopsExit(t *testing.T) {
	c := newClassifier([]EndpointDev{{Index: 0, Dev: "tapA"}, {Index: 1, Dev: "tapB"}})
	stop := make(chan struct{})

	var fdsClosed atomic.Bool
	var closedEarly atomic.Bool
	var loops atomic.Int32

	for i := 0; i < 2; i++ {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			<-stop
			// Stand in for the tail of a real readLoop: the fds must still be
			// open here, otherwise a live loop could recvfrom a recycled fd.
			time.Sleep(20 * time.Millisecond)
			if fdsClosed.Load() {
				closedEarly.Store(true)
			}
			loops.Add(1)
		}()
	}

	var once sync.Once
	c.stopRead = func() { once.Do(func() { close(stop) }) }
	c.closeFDs = func() { fdsClosed.Store(true) }

	if !c.closeDrain(closeDrainTimeout) {
		t.Fatalf("closeDrain reported the read loops did not drain")
	}
	if loops.Load() != 2 {
		t.Fatalf("closeDrain returned with %d/2 read loops finished", loops.Load())
	}
	if !fdsClosed.Load() {
		t.Fatalf("closeDrain drained but never closed the fds")
	}
	if closedEarly.Load() {
		t.Fatalf("fds were closed while a read loop was still running")
	}

	// Idempotence: a second Close must not block or panic on the closed chan.
	c.Close()
}

func TestCloseIsBoundedWhenAReadLoopNeverExits(t *testing.T) {
	c := newClassifier([]EndpointDev{{Index: 0, Dev: "tapA"}})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		<-release // models the pre-fix wedge: never observes teardown
	}()

	var stopped, fdsClosed atomic.Bool
	c.stopRead = func() { stopped.Store(true) }
	c.closeFDs = func() { fdsClosed.Store(true) }

	start := time.Now()
	drained := c.closeDrain(100 * time.Millisecond)
	elapsed := time.Since(start)

	if drained {
		t.Fatalf("closeDrain claimed a wedged read loop drained")
	}
	if elapsed > time.Second {
		t.Fatalf("closeDrain took %s; it must be bounded by its timeout", elapsed)
	}
	if !stopped.Load() {
		t.Fatalf("closeDrain never signalled the read loops to stop")
	}
	// Critical: a timed-out drain must LEAK the sockets rather than close fds a
	// live goroutine is still reading from.
	if fdsClosed.Load() {
		t.Fatalf("closeDrain closed the fds despite a still-running read loop")
	}
}

func TestCloseNilAndStubAreNoOps(t *testing.T) {
	var nilClassifier *Classifier
	nilClassifier.Close() // must not panic

	// A classifier with no platform hooks (the non-linux stub shape).
	(&Classifier{}).Close()
}
