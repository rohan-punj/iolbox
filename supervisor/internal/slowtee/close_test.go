package slowtee

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Finding-#11 regressions for the tee, which had the identical teardown bug as
// dirstat (close(fd) does not wake a goroutine parked in recvfrom) and is closed
// from the same serialized teardownFabric path.

func TestCloseDrainClosesFDsOnlyAfterForwardLoopsExit(t *testing.T) {
	tee := &Tee{wg: &sync.WaitGroup{}}
	stop := make(chan struct{})

	var fdsClosed, closedEarly atomic.Bool
	var loops atomic.Int32

	for i := 0; i < 2; i++ {
		tee.wg.Add(1)
		go func() {
			defer tee.wg.Done()
			<-stop
			time.Sleep(20 * time.Millisecond)
			if fdsClosed.Load() {
				closedEarly.Store(true)
			}
			loops.Add(1)
		}()
	}

	var once sync.Once
	tee.stopRead = func() { once.Do(func() { close(stop) }) }
	tee.closeFDs = func() { fdsClosed.Store(true) }

	if !tee.closeDrain(closeDrainTimeout) {
		t.Fatalf("closeDrain reported the forward loops did not drain")
	}
	if loops.Load() != 2 {
		t.Fatalf("closeDrain returned with %d/2 forward loops finished", loops.Load())
	}
	if !fdsClosed.Load() {
		t.Fatalf("closeDrain drained but never closed the fds")
	}
	if closedEarly.Load() {
		t.Fatalf("fds were closed while a forward loop was still running")
	}
	tee.Close() // idempotent
}

func TestCloseIsBoundedWhenAForwardLoopNeverExits(t *testing.T) {
	tee := &Tee{wg: &sync.WaitGroup{}}
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	tee.wg.Add(1)
	go func() {
		defer tee.wg.Done()
		<-release
	}()

	var stopped, fdsClosed atomic.Bool
	tee.stopRead = func() { stopped.Store(true) }
	tee.closeFDs = func() { fdsClosed.Store(true) }

	start := time.Now()
	drained := tee.closeDrain(100 * time.Millisecond)
	if drained {
		t.Fatalf("closeDrain claimed a wedged forward loop drained")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("closeDrain took %s; it must be bounded by its timeout", elapsed)
	}
	if !stopped.Load() {
		t.Fatalf("closeDrain never signalled the forward loops to stop")
	}
	if fdsClosed.Load() {
		t.Fatalf("closeDrain closed the fds despite a still-running forward loop")
	}
}

func TestTeeCloseNilAndStubAreNoOps(t *testing.T) {
	var nilTee *Tee
	nilTee.Close()
	(&Tee{}).Close()
}
