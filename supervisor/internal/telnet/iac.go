// Package telnet implements just enough of RFC 854 (Telnet) and RFC 1073
// (NAWS) to bridge a raw IOL/VPCS telnet console to a clean byte stream for
// an xterm.js terminal, matching the behaviour PNetLab's webconsole exposes
// to the browser.
//
// The console ports IOL/VPCS listen on are plain telnet servers. A real
// telnet client negotiates options (echo, suppress-go-ahead, window size)
// with IAC (0xFF) command sequences interleaved in the byte stream. xterm.js
// speaks none of that — it just wants clean terminal bytes in one direction
// and keystrokes in the other. This package's Negotiator sits between the
// two: it strips/consumes IAC sequences from the node->client direction,
// answers option negotiation automatically (see policy below), and can emit
// a NAWS subnegotiation toward the node when the client reports a resize.
package telnet

import "bytes"

// Telnet protocol bytes (RFC 854 §symbols, RFC 1073 for NAWS).
const (
	IAC  byte = 255 // Interpret As Command
	DONT byte = 254
	DO   byte = 253
	WONT byte = 252
	WILL byte = 251
	SB   byte = 250 // subnegotiation begin
	GA   byte = 249 // go ahead
	SE   byte = 240 // subnegotiation end

	OptEcho  byte = 1  // ECHO
	OptSGA   byte = 3  // Suppress Go Ahead
	OptTType byte = 24 // Terminal Type
	OptNAWS  byte = 31 // Negotiate About Window Size
)

// negState is the small state machine driving IAC parsing.
type negState int

const (
	stData negState = iota
	stIAC
	stCmd // saw IAC <DO|DONT|WILL|WONT>, waiting for the option byte
	stSB  // inside a subnegotiation, waiting for IAC SE
	stSBIAC
)

// Negotiator consumes a raw telnet byte stream from the node and:
//   - passes clean application data through to Output
//   - strips and answers IAC negotiation automatically
//   - exposes Reply(), the bytes (if any) that must be written back to the
//     node in response to what was just fed in
//
// Policy (matches what a permissive terminal client like PNetLab's webconsole
// bridge does): we WILL accept the peer suppressing go-ahead and echoing
// (most IOL/VPCS consoles want to own local echo, since they are line-mode
// Cisco-style CLIs), and we DO agree to receive NAWS/terminal-type requests by
// replying WILL to the options we support and DONT/WONT to everything else,
// so unrecognised options never leave the peer hanging. This mirrors the
// classic "answer everything, mostly get out of the way" telnet client
// negotiation strategy.
type Negotiator struct {
	state   negState
	pending byte // the DO/DONT/WILL/WONT just seen, while in stCmd
	sb      []byte

	out   bytes.Buffer // clean application data extracted from the last Feed
	reply bytes.Buffer // bytes to send back to the node (option replies)

	// Per-option negotiation state, keyed by option byte. RFC 1143's core
	// insight (the "Q Method"): a telnet endpoint must reply to DO/DONT/WILL/
	// WONT only when the request CHANGES its state. Answering an option that is
	// already in the requested state turns a peer's acknowledgement into a fresh
	// request, and two endpoints that both "answer everything" ping-pong the
	// same DONT<->WONT (and DO<->WILL) pair forever — a 100%-CPU busy loop
	// observed when the supervisor's console hub and the wsbridge negotiator
	// (both instances of THIS type) were wired mouth-to-mouth over a loopback
	// telnet socket. remoteOn tracks whether the PEER has enabled an option
	// toward us (its WILL we agreed to); localOn tracks whether WE have enabled
	// an option toward the peer (our WILL it agreed to). We only emit a reply on
	// a genuine transition.
	remoteOn map[byte]bool // peer->us option enabled (we answered its WILL with DO)
	localOn  map[byte]bool // us->peer option enabled (we answered its DO with WILL)
	// "refused" marks: options we've already sent a terminal WONT/DONT for, so a
	// peer that re-requests a disabled option is answered exactly once. Lazily
	// allocated.
	localRef  map[byte]bool // we sent WONT for opt (refused peer's DO)
	remoteRef map[byte]bool // we sent DONT for opt (refused peer's WILL)
}

