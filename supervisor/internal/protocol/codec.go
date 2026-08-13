package protocol

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
	"time"
)

// Decoder reads newline-delimited JSON requests from a stream.
type Decoder struct {
	r *bufio.Reader
}

// NewDecoder wraps r for reading NDJSON. A large buffer accommodates big lab
// documents and startup-configs sent inline.
func NewDecoder(r io.Reader) *Decoder {
	br := bufio.NewReaderSize(r, 1<<16)
	return &Decoder{r: br}
}

// ReadRequest reads and parses the next request line. It returns io.EOF at end
// of stream. Blank lines are skipped.
func (d *Decoder) ReadRequest() (*Request, error) {
	for {
		line, err := readLine(d.r)
		if err != nil {
			return nil, err
		}
		if len(line) == 0 {
			continue
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			return nil, err
		}
		return &req, nil
	}
}

// readLine reads one line (without the trailing newline), transparently
// handling lines longer than the reader's buffer.
func readLine(r *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		frag, err := r.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			buf = append(buf, frag...)
			continue
		}
		if err != nil {
			if len(frag) > 0 && err == io.EOF {
				buf = append(buf, frag...)
				return trimCR(buf), nil
			}
			return nil, err
		}
		buf = append(buf, frag...)
		// drop trailing '\n'
		buf = buf[:len(buf)-1]
		return trimCR(buf), nil
	}
}

func trimCR(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] == '\r' {
		return b[:len(b)-1]
	}
	return b
}

// Encoder writes newline-delimited JSON responses and events to a stream.
// Writes are serialized so responses and pushed events never interleave.
type Encoder struct {
	mu sync.Mutex
	w  io.Writer
}

// NewEncoder wraps w for writing NDJSON.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

// WriteResponse writes a response line.
func (e *Encoder) WriteResponse(resp *Response) error {
	return e.writeJSON(resp)
}

// WriteEvent writes an event line.
func (e *Encoder) WriteEvent(ev *Event) error {
	return e.writeJSON(ev)
}

// deadlineWriter is the optional interface a network-backed writer satisfies
// (net.Conn does; so does wsbridge's WebSocket text-frame adapter).
type deadlineWriter interface {
	SetWriteDeadline(time.Time) error
}

// WriteEventWithDeadline writes one event, bounding the write with a deadline
// when the underlying writer supports one. Without a deadline a peer that has
// stopped reading blocks the writing goroutine until the kernel's TCP
// retransmit timeout, which is minutes.
//
// The deadline is set and cleared inside the encoder lock so a concurrent
// WriteResponse on the same connection can never inherit a deadline that was
// meant for an event, nor observe a half-configured one. The marshal happens
// before the lock is taken so serialization cost is not serialized.
func (e *Encoder) WriteEventWithDeadline(ev *Event, timeout time.Duration) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	e.mu.Lock()
	defer e.mu.Unlock()
	if d, ok := e.w.(deadlineWriter); ok {
		if err := d.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
			return err
		}
		defer func() { _ = d.SetWriteDeadline(time.Time{}) }()
	}
	_, err = e.w.Write(b)
	return err
}

// Close closes the wrapped stream when it owns a close operation, and is a
// no-op otherwise. The broadcaster uses this to drop a subscriber: closing the
// stream also unblocks the read loop on the same control connection, so the
// client sees a disconnect and reconnects rather than sitting on a live socket
// that will never receive another event.
func (e *Encoder) Close() error {
	if c, ok := e.w.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (e *Encoder) writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	e.mu.Lock()
	defer e.mu.Unlock()
	_, err = e.w.Write(b)
	return err
}
