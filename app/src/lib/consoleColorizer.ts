// IOS console color highlighter — a stream transformer applied to node->browser
// bytes before xterm.write, mirroring the user's PNetLab webconsole colorizer.
//
// CORRECTNESS is the whole game here. Console bytes arrive in arbitrary chunk
// boundaries (a single \r\n can straddle two WS frames; a prompt the user is
// mid-typing on has no terminator yet). So the rule is:
//
//   - We only ever inject SGR color into a line ONCE IT IS COMPLETE, i.e. a run
//     terminated by \n (with optional preceding \r). That guarantees we never
//     touch the line the cursor is currently on, so keystroke echo is never
//     delayed, reordered, or recolored mid-edit.
//   - Any trailing partial line (no terminator yet) is buffered verbatim and
//     re-emitted UNMODIFIED on the next chunk. Nothing is held back that isn't
//     genuinely an incomplete line — a completed line always flushes in the same
//     chunk it terminated in, so interactive latency is unchanged.
//   - We colorize only lines that look "safe" (see rules). Control sequences,
//     escapes, and anything already carrying an ESC are passed through as-is so
//     we never corrupt cursor moves, colored banners, or NAWS chatter.
//
// The transformer is a tiny stateful object (one per console tab) rather than a
// pure fn because it must remember the partial-line tail across chunks. The
// per-line decision (`colorizeLine`) is a pure function, exported for testing.

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
 * Stateful, chunk-boundary-safe colorizing transformer. One instance per console
 * tab. Feed it each decoded string chunk; it returns the string to hand to
 * xterm.write — completed lines colorized, the trailing partial line passed
 * through unmodified and remembered for the next call.
 */
export class ConsoleColorizer {
  /** Bytes seen since the last line terminator — an incomplete line, verbatim. */
  private tail = "";

  /**
   * Transform one chunk. Preserves every terminator exactly ("\n" and "\r\n"
   * both survive), so xterm's convertEol/cursor behavior is unchanged; only the
   * printable content of a completed line is wrapped in SGR codes.
   */
  push(chunk: string): string {
    const data = this.tail + chunk;
    // Split keeping terminators. We walk manually rather than String.split so a
    // lone trailing "\r" (possible first half of a \r\n split across chunks) is
    // held in the tail instead of being treated as a completed line.
    let out = "";
    let i = 0;
    let lineStart = 0;
    while (i < data.length) {
      const c = data[i];
      if (c === "\n") {
        // Completed line runs [lineStart, i); it may end with a \r we keep.
        const raw = data.slice(lineStart, i);
        const hasCr = raw.endsWith("\r");
        const body = hasCr ? raw.slice(0, -1) : raw;
        out += colorizeLine(body) + (hasCr ? "\r\n" : "\n");
        i++;
        lineStart = i;
        continue;
      }
      i++;
    }
    // Whatever follows the last \n is an incomplete line (which may be just a
    // lone "\r"): buffer it verbatim and re-emit unmodified next time.
    this.tail = data.slice(lineStart);
    return out;
  }

  /** Drop buffered state (e.g. on reconnect). */
  reset(): void {
    this.tail = "";
  }
}
