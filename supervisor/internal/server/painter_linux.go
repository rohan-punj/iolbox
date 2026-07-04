//go:build linux

package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/node"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
)

// runShowTimeout bounds a single runShow session end-to-end (turn claim +
// prompt sync + one command). IOL exec output is small; a converged `show`
// returns in well under a second, so a few seconds is generous and still
// fails fast on a wedged console. Also mirrored as the turn-claim timeout
// (v0.3.0 §7 decision 4) since RunExec's ctx bounds both.
const runShowTimeout = 8 * time.Second

// runShow runs ONE exec `show` command on a running IOL node and returns its
// output with the echoed command line and the trailing prompt stripped.
//
// v0.3.0 Phase 4: this no longer dials its own telnet socket. It claims the
// node's console hub's input-arbitration turn and drives the command over the
// hub's OWN decoded byte stream (node.Process.RunExec -> consoleHub.RunExec),
// so it: (a) never opens a second pty owner/Negotiator (the hub is the ONLY
// telnet-speaking thing left in the process — see docs/v0.3.0-console-
// unification.md §3(a)), (b) is guaranteed uninterleaved with concurrent
// interactive typing in the web/native console (the turn gate queues
// interactive keystrokes for the duration, §2.3/§3(b)), and (c) writes the
// show command to the SAME pty the student's own console reads, so it scrolls
// there too (§3(c), the teaching-win property).
//
// Only valid for a RUNNING IOL node (caller checks state); a stopped node has
// no console hub and this returns an error the caller turns into an empty
// result.
func (s *Server) runShow(ctx context.Context, ll *loadedLab, nodeID int, cmd string) (string, error) {
	nr := ll.get(nodeID)
	if nr == nil || nr.proc == nil {
		return "", protocol.Errorf(protocol.CodeNotLoaded, "node %d is not running", nodeID)
	}
	if nr.machine.State() != node.StateRunning {
		return "", protocol.Errorf(protocol.CodeNotLoaded, "node %d is not running", nodeID)
	}

	ctx, cancel := context.WithTimeout(ctx, runShowTimeout)
	defer cancel()

	holder := fmt.Sprintf("painter:show:node%d", nodeID)
	out, err := nr.proc.RunExec(ctx, holder, cmd)
	if err != nil {
		if errors.Is(err, node.ErrNoConsoleHub) {
			return "", protocol.Errorf(protocol.CodeNotLoaded, "node %d has no console", nodeID)
		}
		return "", err
	}
	return out, nil
}
