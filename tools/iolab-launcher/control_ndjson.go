package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"time"
)

const maxNDJSONFrame = 4 << 20

type ndjsonError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ndjsonError) Error() string {
	if e == nil {
		return "control-plane error"
	}
	return e.Code + ": " + e.Message
}

type ndjsonEnvelope struct {
	ID     string          `json:"id"`
	Event  string          `json:"event"`
	OK     *bool           `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  *ndjsonError    `json:"error"`
}

type ndjsonRequest struct {
	ID   string          `json:"id"`
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args,omitempty"`
}

type ndjsonClient struct {
	conn    net.Conn
	reader  *bufio.Reader
	timeout time.Duration
}

var ndjsonID uint64

func dialNDJSON(ctx context.Context, address string, timeout time.Duration) (*ndjsonClient, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	return newNDJSONClient(conn, timeout), nil
}

func newNDJSONClient(conn net.Conn, timeout time.Duration) *ndjsonClient {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &ndjsonClient{conn: conn, reader: bufio.NewReaderSize(conn, 32*1024), timeout: timeout}
}

func (c *ndjsonClient) Close() error { return c.conn.Close() }

func (c *ndjsonClient) request(ctx context.Context, op string, args any) (json.RawMessage, error) {
	if c == nil || c.conn == nil {
		return nil, errors.New("control-plane connection is nil")
	}
	requestID := fmt.Sprintf("launcher-%d", atomic.AddUint64(&ndjsonID, 1))
	var rawArgs json.RawMessage
	if args != nil {
		data, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("marshal %s args: %w", op, err)
		}
		rawArgs = data
	}
	frame, err := json.Marshal(ndjsonRequest{ID: requestID, Op: op, Args: rawArgs})
	if err != nil {
		return nil, err
	}
	frame = append(frame, '\n')
	if err := c.setDeadline(ctx); err != nil {
		return nil, err
	}
	if err := writeAll(c.conn, frame); err != nil {
		return nil, err
	}
	for {
		line, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		var envelope ndjsonEnvelope
		if json.Unmarshal(line, &envelope) != nil {
			continue
		}
		if envelope.Event != "" || envelope.ID == "" || envelope.ID != requestID {
			continue
		}
		if envelope.OK == nil {
			return nil, errors.New("control-plane reply omitted ok")
		}
		if !*envelope.OK {
			if envelope.Error != nil {
				return nil, envelope.Error
			}
			return nil, errors.New("control-plane request failed")
		}
		return envelope.Result, nil
	}
}

func (c *ndjsonClient) setDeadline(ctx context.Context) error {
	deadline := time.Now().Add(c.timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	return c.conn.SetDeadline(deadline)
}

func (c *ndjsonClient) readFrame() ([]byte, error) {
	var frame []byte
	for {
		part, err := c.reader.ReadSlice('\n')
		frame = append(frame, part...)
		if len(frame) > maxNDJSONFrame {
			return nil, fmt.Errorf("control-plane frame exceeds %d bytes", maxNDJSONFrame)
		}
		if err == nil {
			return []byte(strings.TrimSuffix(strings.TrimSuffix(string(frame), "\n"), "\r")), nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) && len(frame) > 0 {
			return []byte(strings.TrimSuffix(strings.TrimSuffix(string(frame), "\n"), "\r")), nil
		}
		return nil, err
	}
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

type helloResult struct {
	Supervisor string   `json:"supervisor"`
	Runtime    string   `json:"runtime"`
	Arch       string   `json:"arch"`
	Features   []string `json:"features"`
	Egress     string   `json:"egress"`
}

func (c *ndjsonClient) hello(ctx context.Context) (helloResult, error) {
	result, err := c.request(ctx, "hello", map[string]string{"client": "iolbox-launcher"})
	if err != nil {
		return helloResult{}, err
	}
	var hello helloResult
	if err := json.Unmarshal(result, &hello); err != nil {
		return helloResult{}, fmt.Errorf("decode hello result: %w", err)
	}
	return hello, nil
}

type labLoadResult struct {
	LabID    string   `json:"labId"`
	Warnings []string `json:"warnings"`
}

func (c *ndjsonClient) labLoad(ctx context.Context, lab json.RawMessage) (labLoadResult, error) {
	trimmed := strings.TrimSpace(string(lab))
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' || !json.Valid(lab) {
		return labLoadResult{}, errors.New("lab.load requires a structured JSON object")
	}
	result, err := c.request(ctx, "lab.load", map[string]json.RawMessage{"lab": lab})
	if err != nil {
		return labLoadResult{}, err
	}
	var load labLoadResult
	if err := json.Unmarshal(result, &load); err != nil {
		return labLoadResult{}, fmt.Errorf("decode lab.load result: %w", err)
	}
	if len(load.Warnings) > 0 {
		return load, fmt.Errorf("lab.load reported warnings: %s", strings.Join(load.Warnings, "; "))
	}
	return load, nil
}

type labStartResult struct {
	Started []json.RawMessage `json:"started"`
	Failed  []json.RawMessage `json:"failed"`
}

func (c *ndjsonClient) labStart(ctx context.Context, args any) (labStartResult, error) {
	if args == nil {
		args = map[string]any{}
	}
	result, err := c.request(ctx, "lab.start", args)
	if err != nil {
		return labStartResult{}, err
	}
	var start labStartResult
	if err := json.Unmarshal(result, &start); err != nil {
		return labStartResult{}, fmt.Errorf("decode lab.start result: %w", err)
	}
	if len(start.Failed) > 0 {
		return start, fmt.Errorf("lab.start reported %d failed node(s)", len(start.Failed))
	}
	return start, nil
}

func (c *ndjsonClient) labSaveDoc(ctx context.Context, yaml string) (string, error) {
	result, err := c.request(ctx, "lab.saveDoc", map[string]string{"lab": yaml})
	if err != nil {
		return "", err
	}
	var saved struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(result, &saved); err != nil || saved.ID == "" {
		return "", fmt.Errorf("decode lab.saveDoc result: %w", err)
	}
	return saved.ID, nil
}

func (c *ndjsonClient) labListDocs(ctx context.Context) ([]string, error) {
	result, err := c.request(ctx, "lab.listDocs", map[string]any{})
	if err != nil {
		return nil, err
	}
	var listed struct {
		Labs []string `json:"labs"`
	}
	if err := json.Unmarshal(result, &listed); err != nil {
		return nil, fmt.Errorf("decode lab.listDocs result: %w", err)
	}
	return listed.Labs, nil
}
