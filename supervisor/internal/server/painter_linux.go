//go:build linux

package server

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/consolescript"
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
// The connection-agnostic prompt-sync/exec-parsing logic (SyncPrompt/RunExec/
// HasPromptSuffix/CleanShowOutput) lives in internal/consolescript (v0.3.0
// Phase 0 extraction) so it can be shared with a future hub-based turn
// (Phase 4); this type retains only the net.Conn + telnet.Negotiator plumbing
// that's specific to today's dial-your-own-socket path.
type consoleSession struct {
	conn net.Conn
	neg  *telnet.Negotiator
	sess *consolescript.Session
}

// dialConsole opens a telnet session to the loopback console port and completes
// initial IAC negotiation.
func dialConsole(ctx context.Context, port int) (*consoleSession, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, protocol.Errorf(protocol.CodeNotLoaded, "dial console :%d: %v", port, err)
	}
	s := &consoleSession{conn: conn, neg: telnet.NewNegotiator()}
	s.sess = consolescript.New(s.write)
	return s, nil
}

func (s *consoleSession) close() { _ = s.conn.Close() }

// write sends raw bytes to the console.
func (s *consoleSession) write(b []byte) error {
	_, err := s.conn.Write(b)
	return err
}

// readInto reads one chunk, feeds it through the telnet negotiator (answering
// option requests), and appends the clean output to the session buffer.
// Honors the ctx deadline via a read deadline. Satisfies
// consolescript.ReadFunc.
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
		s.sess.Feed(clean)
	}
	return err
}

// runShow performs the full scripted exec: sync prompt, ensure enable +
// `terminal length 0`, run the command, and return the trimmed output.
func (s *consoleSession) runShow(ctx context.Context, cmd string) (string, error) {
	return s.sess.RunExec(ctx, s.readInto, cmd)
}
