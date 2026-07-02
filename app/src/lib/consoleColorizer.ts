// IOS console color highlighter — a stream transformer applied to node->browser
// bytes before xterm.write, mirroring the user's PNetLab webconsole colorizer.
//
// CORRECTNESS is the whole game here, and the #1 rule is PASS-THROUGH LATENCY:
// every byte that arrives is forwarded to xterm IN THE SAME push() call it
// arrived in. Nothing is ever held back. This is what keeps interactive echo
// instant — the user types, IOS echoes a char, and that char lands on screen in
// the same frame, on the current row, with the cursor exactly where it should be.
//
// Colorization is a *best-effort overlay* that only fires when it can be done
// without adding a single byte of latency:
//
//   - We track how many bytes of the CURRENT (still-unterminated) line have
//     already been emitted raw this session (`emittedInLine`). When a chunk
//     contains the line's terminating \n we may colorize ONLY IF
//     `emittedInLine === 0` — i.e. the whole line arrived complete inside this
//     one chunk (bulk output like `show run` streaming in full lines). To
//     "recolor" we emit a carriage return + the colorized line, overwriting the
//     raw copy we would otherwise have emitted; but since emittedInLine===0 we
//     haven't emitted any of it yet, so we simply emit the colored form instead
//     of the raw form. No overwrite, no flicker, no latency.
//   - If ANY of the line was already passed through raw (emittedInLine > 0), the
//     line is an interactive/streamed-mid-line case: we emit the remaining bytes
//     RAW and never recolor. Typed lines therefore never get recolored — correct
//     and expected.
//   - Lines that contain an ESC pass through untouched (cursor moves, colored
//     banners, NAWS chatter — never corrupt them).
//
// EQUIVALENCE (the property the tests pin down):
//   - Feed a chunk of whole lines ("foo\nbar\n") in one push and each complete
//     line is colorized.
//   - Feed the SAME text one byte at a time and every byte is returned
//     immediately and RAW (byte-for-byte identical input→output, zero added
//     latency), with no color — because each line's \n arrives in a push where
//     emittedInLine > 0.
//   Both render identically in the terminal apart from the SGR codes.
//
// The transformer is a tiny stateful object (one per console tab) rather than a
// pure fn because it must remember `emittedInLine` across chunks. The per-line
// decision (`colorizeLine`) is a pure function, exported for testing.

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

/**
 * Stateful, pass-through-immediately colorizing transformer. One instance per
 * console tab. Feed it each decoded string chunk; it returns the string to hand
 * to xterm.write. EVERY input byte is forwarded within the same call — nothing
 * is buffered/held back. A completed line is colorized ONLY when the whole line
 * arrived inside a single chunk (no part of it was emitted raw first); otherwise
 * the bytes go through raw so interactive echo is never delayed or recolored.
 */
export class ConsoleColorizer {
  /**
   * How many raw bytes of the current (still-unterminated) line have already
   * been forwarded to xterm. 0 means "nothing of this line has been shown yet",
   * which is the only condition under which we're allowed to colorize the line
   * when its terminator arrives.
   */
  private emittedInLine = 0;

  /**
   * Transform one chunk, forwarding every byte immediately. Preserves every
   * terminator exactly ("\n" and "\r\n" both survive), so xterm's convertEol /
   * cursor behavior is unchanged; only fully-in-this-chunk lines are wrapped in
   * SGR codes.
   */
  push(chunk: string): string {
    if (chunk.length === 0) return "";
    let out = "";
    let segStart = 0; // start of the not-yet-emitted run within `chunk`
    let i = 0;
    while (i < chunk.length) {
      if (chunk[i] === "\n") {
        // The line terminates here. The portion of this line living in THIS
        // chunk is chunk[segStart..i) (the terminator is at i). Whether we may
        // colorize depends on emittedInLine: if 0, the entire line is in hand.
        const seg = chunk.slice(segStart, i); // this-chunk part of the line, no \n
        const canColorize = this.emittedInLine === 0;
        if (canColorize) {
          // Whole line is in `seg`. Strip a trailing \r so colorizeLine sees the
          // bare body, then re-attach the exact terminator we found.
          const hasCr = seg.endsWith("\r");
          const body = hasCr ? seg.slice(0, -1) : seg;
          out += colorizeLine(body) + (hasCr ? "\r\n" : "\n");
        } else {
          // Part of this line already went out raw earlier — emit the remaining
          // bytes (this-chunk tail + terminator) raw and DO NOT recolor.
          out += seg + "\n";
        }
        this.emittedInLine = 0; // next line starts fresh
        i++;
        segStart = i;
        continue;
      }
      i++;
    }
    // Trailing bytes after the last \n are an incomplete line: forward them raw
    // NOW (never buffered) and remember we've shown that many bytes of it, so the
    // line can't be recolored when its terminator arrives in a later chunk.
    if (segStart < chunk.length) {
      const rest = chunk.slice(segStart);
      out += rest;
      this.emittedInLine += rest.length;
    }
    return out;
  }

  /** Drop buffered state (e.g. on reconnect). */
  reset(): void {
    this.emittedInLine = 0;
  }
}