// NewNegotiator returns a Negotiator ready to process a fresh telnet stream.
func NewNegotiator() *Negotiator {
	return &Negotiator{
		remoteOn: make(map[byte]bool),
		localOn:  make(map[byte]bool),
	}
}

// Feed processes raw bytes received from the node's telnet socket. It returns
// the clean application data (safe to forward to the browser/xterm.js) with
// all IAC sequences consumed. Call Reply() afterward (or interleave) to get
// any bytes that must be written back to the node.
func (n *Negotiator) Feed(p []byte) []byte {
	n.out.Reset()
	for _, b := range p {
		n.step(b)
	}
	return n.out.Bytes()
}

// Reply drains and returns any option-negotiation reply bytes queued while
// processing the most recent Feed call(s). The caller writes these back to
// the node's telnet socket. Safe to call even if empty (returns nil).
func (n *Negotiator) Reply() []byte {
	if n.reply.Len() == 0 {
		return nil
	}
	b := append([]byte(nil), n.reply.Bytes()...)
	n.reply.Reset()
	return b
}

func (n *Negotiator) step(b byte) {
	switch n.state {
	case stData:
		if b == IAC {
			n.state = stIAC
			return
		}
		n.out.WriteByte(b)

	case stIAC:
		switch b {
		case IAC:
			// Escaped 0xFF literal in the data stream.
			n.out.WriteByte(IAC)
			n.state = stData
		case DO, DONT, WILL, WONT:
			n.pending = b
			n.state = stCmd
		case SB:
			n.sb = n.sb[:0]
			n.state = stSB
		case GA:
			// Go-ahead: no state to track, no reply.
			n.state = stData
		default:
			// Other IAC <cmd> with no trailing option byte (NOP, BRK, etc).
			n.state = stData
		}

	case stCmd:
		n.handleOption(n.pending, b)
		n.state = stData

	case stSB:
		if b == IAC {
			n.state = stSBIAC
			return
		}
		n.sb = append(n.sb, b)

	case stSBIAC:
		if b == SE {
			n.handleSubnegotiation(n.sb)
			n.state = stData
			return
		}
		if b == IAC {
			// Escaped 0xFF inside a subnegotiation payload.
			n.sb = append(n.sb, IAC)
			n.state = stSB
			return
		}
		// Unexpected; treat as data resuming.
		n.sb = append(n.sb, b)
		n.state = stSB
	}
}

// handleOption answers a DO/DONT/WILL/WONT request for a single option byte
// following RFC 1143's rule: reply ONLY when the request transitions our state.
// Supported options (SGA, ECHO, NAWS) get an agreeing reply; anything else is
// refused. Acknowledgements that don't change state are absorbed silently, so
// two negotiators cannot ping-pong the same option forever (see the busy-loop
// note on the Negotiator struct).
func (n *Negotiator) handleOption(cmd, opt byte) {
	switch cmd {
	case DO:
		// Peer wants US to enable `opt`. We agree only to SGA (harmless, quiets
		// line-mode peers); everything else is refused. Reply only on a change:
		// a repeated DO for an already-enabled option is the peer's ack of our
		// WILL and must not be answered again.
		want := opt == OptSGA
		if want {
			if !n.localOn[opt] {
				n.localOn[opt] = true
				n.sendReply(WILL, opt)
			}
		} else {
			if n.localOn[opt] {
				n.localOn[opt] = false
			}
			// Only announce refusal the FIRST time; a peer re-asking after we
			// already said WONT gets silence (it has our answer).
			if !n.localRefused(opt) {
				n.markLocalRefused(opt)
				n.sendReply(WONT, opt)
			}
		}
	case DONT:
		// Peer wants us to stop `opt`. Acknowledge with WONT only if we were
		// enabled or haven't answered yet; a DONT echoing our own WONT is
		// absorbed to end the exchange.
		if n.localOn[opt] {
			n.localOn[opt] = false
			n.sendReply(WONT, opt)
		} else if !n.localRefused(opt) {
			n.markLocalRefused(opt)
			n.sendReply(WONT, opt)
		}
	case WILL:
		// Peer offers to enable `opt` on its side. Accept SGA/ECHO/NAWS; refuse
		// the rest. Reply only on a transition.
		accept := opt == OptSGA || opt == OptEcho || opt == OptNAWS
		if accept {
			if !n.remoteOn[opt] {
				n.remoteOn[opt] = true
				n.sendReply(DO, opt)
			}
		} else {
			if n.remoteOn[opt] {
				n.remoteOn[opt] = false
			}
			if !n.remoteRefused(opt) {
				n.markRemoteRefused(opt)
				n.sendReply(DONT, opt)
			}
		}
	case WONT:
		// Peer will not / no longer enable `opt`. Acknowledge with DONT only on
		// a change; a WONT echoing our own DONT is absorbed.
		if n.remoteOn[opt] {
			n.remoteOn[opt] = false
			n.sendReply(DONT, opt)
		} else if !n.remoteRefused(opt) {
			n.markRemoteRefused(opt)
			n.sendReply(DONT, opt)
		}
	}
}

