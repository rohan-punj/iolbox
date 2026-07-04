//go:build linux

package server

import (
	"bytes"
	"context"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/node"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
	"github.com/rohanpunj/iolbox/supervisor/internal/telnet"
)

// runShowTimeout bounds a single runShow session end-to-end (dial + prompt sync
// + one command). IOL exec output is small; a converged `show` returns in well
// under a second, so a few seconds is generous and still fails fast on a wedged
// console.
const runShowTimeout = 8 * time.Second

// runShow runs ONE exec `show` command on a running IOL node and returns its
// output with the echoed command line and the trailing prompt stripped.
//
// It reuses the SAME console path config.extract's siblings use: it dials the
// node's loopback telnet console port, which the per-node consoleHub multiplexes
// (see node/console_hub.go) — so this attaches as just another hub client
// ALONGSIDE the webconsole and never opens a second pty owner that would fight
// it. The session: negotiate telnet, wait for an exec prompt, enter enable mode
// if needed, set `terminal length 0` (no pager), send the show command, read
// until the prompt returns, then strip the echo + prompt.
//
// Only valid for a RUNNING IOL node (caller checks state); a stopped node has no
// console port and this returns an error the caller turns into an empty result.
func (s *Server) runShow(ctx context.Context, ll *loadedLab, nodeID int, cmd string) (string, error) {
	nr := ll.get(nodeID)
	if nr == nil || nr.proc == nil {
		return "", protocol.Errorf(protocol.CodeNotLoaded, "node %d is not running", nodeID)
	}
	if nr.machine.State() != node.StateRunning {
		return "", protocol.Errorf(protocol.CodeNotLoaded, "node %d is not running", nodeID)
	}
	port := nr.consolePort

	ctx, cancel := context.WithTimeout(ctx, runShowTimeout)
	defer cancel()

	sess, err := dialConsole(ctx, port)
	if err != nil {
		return "", err
	}
	defer sess.close()

	return sess.runShow(ctx, cmd)
}

// consoleSession is one scripted telnet exec session against a node console.
type consoleSession struct {
	conn net.Conn
	neg  *telnet.Negotiator
	buf  bytes.Buffer // accumulated clean output not yet consumed by a wait
}

// dialConsole opens a telnet session to the loopback console port and completes
// initial IAC negotiation.
func dialConsole(ctx context.Context, port int) (*consoleSession, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, protocol.Errorf(protocol.CodeNotLoaded, "dial console :%d: %v", port, err)
	}
	return &consoleSession{conn: conn, neg: telnet.NewNegotiator()}, nil
}

func (s *consoleSession) close() { _ = s.conn.Close() }

// write sends raw bytes to the console.
func (s *consoleSession) write(b []byte) error {
	_, err := s.conn.Write(b)
	return err
}

// readInto reads one chunk, feeds it through the telnet negotiator (answering
// option requests), and appends the clean output to buf. Honors the ctx
// deadline via a read deadline.
func (s *consoleSession) readInto(ctx context.Context) error {
	if dl, ok := ctx.Deadline(); ok {
		_ = s.conn.SetReadDeadline(dl)
	}
	tmp := make([]byte, 4096)
	n, err := s.conn.Read(tmp)
	if n > 0 {
		clean := s.neg.Feed(tmp[:n])
		if reply := s.neg.Reply(); len(reply) > 0 {
			_ = s.write(reply)
		}
		s.buf.Write(clean)
	}
	return err
}

// promptRe matches a trailing IOS exec/enable prompt at end of buffer:
//
//	R1>   (user exec)
//	R1#   (privileged exec)
//	R1(config)#  (config — we never want this; used to detect we overshot)
//
// We look for "<word>[>#]" with optional trailing whitespace at the very end.
func hasPromptSuffix(s string) (prompt string, priv bool, ok bool) {
	t := strings.TrimRight(s, " \t\r\n")
	if t == "" {
		return "", false, false
	}
	// Last line.
	if i := strings.LastIndexByte(t, '\n'); i >= 0 {
		t = t[i+1:]
	}
	t = strings.TrimSpace(t)
	if t == "" {
		return "", false, false
	}
	last := t[len(t)-1]
	if last != '>' && last != '#' {
		return "", false, false
	}
	// The prompt token must be a plausible hostname (letters/digits/-/./(/)).
	for _, c := range t[:len(t)-1] {
		if !(c == '-' || c == '.' || c == '(' || c == ')' || c == '_' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return "", false, false
		}
	}
	return t, last == '#', true
}

// syncPrompt sends a bare newline and reads until an exec prompt appears,
// returning whether we're in privileged (enable) mode. Bounded by ctx.
func (s *consoleSession) syncPrompt(ctx context.Context) (priv bool, err error) {
	if err := s.write([]byte("\r")); err != nil {
		return false, err
	}
	for {
		if _, p, ok := hasPromptSuffix(s.buf.String()); ok {
			return p, nil
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if err := s.readInto(ctx); err != nil {
			// A read timeout with no prompt yet is a real failure.
			return false, err
		}
	}
}

// runShow performs the full scripted exec: sync prompt, ensure enable +
// `terminal length 0`, run the command, and return the trimmed output.
func (s *consoleSession) runShow(ctx context.Context, cmd string) (string, error) {
	priv, err := s.syncPrompt(ctx)
	if err != nil {
		return "", err
	}
	if !priv {
		// Enter enable mode. Labs boot with no enable secret (injected default
		// config), so `enable` drops straight to `#`; if a password is prompted we
		// simply won't reach `#` and the sync below times out -> empty result.
		if err := s.write([]byte("enable\r")); err != nil {
			return "", err
		}
		if _, err := s.syncPrompt(ctx); err != nil {
			return "", err
		}
	}
	// Disable the pager so long output isn't broken by --More-- prompts.
	s.buf.Reset()
	if err := s.write([]byte("terminal length 0\r")); err != nil {
		return "", err
	}
	if _, err := s.syncPrompt(ctx); err != nil {
		return "", err
	}

	// Run the show command and capture until the prompt returns.
	s.buf.Reset()
	if err := s.write([]byte(cmd + "\r")); err != nil {
		return "", err
	}
	for {
		if _, _, ok := hasPromptSuffix(s.buf.String()); ok {
			break
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if err := s.readInto(ctx); err != nil {
			return "", err
		}
	}
	return cleanShowOutput(s.buf.String(), cmd), nil
}

// cleanShowOutput strips the echoed command line and the trailing prompt line
// from a captured show session, returning just the command's output.
func cleanShowOutput(raw, cmd string) string {
	// Normalise CRLF.
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	lines := strings.Split(raw, "\n")

	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		// Drop the echoed command line.
		if trimmed == strings.TrimSpace(cmd) {
			continue
		}
		// Drop a trailing prompt-only line.
		if _, _, ok := hasPromptSuffix(ln); ok && !strings.ContainsAny(trimmed, " ") {
			continue
		}
		out = append(out, ln)
	}
	// Trim leading/trailing blank lines.
	joined := strings.Join(out, "\n")
	return strings.Trim(joined, "\n")
}
