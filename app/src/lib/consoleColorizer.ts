// IOS console color highlighter — a stream transformer applied to node->browser
// bytes before xterm.write, mirroring the user's PNetLab webconsole colorizer.
//
// WHY v2: live WS-frame evidence against real IOL (17.18.02 over the
// pty→telnet→wsbridge chain) showed the v1 assumption — "bulk output delivers
// whole lines inside one chunk" — is simply false in practice. Lines routinely
// split mid-word across frames (one frame even carried a single byte of a
// word), and the IOS prompt ALWAYS arrives as an unterminated tail ("\r\nR1>",
// no trailing newline). v1 colorized a line only when its terminator arrived in
// the same chunk as its start, so in real sessions essentially NOTHING was
// colorized (and prompts never could be). v2 fixes both while keeping
// interactive echo effectively instant:
//
//   - Complete lines are colorized whenever NO part of the line was already
//     emitted raw (`emittedInLine === 0`) — even if the line's bytes arrived
//     across several chunks, because the incomplete tail is briefly HELD.
//   - The incomplete tail of the current line is held back for a very short
//     flush window (flushDelayMs ≈ 16ms — about one display frame). If the rest
//     of the line arrives in that window, the whole line colorizes; if not, the
//     tail is flushed. A prompt-shaped tail ("R1#", "SW1(config-if)#") is
//     colorized ON FLUSH — the only chance a prompt ever gets, since it never
//     receives a terminator.
//   - INTERACTIVE ECHO IS NEVER DELAYED: a tiny chunk (≤ 3 bytes, no newline)
//     arriving while nothing is held — the shape of per-keystroke echo, seen as
//     1-byte frames on the wire — is forwarded raw synchronously in the same
//     push() call.
//   - Once any part of a line has gone out raw, the rest of that line is
//     forwarded raw immediately (never recolored, never held).
//   - A tail containing ESC is never held or rewritten (cursor moves, colored
//     banners — corrupting those is worse than not coloring). Complete lines
//     containing ESC pass through colorizeLine untouched as before.
//
// Byte ordering is inviolable: held bytes are always emitted before any bytes
// from a later chunk, and terminators ("\n", "\r\n") are preserved exactly.
//
// The transformer emits through a SINK callback (not a return value) because
// the flush window makes emission asynchronous. One instance per console tab;
// the per-line decision (`colorizeLine`) stays a pure exported function.

// SGR helpers — foreground colors + reset. Kept as plain strings so a colorized
// line is just `code + text + RESET` and the terminal state is always restored.
const ESC = "\x1b[";
const RESET = "\x1b[0m";
const CYAN_BOLD = `${ESC}1;36m`; // prompts
const GREEN = `${ESC}32m`; // "up"
const RED = `${ESC}31m`; // "down" / "administratively down"
const YELLOW = `${ESC}33m`; // %-prefixed error/warning lines
const BLUE = `${ESC}38;5;75m`; // IP addresses (subtle blue, 256-color)

