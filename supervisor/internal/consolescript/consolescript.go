// Package consolescript holds the connection-agnostic console-scripting
// primitives originally written as private helpers/methods on
// server.consoleSession (supervisor/internal/server/painter_linux.go). They
// are pure byte/string logic — prompt-suffix detection, prompt-sync polling,
// and show-output cleaning — with no dependency on net.Conn or any particular
// transport. This is what lets both today's per-dial consoleSession (which
// still owns its own socket + telnet.Negotiator) and a future hub-based
// "claim a turn, write, read-until-prompt" path (v0.3.0 Phase 4) share
// exactly the same exec/prompt-sync semantics.
//
// Extracted as part of v0.3.0 Phase 0 (docs/v0.3.0-console-unification.md
// §4 "Phase 0"). Zero behavior change: the logic is unchanged from
// painter_linux.go, only its shape (byte-accumulator + write func instead of
// a struct wrapping a net.Conn) is generalized.
package consolescript

import (
	"context"
	"strings"
)

// Writer is the minimal write capability a Session needs: send raw bytes
// toward the console. Both a net.Conn and a future hub turn-write path
// satisfy this trivially.
type Writer func(p []byte) error

// Session accumulates clean (telnet-IAC-free) output bytes fed to it by the
// caller and provides the prompt-sync / exec helpers on top of that buffer.
// It does not read from anything itself — the caller (today: consoleSession
// reading a telnet socket through a Negotiator; later: a hub subscriber
// reading decoded broadcast output) is responsible for feeding bytes in via
// Feed and performing the actual write via the Writer passed to New.
type Session struct {
	write Writer
	buf   strings.Builder
}

// New returns a Session that writes outbound bytes via write.
func New(write Writer) *Session {
	return &Session{write: write}
}

// Write sends raw bytes to the console.
func (s *Session) Write(p []byte) error {
	return s.write(p)
}

// Feed appends newly-arrived clean (already telnet-decoded) bytes to the
// session's accumulated buffer. Callers are responsible for stripping/
// answering telnet IAC negotiation before calling Feed — this package only
// ever sees application bytes.
func (s *Session) Feed(clean []byte) {
	if len(clean) > 0 {
		s.buf.Write(clean)
	}
}

// Reset clears the accumulated buffer (used between sync/capture phases so
// each phase's prompt/output detection isn't confused by earlier bytes).
func (s *Session) Reset() {
	s.buf.Reset()
}

// String returns the accumulated buffer's current contents.
func (s *Session) String() string {
	return s.buf.String()
}

// HasPromptSuffix matches a trailing IOS exec/enable prompt at the end of s:
//
//	R1>   (user exec)
//	R1#   (privileged exec)
//	R1(config)#  (config — we never want this; used to detect we overshot)
//
// It looks for "<word>[>#]" with optional trailing whitespace at the very
// end.
func HasPromptSuffix(s string) (prompt string, priv bool, ok bool) {
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

// ReadFunc pulls one more chunk of clean bytes into the session, bounded by
// ctx (e.g. via a read deadline on the underlying transport), and Feeds it.
// The caller owns the actual transport read + telnet decode; this package
// only needs "give me more bytes or tell me why you can't". Returning an
// error (including ctx's own error) stops the sync/capture loop that's
// calling it.
type ReadFunc func(ctx context.Context) error

// SyncPrompt sends a bare newline and reads (via read) until an exec prompt
// appears at the end of the accumulated buffer, returning whether the prompt
// is privileged (enable) mode. Bounded by ctx: a read error (including a
// deadline set from ctx by the caller's ReadFunc) aborts the loop.
func (s *Session) SyncPrompt(ctx context.Context, read ReadFunc) (priv bool, err error) {
	if err := s.Write([]byte("\r")); err != nil {
		return false, err
	}
	for {
		if _, p, ok := HasPromptSuffix(s.String()); ok {
			return p, nil
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if err := read(ctx); err != nil {
			// A read timeout with no prompt yet is a real failure.
			return false, err
		}
	}
}

// RunExec performs the full scripted exec sequence against whatever console
// the Session's Writer/ReadFunc are wired to: sync the prompt, ensure enable
// mode + `terminal length 0`, run cmd, and return its trimmed output.
//
// This is the exact sequence runShow performed inline in
// painter_linux.go before the v0.3.0 Phase 0 extraction; behavior is
// unchanged, only decoupled from a net.Conn-backed consoleSession so a future
// hub-based turn (Phase 4) can call it identically.
func (s *Session) RunExec(ctx context.Context, read ReadFunc, cmd string) (string, error) {
	priv, err := s.SyncPrompt(ctx, read)
	if err != nil {
		return "", err
	}
	if !priv {
		// Enter enable mode. Labs boot with no enable secret (injected default
		// config), so `enable` drops straight to `#`; if a password is prompted we
		// simply won't reach `#` and the sync below times out -> empty result.
		if err := s.Write([]byte("enable\r")); err != nil {
			return "", err
		}
		if _, err := s.SyncPrompt(ctx, read); err != nil {
			return "", err
		}
	}
	// Disable the pager so long output isn't broken by --More-- prompts.
	s.Reset()
	if err := s.Write([]byte("terminal length 0\r")); err != nil {
		return "", err
	}
	if _, err := s.SyncPrompt(ctx, read); err != nil {
		return "", err
	}

	// Run the show command and capture until the prompt returns.
	s.Reset()
	if err := s.Write([]byte(cmd + "\r")); err != nil {
		return "", err
	}
	for {
		if _, _, ok := HasPromptSuffix(s.String()); ok {
			break
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if err := read(ctx); err != nil {
			return "", err
		}
	}
	return CleanShowOutput(s.String(), cmd), nil
}

// CleanShowOutput strips the echoed command line and the trailing prompt line
// from a captured show session, returning just the command's output.
func CleanShowOutput(raw, cmd string) string {
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
		if _, _, ok := HasPromptSuffix(ln); ok && !strings.ContainsAny(trimmed, " ") {
			continue
		}
		out = append(out, ln)
	}
	// Trim leading/trailing blank lines.
	joined := strings.Join(out, "\n")
	return strings.Trim(joined, "\n")
}
