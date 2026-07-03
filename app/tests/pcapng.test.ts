// Unit tests for the packet summarizer's 802.3 LLC/SNAP decoding (see
// app/src/lib/pcapng.ts). Run with plain node (>= 22.6, type stripping) from app/:
//   node --test tests/pcapng.test.ts
// Outside src/ so svelte-check's tsconfig doesn't see the explicit-.ts import.
import { test } from "node:test";
import assert from "node:assert/strict";
import { summarize } from "../src/lib/pcapng.ts";

/** hex string -> Uint8Array (frame bytes). */
function h(s: string): Uint8Array {
  return new Uint8Array(Buffer.from(s.replace(/\s+/g, ""), "hex"));
}

// ---- STP (DSAP/SSAP 0x42) ----

test("STP config BPDU (RSTP) → root bridge id + cost", () => {
  // dst 01:80:c2:00:00:00, src aa:bb:cc:00:01:00, length 0x0027 (39, < 0x0600).
  // LLC 42 42 03, then BPDU: proto 0000 version 02(RSTP) type 02 flags 3c
  // root-id 2000/aa:bb:cc:00:01:00, root-path-cost 00000000.
  const frame = h(
    "0180c2000000 aabbcc000100 0027" +
      "424203" +
      "0000 02 02 3c" +
      "2000 aabbcc000100" + // root bridge id: prio 0x2000, mac
      "00000000" + // root path cost
      "2000aabbcc0001008000000000001400020f00"
  );
  const s = summarize(frame, 60);
  assert.equal(s.proto, "STP");
  assert.equal(s.addr, "aa:bb:cc:00:01:00 > 01:80:c2:00:00:00");
  assert.ok(s.info.includes("RSTP"), "version 2 → RSTP");
  assert.ok(s.info.includes("root 8192/aa:bb:cc:00:01:00"), `root id in info: ${s.info}`);
  assert.ok(s.info.includes("cost 0"), `path cost in info: ${s.info}`);
});

test("STP legacy config BPDU → 'STP', MST → 'MST'", () => {
  const stp = h("0180c2000000 aabbcc000100 0027 424203 0000 00 00 00 2000aabbcc00010000000000 00000000000000000000");
  assert.ok(summarize(stp, 60).info.startsWith("STP"));
  const mst = h("0180c2000000 aabbcc000100 0027 424203 0000 03 02 3c 8000aabbcc00020000000005 00000000000000000000");
  assert.ok(summarize(mst, 60).info.startsWith("MST"), "version 3 → MST");
});

test("STP TCN BPDU → 'Topology change'", () => {
  // TCN: LLC 42 42 03, proto 0000 version 00 type 80.
  const frame = h("0180c2000000 aabbcc000100 0007 424203 0000 00 80");
  const s = summarize(frame, 60);
  assert.equal(s.proto, "STP");
  assert.equal(s.info, "Topology change");
});

// ---- SNAP: CDP / DTP / VTP (OUI 00:00:0c) ----

test("CDP (real IOL frame) → Device ID TLV string", () => {
  // Trimmed real capture: dst 01:00:0c:cc:cc:cc, SNAP OUI 00000c pid 2000,
  // CDP hdr 02 b4 da2c, Device ID TLV 0001 0006 "R2".
  const frame = h(
    "01000ccccccc" + // dst 01:00:0c:cc:cc:cc
      "aabbcc000200 017d" +
      "aaaa0300000c 2000" +
      "02 b4 da2c" +
      "0001 0006 5232" + // Device ID "R2"
      "0005 0103 43"
  );
  const s = summarize(frame, 395);
  assert.equal(s.proto, "CDP");
  assert.equal(s.addr, "aa:bb:cc:00:02:00 > 01:00:0c:cc:cc:cc");
  assert.equal(s.info, "Device ID R2");
});

test("DTP and VTP over SNAP", () => {
  const dtp = h("01000ccccccc aabbcc000200 001e aaaa0300000c 2004 01000000");
  assert.equal(summarize(dtp, 60).proto, "DTP");
  const vtp = h("01000ccccccc aabbcc000200 001e aaaa0300000c 2003 01010000");
  assert.equal(summarize(vtp, 60).proto, "VTP");
});

test("SNAP OUI 00:00:00 reuses the ethertype path (ARP)", () => {
  // OUI 000000 + pid 0806 → decode as ARP through the shared dispatcher.
  const frame = h(
    "ffffffffffff aabbcc000100 0028" +
      "aaaa03 000000 0806" +
      "0001 0800 06 04 0001 aabbcc000100 0a000001 000000000000 0a000002"
  );
  const s = summarize(frame, 60);
  assert.equal(s.proto, "ARP");
  assert.ok(s.info.includes("Who has 10.0.0.2"), s.info);
});

// ---- LOOP (0x9000) ----

test("LOOP (0x9000, real IOL keepalive) labeled, not hex", () => {
  const frame = h(
    "aabbcc000100 aabbcc000100 9000" + "0000 0001 00000000" + "00".repeat(40)
  );
  const s = summarize(frame, 60);
  assert.equal(s.proto, "LOOP");
  assert.equal(s.addr, "aa:bb:cc:00:01:00 > aa:bb:cc:00:01:00");
});

// ---- robustness ----

test("other LLC → 'LLC' with dsap/ssap info", () => {
  const frame = h("0180c2000000 aabbcc000100 000f f0f0 03 0000000000");
  const s = summarize(frame, 60);
  assert.equal(s.proto, "LLC");
  assert.ok(s.info.includes("dsap 0xf0") && s.info.includes("ssap 0xf0"), s.info);
});

test("truncated LLC runt does not crash", () => {
  // 14 bytes exactly: dst, src, length — no LLC header at all.
  const frame = h("0180c2000000 aabbcc000100 0003");
  const s = summarize(frame, 60);
  assert.equal(s.proto, "LLC");
  assert.equal(s.info, "runt");
});

test("truncated CDP (Device ID TLV cut off) does not crash", () => {
  const frame = h("01000ccccccc aabbcc000200 0010 aaaa0300000c 2000 02 b4 da2c 0001 0006 52");
  const s = summarize(frame, 60); // valLen guarded to available bytes
  assert.equal(s.proto, "CDP");
  assert.ok(s.info === "Device ID R" || s.info === "", `no crash, got: ${s.info}`);
});
