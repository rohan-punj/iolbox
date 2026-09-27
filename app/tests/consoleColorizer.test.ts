// Unit tests for the console colorizer (see app/src/lib/consoleColorizer.ts).
// Run with plain node (>= 22.6, type stripping): from app/:
//   node --test tests/consoleColorizer.test.ts
// Deliberately outside src/ so svelte-check's tsconfig (no
// allowImportingTsExtensions) doesn't see the explicit-.ts import node needs.
import { test } from "node:test";
import assert from "node:assert/strict";
import { ConsoleColorizer, colorizeLine } from "../src/lib/consoleColorizer.ts";

// SecureCRT "Cisco Words" truecolor SGR the ported rule set emits.
const CYAN = "\x1b[38;2;0;255;255m"; // prompts ending in '#', interfaces
const AQUA = "\x1b[38;2;127;255;212m"; // user-exec prompts ending in '>'
const R39 = "\x1b[39m"; // reset foreground only (SecureCRT rules use [39m, not [0m)
const ESC = "\x1b[";
// Any 24-bit foreground colour (a rule fired on the span).
const TRUECOLOR = /\x1b\[38;2;\d+;\d+;\d+m/;
const strip = (s: string) => s.replace(/\x1b\[[0-9;]*m/g, "");

/** Collects sink output; helpers to wait for the flush window. */
function harness(flushMs = 5) {
  const out: string[] = [];
  const c = new ConsoleColorizer((s) => out.push(s), flushMs);
  return {
    c,
    out,
    text: () => out.join(""),
    settle: () => new Promise((r) => setTimeout(r, flushMs + 25)),
  };
}

// ---- colorizeLine (pure, SecureCRT rule set) ----

test("colorizeLine: privileged prompt '#' is cyan, whole prompt only", () => {
  assert.equal(colorizeLine("R1#"), `${CYAN}R1#${R39}`);
  assert.equal(colorizeLine("SW1(config-if)#"), `${CYAN}SW1(config-if)#${R39}`);
});

test("colorizeLine: user-exec prompt '>' is aquamarine", () => {
  assert.equal(colorizeLine("R1>"), `${AQUA}R1>${R39}`);
});

test("colorizeLine: prompt PREFIX only — command text after #/> keeps its own rules", () => {
  // SecureCRT's `^\w[^>]*#` claims only up to the prompt '#', so the command echo
  // is not swept into the prompt colour.
  const got = colorizeLine("R1#do sh run");
  assert.ok(got.startsWith(`${CYAN}R1#${R39}`), "prompt prefix cyan");
  assert.equal(strip(got), "R1#do sh run", "content preserved");
});

test("colorizeLine: interface names get colour", () => {
  const got = colorizeLine("Ethernet0/0 is up, line protocol is up");
  assert.match(got, TRUECOLOR, "an interface/keyword span is coloured");
  assert.equal(strip(got), "Ethernet0/0 is up, line protocol is up");
});

test("colorizeLine: IPv4 address gets colour", () => {
  const got = colorizeLine("  Internet address is 10.0.0.1/24");
  assert.match(got, TRUECOLOR);
  assert.equal(strip(got), "  Internet address is 10.0.0.1/24");
});

test("colorizeLine: content is always preserved modulo SGR", () => {
  const line = "Ethernet0/1  unassigned  YES unset  administratively down  down";
  assert.equal(strip(colorizeLine(line)), line);
});

test("colorizeLine: ESC-bearing lines untouched", () => {
  const line = `already ${ESC}31mred${ESC}0m here up down`;
  assert.equal(colorizeLine(line), line);
});

test("colorizeLine: empty and no-match lines returned unchanged", () => {
  assert.equal(colorizeLine(""), "");
  assert.equal(colorizeLine("     "), "     "); // whitespace: no rule claims it
});

// ---- streaming transformer ----

test("bulk multi-line chunk: every complete line colorized, terminators exact", () => {
  const h = harness();
  h.c.push("Ethernet0/0 is up\r\nEthernet0/1 is administratively down\r\n");
  const got = h.text();
  assert.match(got, TRUECOLOR, "lines colorized");
  assert.ok(got.endsWith("\r\n"), "terminators preserved exactly");
  assert.equal(strip(got), "Ethernet0/0 is up\r\nEthernet0/1 is administratively down\r\n");
});

test("typed echo byte-by-byte: instant, raw, synchronous", () => {
  const h = harness();
  h.c.noteInput("show");
  for (const ch of "show") {
    const before = h.out.length;
    h.c.push(ch);
    assert.equal(h.out.length, before + 1, "each byte emitted in the same push call");
    assert.equal(h.out[h.out.length - 1], ch, "byte emitted raw");
  }
});

test("line split across chunks (body + terminator) still colorized", async () => {
  const h = harness();
  h.c.push("Ethernet0/0 is up"); // no terminator: held
  h.c.push("\r\n"); // terminator lands in the NEXT chunk
  const got = h.text();
  assert.match(got, TRUECOLOR, "split line must still color");
  assert.ok(got.endsWith("\r\n"));
  await h.settle();
  assert.equal(h.text(), got);
});

test("mid-word splits (real IOL framing) reassemble and colorize", () => {
  const h = harness();
  // Mirrors observed frames: "...Ethernet0/2            " | "u" | "nassigned ...\r\n"
  h.c.push("Ethernet0/2            ");
  h.c.push("u");
  h.c.push("nassigned      YES unset  administratively down down    \r\n");
  const got = h.text();
  assert.match(got, TRUECOLOR);
  assert.equal(
    strip(got),
    "Ethernet0/2            unassigned      YES unset  administratively down down    \r\n",
    "byte content identical modulo SGR"
  );
});

test("held non-prompt tail flushes raw after the window", async () => {
  const h = harness();
  h.c.push("partial line without termina");
  assert.equal(h.text(), "", "tail held during the window");
  await h.settle();
  assert.equal(h.text(), "partial line without termina", "flushed raw, byte-exact");
});

test("prompt tail is colorized immediately and stays unchanged after the flush window", async () => {
  const h = harness();
  h.c.push("\r\nR1>"); // exactly how the prompt arrives on the wire
  assert.equal(h.text(), `\r\n${AQUA}R1>${R39}`);
  await h.settle();
  assert.equal(h.text(), `\r\n${AQUA}R1>${R39}`);
});

test("config-mode prompt tail colorized; prompt+typed-echo tail is NOT", async () => {
  const h = harness();
  h.c.push("SW1(config-if)#");
  await h.settle();
  assert.equal(h.text(), `${CYAN}SW1(config-if)#${R39}`);

  const h2 = harness();
  h2.c.push("R1#show ru"); // tail already carries typed text
  await h2.settle();
  assert.equal(h2.text(), "R1#show ru", "not wholesale-colored");
});

test("bug1 regression: prompt followed by bulk body — body NOT swept into prompt colour", async () => {
  const h = harness();
  h.c.push("\r\nR1#"); // prompt tail — colored immediately
  h.c.push("version 17.18\r\n!\r\nservice timestamps debug\r\nhostname R1\r\n");
  const got = h.text();
  // The prompt's foreground resets before the following body arrives, so
  // the body cannot inherit cyan. Later complete lines get their own rules.
  assert.ok(got.includes(`${CYAN}R1#${R39}`), "prompt cyan reset before the body");
  assert.ok(!got.includes(`${CYAN}R1#${R39}version 17.18${R39}`), "body not inside the prompt span");
  assert.equal(
    strip(got),
    "\r\nR1#version 17.18\r\n!\r\nservice timestamps debug\r\nhostname R1\r\n",
    "content preserved"
  );
  await h.settle();
});

test("dirty line is never recolored, terminator preserved", async () => {
  const h = harness();
  h.c.noteInput("up down 10.0.0.1");
  for (const ch of "up down 10.0.0.1") h.c.push(ch); // echo path → all raw
  h.c.push("\r\nEthernet0/0 is up\r\n"); // terminator for the dirty line + a fresh line
  const got = h.text();
  assert.ok(got.startsWith("up down 10.0.0.1\r\n"), "dirty line stays raw");
  assert.match(got.slice("up down 10.0.0.1\r\n".length), TRUECOLOR, "following fresh line colorized");
});

test("prompt color is identical at every frame split, including one-byte frames", () => {
  for (const prompt of ["R0#", "R1>", "R0(config-if-range)#"]) {
    const text = `\r\n${prompt}`;
    const expected = `\r\n${colorizeLine(prompt)}`;
    for (let split = 0; split <= text.length; split++) {
      const h = harness();
      h.c.push(text.slice(0, split));
      h.c.push(text.slice(split));
      assert.equal(h.text(), expected, `split ${split}: ${prompt}`);
      h.c.reset();
    }
    const h = harness();
    for (const ch of text) h.c.push(ch);
    assert.equal(h.text(), expected, `one-byte frames: ${prompt}`);
  }
});

test("rapid Enter keeps every fragmented prompt colored without a flush delay", () => {
  const h = harness();
  for (let i = 0; i < 20; i++) {
    h.c.noteInput("\r");
    for (const ch of "\r\nR0#") h.c.push(ch);
  }
  assert.equal(h.text(), `\r\n${CYAN}R0#${R39}`.repeat(20));
});

test("input flushes held output before immediate echo, preserving order", () => {
  const h = harness(10_000);
  h.c.push("incomplete output");
  h.c.noteInput("s");
  h.c.push("s");
  assert.equal(h.text(), "incomplete outputs");
  h.c.reset();
});

test("partial fragments do not extend the maximum output hold", (t) => {
  t.mock.timers.enable({ apis: ["Date", "setTimeout"], now: 0 });
  const h = harness(16);
  h.c.push("partial");
  t.mock.timers.tick(8);
  h.c.push(" output");
  t.mock.timers.tick(8);
  assert.equal(h.text(), "partial output", "flushed 16ms after the first fragment");
  h.c.reset();
});

test("a completed line gives the next incomplete tail its own flush window", (t) => {
  t.mock.timers.enable({ apis: ["Date", "setTimeout"], now: 0 });
  const h = harness(16);
  h.c.push("Ethernet0/0");
  t.mock.timers.tick(8);
  h.c.push(" is up\r\nnext tail");
  const completedLine = colorizeLine("Ethernet0/0 is up") + "\r\n";
  assert.equal(h.text(), completedLine);
  t.mock.timers.tick(8);
  assert.equal(h.text(), completedLine, "old line's deadline does not flush the new tail");
  t.mock.timers.tick(8);
  assert.equal(h.text(), completedLine + "next tail");
  h.c.reset();
});

test("small initial output fragments reassemble into colored complete lines", () => {
  const h = harness();
  const text = "Ethernet0/0 is up\r\n";
  for (const ch of text) h.c.push(ch);
  assert.equal(h.text(), colorizeLine("Ethernet0/0 is up") + "\r\n");
});

test("ESC in tail is never held (emitted immediately, raw)", () => {
  const h = harness();
  h.c.push(`tail with ${ESC}2K erase`);
  assert.equal(h.text(), `tail with ${ESC}2K erase`, "ESC tail bypasses the hold window");
});

test("reset drops held bytes (reconnect semantics)", async () => {
  const h = harness();
  h.c.push("stale tail");
  h.c.reset();
  await h.settle();
  assert.equal(h.text(), "", "held bytes dropped by reset");
  h.c.push("R1#\r\n"); // fresh stream colorizes normally
  assert.match(h.text(), TRUECOLOR);
});

test("flushHeld emits held bytes on demand (colorize-toggle-off path)", () => {
  const h = harness(10_000); // effectively no timer
  h.c.push("held text");
  assert.equal(h.text(), "");
  h.c.flushHeld();
  assert.equal(h.text(), "held text");
});

test("equivalence: same text bulk vs split renders identical modulo SGR", async () => {
  const text = "Interface  IP-Address  Status\r\nEthernet0/0  10.0.0.1  up\r\nR1#";
  const bulk = harness();
  bulk.c.push(text);
  await bulk.settle();
  const split = harness();
  for (let i = 0; i < text.length; i += 7) split.c.push(text.slice(i, i + 7));
  await split.settle();
  assert.equal(strip(bulk.text()), text);
  assert.equal(strip(split.text()), text);
});