// IOS prompt: hostname, optional (config-if) style submode, then > or #.
// e.g. "Router>", "R1#", "R1(config)#", "SW1(config-if)#", "R1(config-router-af)#".
const PROMPT_RE = /^[\w.-]+(\([\w-]+\))?[>#]/;

// A held TAIL that is exactly a prompt (nothing after the > or #). Anchored
// both ends, unlike PROMPT_RE: a tail like "R1#sh" is a prompt + typed echo and
// must NOT be wholesale-colored on flush.
const PROMPT_TAIL_RE = /^[\w.-]+(\([\w-]+\))?[>#] ?$/;

// A %-prefixed IOS notice/error/warning line (leading whitespace tolerated).
const PERCENT_RE = /^\s*%/;

// Dotted-quad IPv4 (avoids matching inside longer digit runs like 10.10.10.100/8
// is fine; the boundaries keep us off version strings like 15.6.3 by requiring
// four octets). Applied per-line only, and only when the line has no ESC already.
const IPV4_RE = /\b(?:\d{1,3}\.){3}\d{1,3}\b/g;

// Interface up/down state. One combined pass so "administratively down" is
// matched as a single red phrase BEFORE the standalone-word alternative can
// grab its trailing "down" — a two-pass approach double-wraps and leaves a stray
// reset mid-phrase. Order in the alternation matters (phrase first).
const STATE_RE = /administratively down|\b(?:up|down)\b/gi;

/**
 * Decide the colorized form of ONE complete line (terminator stripped by the
 * caller). Pure — no buffering, no side effects. Returns the line with SGR codes
 * injected, or the original line unchanged when no rule applies or the line is
 * unsafe to touch (already contains an ESC escape).
 */
export function colorizeLine(line: string): string {
  // Never rewrite a line that already carries escape sequences — it may be a
  // colored banner, a cursor-move, or terminal chatter. Leave it byte-for-byte.
  if (line.includes("\x1b")) return line;
  if (line.length === 0) return line;

  // Prompt line (highest priority; typically short, e.g. "R1(config-if)#"): make
  // the whole prompt cyan-bold so the eye finds command boundaries fast.
  if (PROMPT_RE.test(line)) {
    return `${CYAN_BOLD}${line}${RESET}`;
  }

  // %-notice/error/warning: tint the whole line yellow-orange.
  if (PERCENT_RE.test(line)) {
    return `${YELLOW}${line}${RESET}`;
  }

  // Otherwise do inline, token-level colorization: state words + IP addresses.
  // "up" -> green, everything else the pattern matches ("down" /
  // "administratively down") -> red.
  let out = line;
  out = out.replace(STATE_RE, (m) =>
    m.toLowerCase() === "up" ? `${GREEN}${m}${RESET}` : `${RED}${m}${RESET}`
  );
  out = out.replace(IPV4_RE, (m) => `${BLUE}${m}${RESET}`);
  return out;
}

/** Bytes emitted by the colorizer, in order. */
export type ColorizerSink = (text: string) => void;

/** Chunks at or below this size (with no newline, nothing held) are treated as
 *  interactive echo and forwarded raw synchronously. On the wire, per-keystroke
 *  echo is a 1-byte frame; 3 covers backspace ("\b \b") too. */
const ECHO_CHUNK_MAX = 3;

/** How long an incomplete line tail is held before being flushed raw (or
 *  prompt-colorized) — about one display frame, imperceptible on echo. */
const DEFAULT_FLUSH_MS = 16;

/**
 * Stateful colorizing transformer. One instance per console tab. Feed it each
 * decoded string chunk via push(); it emits transformed output through the sink
 * — synchronously for everything except an incomplete line tail, which is held
 * for at most flushDelayMs (see the header comment for the full rules).
 */
export class ConsoleColorizer {
  /** Raw bytes of the CURRENT line already emitted (0 = line untouched, so it
   *  may still be colorized when it completes). */
  private emittedInLine = 0;
  /** Held (not yet emitted) tail of the current line. Always emitted BEFORE any
   *  later chunk's bytes — ordering is inviolable. */
  private held = "";
  private timer: ReturnType<typeof setTimeout> | null = null;
  private sink: ColorizerSink;
  private flushDelayMs: number;

  // NOTE: plain field assignment, not TS parameter properties — the node test
  // runner type-strips this file and parameter properties aren't erasable.
  constructor(sink: ColorizerSink, flushDelayMs: number = DEFAULT_FLUSH_MS) {
    this.sink = sink;
    this.flushDelayMs = flushDelayMs;
  }

  /** Transform one chunk. Emits through the sink (usually synchronously within
   *  this call; an incomplete tail may follow up to flushDelayMs later). */
  push(chunk: string): void {
    if (chunk.length === 0) return;

    // Interactive echo fast path: tiny newline-less chunk, nothing held —
    // forward raw in the same call. The line is now "dirty" (partially shown),
    // so it can never be recolored.
    if (
      this.held === "" &&
      chunk.length <= ECHO_CHUNK_MAX &&
      !chunk.includes("\n") &&
      !chunk.includes("\x1b")
    ) {
      this.emittedInLine += chunk.length;
      this.sink(chunk);
      return;
    }

    // Merge the held tail back in front so ordering is preserved, then scan.
    this.cancelTimer();
    const text = this.held + chunk;
    this.held = "";

    let out = "";
    let segStart = 0;
    for (let i = 0; i < text.length; i++) {
      if (text[i] !== "\n") continue;
      const seg = text.slice(segStart, i); // line content in hand (no \n)
      if (this.emittedInLine === 0) {
        // Whole line is in hand (nothing was emitted raw): colorize. Strip a
        // trailing \r so colorizeLine sees the bare body, then re-attach the
        // exact terminator.
        const hasCr = seg.endsWith("\r");
        const body = hasCr ? seg.slice(0, -1) : seg;
        out += colorizeLine(body) + (hasCr ? "\r\n" : "\n");
      } else {
        // Part of this line already went out raw — emit the rest raw too.
        out += seg + "\n";
      }
      this.emittedInLine = 0; // next line starts fresh
      segStart = i + 1;
    }

    const tail = text.slice(segStart);
    if (tail === "") {
      if (out) this.sink(out);
      return;
    }
    // A dirty line's tail, or a tail carrying ESC (cursor moves, banners):
    // emit raw immediately — holding gains nothing / risks corruption.
    if (this.emittedInLine > 0 || tail.includes("\x1b")) {
      this.emittedInLine += tail.length;
      this.sink(out + tail);
      return;
    }
    // Clean incomplete tail: hold it for the flush window so the rest of the
    // line (or nothing — then it's likely a prompt) can decide its color.
    if (out) this.sink(out);
    this.held = tail;
    this.timer = setTimeout(() => {
      this.timer = null;
      this.flushHeld();
    }, this.flushDelayMs);
  }

  /**
   * Emit the held tail now. A tail that is exactly a prompt is colorized —
   * prompts never receive a terminator, so the flush window is their only
   * chance. Anything else goes out raw. Safe to call any time (idempotent when
   * nothing is held); ConsoleTerm calls it when colorizing is toggled off so no
   * bytes are ever stranded.
   */
  flushHeld(): void {
    this.cancelTimer();
    if (this.held === "") return;
    const tail = this.held;
    this.held = "";
    const isPrompt = this.emittedInLine === 0 && PROMPT_TAIL_RE.test(tail);
    this.emittedInLine += tail.length;
    this.sink(isPrompt ? `${CYAN_BOLD}${tail}${RESET}` : tail);
  }

  /** Drop all buffered state (e.g. on reconnect — the stream restarts). */
  reset(): void {
    this.cancelTimer();
    this.held = "";
    this.emittedInLine = 0;
  }

  private cancelTimer(): void {
    if (this.timer !== null) {
      clearTimeout(this.timer);
      this.timer = null;
    }
  }
}
