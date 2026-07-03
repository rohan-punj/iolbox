// Unit tests for the v2 stream colorizer (see app/src/lib/consoleColorizer.ts).
// Run with plain node (>= 22.6, type stripping): from app/:
//   node --test tests/consoleColorizer.test.ts
// Deliberately outside src/ so svelte-check's tsconfig (no
// allowImportingTsExtensions) doesn't see the explicit-.ts import node needs.
import { test } from "node:test";
import assert from "node:assert/strict";
import { ConsoleColorizer, colorizeLine } from "../src/lib/consoleColorizer.ts";

const ESC = "\x1b[";
const RESET = "\x1b[0m";
const CYAN_BOLD = `${ESC}1;36m`;
const GREEN = `${ESC}32m`;
const RED = `${ESC}31m`;

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

// ---- colorizeLine (pure) ----

test("colorizeLine: up green, down red, IPs blue", () => {
  const line = "Ethernet0/0  10.0.0.1  YES NVRAM  up  up";
  const got = colorizeLine(line);
  assert.ok(got.includes(`${GREEN}up${RESET}`), "up must be green");
  assert.ok(got.includes(`${ESC}38;5;75m10.0.0.1${RESET}`), "IP must be blue");
});

test("colorizeLine: administratively down is ONE red phrase", () => {
  const got = colorizeLine("Ethernet0/1  unassigned  YES unset  administratively down down");
  assert.ok(got.includes(`${RED}administratively down${RESET}`));
});

test("colorizeLine: ESC-bearing lines untouched", () => {
  const line = `already ${ESC}31mred${RESET} here up down`;
  assert.equal(colorizeLine(line), line);
});

// ---- streaming transformer ----

test("bulk multi-line chunk: every complete line colorized", () => {
  const h = harness();
  h.c.push("Ethernet0/0  10.0.0.1  YES NVRAM  up  up\r\nEthernet0/1  unassigned  YES unset  administratively down down\r\n");
  const got = h.text();
  assert.ok(got.includes(`${GREEN}up${RESET}`), "green up");
  assert.ok(got.includes(`${RED}administratively down${RESET}`), "red admin-down");
  assert.ok(got.endsWith("\r\n"), "terminators preserved exactly");
});

test("typed echo byte-by-byte: instant, raw, synchronous", () => {
  const h = harness();
  for (const ch of "show") {
    const before = h.out.length;
    h.c.push(ch);
    assert.equal(h.out.length, before + 1, "each byte emitted in the same push call");
    assert.equal(h.out[h.out.length - 1], ch, "byte emitted raw");
  }
});

test("NEW: line split across chunks (body + terminator) still colorized", async () => {
  const h = harness();
  h.c.push("Ethernet0/0  10.0.0.1  YES NVRAM  up  up"); // no terminator: held
  h.c.push("\r\n"); // terminator lands in the NEXT chunk
  const got = h.text();
  assert.ok(got.includes(`${GREEN}up${RESET}`), "split line must still color");
  assert.ok(got.endsWith("\r\n"));
  await h.settle(); // nothing further may arrive
  assert.equal(h.text(), got);
});

test("NEW: mid-word splits (real IOL framing) reassemble and colorize", () => {
  const h = harness();
  // Mirrors observed frames: "...Ethernet0/2            " | "u" | "nassigned ... down\r\n"
  h.c.push("Ethernet0/2            ");
  h.c.push("u");
  h.c.push("nassigned      YES unset  administratively down down    \r\n");
  const got = h.text();
  assert.ok(got.includes(`${RED}administratively down${RESET}`));
  assert.equal(
    got.replace(/\x1b\[[0-9;]*m/g, ""),
    "Ethernet0/2            unassigned      YES unset  administratively down down    \r\n",
    "byte content identical modulo SGR"
  );
});

test("held tail flushes raw after the window (no color for non-prompts)", async () => {
  const h = harness();
  h.c.push("partial line without termina");
  assert.equal(h.text(), "", "tail held during the window");
  await h.settle();
  assert.equal(h.text(), "partial line without termina", "flushed raw, byte-exact");
});

test("prompt tail is colorized on flush", async () => {
  const h = harness();
  h.c.push("\r\nR1>"); // exactly how the prompt arrives on the wire
  await h.settle();
  assert.equal(h.text(), `\r\n${CYAN_BOLD}R1>${RESET}`);
});

test("config-mode prompt tail colorized; prompt+typed-echo tail is NOT", async () => {
  const h = harness();
  h.c.push("SW1(config-if)#");
  await h.settle();
  assert.equal(h.text(), `${CYAN_BOLD}SW1(config-if)#${RESET}`);

  const h2 = harness();
  h2.c.push("R1#show ru"); // tail already carries typed text
  await h2.settle();
  assert.equal(h2.text(), "R1#show ru", "not wholesale-colored");
});

test("dirty line is never recolored, terminator preserved", async () => {
  const h = harness();
  for (const ch of "up down 10.0.0.1") h.c.push(ch); // echo path → all raw
  h.c.push("\r\nnext up\r\n"); // terminator for the dirty line + a fresh line
  const got = h.text();
  assert.ok(got.startsWith("up down 10.0.0.1\r\n"), "dirty line stays raw");
  assert.ok(got.includes(`next ${GREEN}up${RESET}`), "following fresh line colorized");
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
  assert.ok(h.text().includes(CYAN_BOLD));
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
  const strip = (s: string) => s.replace(/\x1b\[[0-9;]*m/g, "");
  assert.equal(strip(bulk.text()), text);
  assert.equal(strip(split.text()), text);
});
