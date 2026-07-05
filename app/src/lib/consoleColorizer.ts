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
// Byte ordering is inviolboxle: held bytes are always emitted before any bytes
// from a later chunk, and terminators ("\n", "\r\n") are preserved exactly.
//
// The transformer emits through a SINK callback (not a return value) because
// the flush window makes emission asynchronous. One instance per console tab;
// the per-line decision (`colorizeLine`) stays a pure exported function.

// SecureCRT "Cisco Words" keyword-highlighting rules, in priority order (first
// rule to claim a character wins). Ported verbatim from the PNetLab web-console
// (engine-custom/opt/unetlab/html/console/vendor/securecrt-cisco-rules.js, itself
// generated from SecureCRT's "Cisco Words for BlackBckgrd.ini") so the iolbox
// console highlights IOS/NX-OS output exactly like the PNetLab HTML5 console.
// Each rule is a global, case-insensitive RegExp + a 24-bit RGB colour. Kept
// inline (not a separate module) so the plain `node --test` runner, which does
// not resolve extensionless .ts imports, can load this file directly.
interface CiscoRule {
  re: RegExp;
  r: number;
  g: number;
  b: number;
}

const CISCO_RULES: CiscoRule[] = [
  { re: /.*'.*/gi, r: 255, g: 255, b: 255 },
  { re: /on-fail.*/gi, r: 255, g: 0, b: 0 },
  { re: /^\w[^>]*#|hostname/gi, r: 0, g: 255, b: 255 },
  { re: /^\w[^#]*>/gi, r: 127, g: 255, b: 212 },
  { re: /Embedded-Service-Engine\d\/\d/gi, r: 0, g: 255, b: 255 },
  { re: /.*thernet[0-9]+(?:[\/.:][0-9]+)+[,:]?(?:\x20|$)/gi, r: 0, g: 255, b: 255 },
  { re: /.*thernet[0-9]+[,:]?(?:\x20|$)/gi, r: 0, g: 255, b: 255 },
  { re: /\b[efgt][a-z]*[0-9]+(?:[\/.:][0-9]+)+[,:*]?(?:\x20|$)/gi, r: 0, g: 255, b: 255 },
  { re: /[fgm][aeu][0-9]+[,:*]?(?:\x20|$)/gi, r: 0, g: 255, b: 255 },
  { re: /(?:nvi|port-channel|Serial|Po|vfc)[0-9|\/|:|,]+[,:]?(?:\x20|$)/gi, r: 0, g: 255, b: 255 },
  { re: /(?:multi|lo[^c]|tun|mgmt|null)[a-z]*[0-9]+,?/gi, r: 0, g: 255, b: 255 },
  { re: /con[0-9]?|vty|line|aux|console/gi, r: 0, g: 255, b: 255 },
  { re: /wwn|pwwn|(?:[a-f0-9]{2}:){7}[a-f0-9]{2}/gi, r: 255, g: 127, b: 80 },
  { re: /i?(?:[0-9]{1,3}\.){3}[0-9]{1,3}(?:\/[0-9]{1,2}|:[0-9]{1,5})?(?:,(?:[0-9]{1,5})?)?/gi, r: 0, g: 255, b: 127 },
  { re: /(?:)(?:0|255)\.(?:[0-9]{1,3}\.){2}[0-9]{1,3}/gi, r: 173, g: 255, b: 47 },
  { re: /(?:[a-f0-9]{2}[:-]){5}[a-f0-9]{2}/gi, r: 255, g: 215, b: 0 },
  { re: /[a-f0-9]{4}\.[a-f0-9]{4}\.[a-f0-9]{4}/gi, r: 255, g: 215, b: 0 },
  { re: /\d*\d*\.\d*\((?:\d|[a-z])*\)[^,]*|\d\d?\.\d\d?\.\d\d?\.\D\D?/gi, r: 255, g: 140, b: 0 },
  { re: /(?:[a-z]{2}.\d{4}.{4})/gi, r: 255, g: 140, b: 0 },
  { re: /(?:yes|permit|\[OK\]|on|enabled).?/gi, r: 50, g: 205, b: 50 },
  { re: /down->up|running|.*SUCCESS.*|.*success.*|up.?(?:\x20|$)|passed.*|Complete/gi, r: 50, g: 205, b: 50 },
  { re: /.*_ERR:|.*fail.*|invalid|.*reload.*/gi, r: 255, g: 0, b: 0 },
  { re: /no|administratively|shut.*|never,?|deny|.*down.?/gi, r: 255, g: 0, b: 0 },
  { re: /not|initializing.?|Off|1024|768|des|des56.*/gi, r: 255, g: 0, b: 0 },
  { re: /telnet|half-duplex.?|\(err-disabled\)|disabled/gi, r: 255, g: 0, b: 0 },
  { re: /up->down|trunk|Active|inhibit/gi, r: 255, g: 0, b: 0 },
  { re: /(?:class|policy|service|parameter|match)(?:-map.?|-policy.?)?/gi, r: 255, g: 165, b: 0 },
  { re: /(?:version|PN|SN|S\/N|ID|PID|VID|NAME|DESCR):?/gi, r: 255, g: 165, b: 0 },
  { re: /Device|ID|Local|Intrfce|Holdtme|Capability|Platform|Port|ID/gi, r: 255, g: 165, b: 0 },
  { re: /H|Address|Interface|Hold|Uptime|SRTT|RTO|Q|Seq/gi, r: 255, g: 165, b: 0 },
  { re: /Neighbor|V|AS|MsgRcvd|MsgSent|TblVer|InQ|OutQ|Up\/Down|State\/Pf.*/gi, r: 255, g: 165, b: 0 },
  { re: /interface|IP-Address|Method|OK\?|Status|Protocol/gi, r: 255, g: 165, b: 0 },
  { re: /aaa|vlan\d*|description.?|MTU|BW|DLY|Vl[0-9]+/gi, r: 255, g: 165, b: 0 },
  { re: /bits\/sec,|packets\/sec/gi, r: 0, g: 255, b: 127 },
  { re: /Building|configuration\.\.\./gi, r: 255, g: 0, b: 0 },
  { re: /erase|remove|delete./gi, r: 255, g: 0, b: 0 },
  { re: /\[confirm\]|\(yes\/no\):.*|\[yes\/no\]:.*|.*-more-.*/gi, r: 255, g: 0, b: 0 },
  { re: /.*-more-.*/gi, r: 255, g: 0, b: 0 },
  { re: /username.*|password.*|key|.*-key/gi, r: 255, g: 127, b: 80 },
  { re: /\(hitcnt=0\)/gi, r: 255, g: 255, b: 255 },
  { re: /\(hitcnt=[1-9][0-9]*\)/gi, r: 255, g: 127, b: 80 },
  { re: /access-(?:lists?|class|group)|use-acl|prefix-list/gi, r: 255, g: 140, b: 0 },
  { re: /time-range|object-group|route-map/gi, r: 255, g: 140, b: 0 },
  { re: /remark|\*+|!+|###+|@+$/gi, r: 0, g: 255, b: 127 },
  { re: /\[#+(?:\x20|$)|^\](?:\x20|$)/gi, r: 255, g: 255, b: 0 },
  { re: /\[#+\]/gi, r: 124, g: 252, b: 0 },
  { re: /ftp|tcp|udp|tftp|scp|ssh|ntp|snmp.*|inspect|icmp/gi, r: 255, g: 105, b: 180 },
  { re: /router|eigrp|bgp|ospf|rip|gre|hsrp/gi, r: 255, g: 105, b: 180 },
  { re: /Syslog_Messages/gi, r: 255, g: 140, b: 0 },
  { re: /%.+-[0-9]-.+:|\b.*\.(?:bin|tar)/gi, r: 255, g: 127, b: 80 },
];

// A held TAIL that is exactly a prompt (nothing after the > or #). Anchored both
// ends: a tail like "R1#sh" is a prompt + typed echo and must NOT be colorized on
// flush (that would tint the typed command too). Prompts never receive a
// terminator, so the flush window is the only chance to color a bare prompt.
const PROMPT_TAIL_RE = /^[\w.-]+(\([\w-]+\))?[>#] ?$/;

// A JS-word character (used for SecureCRT-style word-boundary rejection below).
function isWordChar(ch: string): boolean {
  const c = ch.charCodeAt(0);
  return (c >= 48 && c <= 57) || (c >= 65 && c <= 90) || (c >= 97 && c <= 122) || c === 95;
}

/**
 * Colorize ONE complete line (terminator stripped by the caller) using the
 * SecureCRT "Cisco Words" rule set (securecrtRules.ts), the exact algorithm the
 * PNetLab HTML5 console uses: walk the rules in priority order, let the FIRST
 * rule that claims a character own it, then emit 24-bit truecolor SGR runs.
 * Matches are rejected when they start or end inside a word so keyword rules
 * never tint fragments (e.g. "port" inside "transport").
 *
 * Pure — no buffering, no side effects. Returns the line unchanged when nothing
 * matches, the line is empty/pathologically long, or it already carries an ESC
 * escape (a colored banner or cursor-addressed output we must not corrupt).
 */
export function colorizeLine(line: string): string {
  if (line.includes("\x1b")) return line;
  const n = line.length;
  if (n === 0 || n > 1000) return line;

  // owner[k] = index of the first CISCO_RULE that claims character k (-1 = none).
  const owner = new Array<number>(n).fill(-1);
  for (let ri = 0; ri < CISCO_RULES.length; ri++) {
    const re = CISCO_RULES[ri].re;
    re.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = re.exec(line)) !== null) {
      const a = m.index;
      const b = re.lastIndex;
      if (b === a) {
        re.lastIndex++; // zero-width match: step past it so exec() terminates
        continue;
      }
      // Reject a match that starts or ends mid-word (SecureCRT keys on word
      // boundaries — symbols/spaces delimit words).
      if (a > 0 && isWordChar(line[a - 1]) && isWordChar(line[a])) continue;
      if (b < n && isWordChar(line[b]) && isWordChar(line[b - 1])) continue;
      for (let k = a; k < b; k++) if (owner[k] === -1) owner[k] = ri;
    }
  }

  let out = "";
  let cur = -1;
  for (let k = 0; k < n; k++) {
    if (owner[k] !== cur) {
      if (cur !== -1) out += "\x1b[39m"; // reset foreground to default
      cur = owner[k];
      if (cur !== -1) {
        const c = CISCO_RULES[cur];
        out += `\x1b[38;2;${c.r};${c.g};${c.b}m`;
      }
    }
    out += line[k];
  }
  if (cur !== -1) out += "\x1b[39m";
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
   *  later chunk's bytes — ordering is inviolboxle. */
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
    this.sink(isPrompt ? colorizeLine(tail) : tail);
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
