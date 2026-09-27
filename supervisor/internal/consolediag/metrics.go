// Package consolediag provides opt-in, bounded console timing summaries.
// It records byte counts and durations, never console contents.
package consolediag

import (
	"log"
	"os"
	"sync"
	"time"
)

type Recorder struct {
	mu            sync.Mutex
	name, stage   string
	last          time.Time
	chunks, bytes uint64
	total, max    time.Duration
}

func New(name, stage string) *Recorder {
	if os.Getenv("IOLBOX_CONSOLE_METRICS") != "1" {
		return nil
	}
	return &Recorder{name: name, stage: stage, last: time.Now()}
}

func (r *Recorder) Observe(bytes int, elapsed time.Duration) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chunks++
	r.bytes += uint64(bytes)
	r.total += elapsed
	if elapsed > r.max {
		r.max = elapsed
	}
	if time.Since(r.last) >= time.Second {
		r.flushLocked()
	}
}

func (r *Recorder) Flush() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushLocked()
}

func (r *Recorder) flushLocked() {
	if r.chunks == 0 {
		return
	}
	log.Printf("console-metrics stage=%s source=%q chunks=%d bytes=%d mean_us=%d max_us=%d unix_ms=%d", r.stage, r.name, r.chunks, r.bytes, (r.total / time.Duration(r.chunks)).Microseconds(), r.max.Microseconds(), time.Now().UnixMilli())
	r.chunks = 0
	r.bytes = 0
	r.total = 0
	r.max = 0
	r.last = time.Now()
}