// The "refused" sets record that we have already sent a terminal WONT (local)
// or DONT (remote) for an option, so a peer that keeps re-requesting the same
// disabled option gets one answer, not one per request. Enabling an option
// clears its refused mark implicitly via the On maps above.
func (n *Negotiator) localRefused(opt byte) bool  { return n.localRef[opt] }
func (n *Negotiator) remoteRefused(opt byte) bool { return n.remoteRef[opt] }
func (n *Negotiator) markLocalRefused(opt byte) {
	if n.localRef == nil {
		n.localRef = make(map[byte]bool)
	}
	n.localRef[opt] = true
}
func (n *Negotiator) markRemoteRefused(opt byte) {
	if n.remoteRef == nil {
		n.remoteRef = make(map[byte]bool)
	}
	n.remoteRef[opt] = true
}

// handleSubnegotiation processes a completed IAC SB ... IAC SE payload. Only
// inbound-from-node subnegotiations reach here (e.g. terminal-type queries);
// NAWS is something we send, not receive, so there is nothing to act on for
// it today beyond not crashing.
func (n *Negotiator) handleSubnegotiation(payload []byte) {
	// No inbound subnegotiations are currently acted on. Reserved for future
	// terminal-type answering if a node ever requires it.
	_ = payload
}

func (n *Negotiator) sendReply(cmd, opt byte) {
	n.reply.Write([]byte{IAC, cmd, opt})
}

// NAWS builds the IAC SB NAWS <cols-hi> <cols-lo> <rows-hi> <rows-lo> IAC SE
// subnegotiation (RFC 1073) advertising a client window size. Any byte in
// cols/rows equal to IAC (0xFF) is escaped by doubling per RFC 854, though in
// practice terminal dimensions never approach that value.
func NAWS(cols, rows uint16) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{IAC, SB, OptNAWS})
	writeEscaped16(&buf, cols)
	writeEscaped16(&buf, rows)
	buf.Write([]byte{IAC, SE})
	return buf.Bytes()
}

func writeEscaped16(buf *bytes.Buffer, v uint16) {
	hi, lo := byte(v>>8), byte(v&0xFF)
	writeEscapedByte(buf, hi)
	writeEscapedByte(buf, lo)
}

func writeEscapedByte(buf *bytes.Buffer, b byte) {
	buf.WriteByte(b)
	if b == IAC {
		buf.WriteByte(IAC)
	}
}

// WillNAWS returns the IAC WILL NAWS sequence a client sends to volunteer
// window-size reporting. The bridge sends this once at session start so the
// node (if it honours NAWS) knows to expect subnegotiations.
func WillNAWS() []byte {
	return []byte{IAC, WILL, OptNAWS}
}
