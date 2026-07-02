package protocol

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
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
