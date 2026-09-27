// Run: node --test tests/consoleMetrics.test.ts
import { test } from "node:test";
import assert from "node:assert/strict";
import { ConsoleMetrics, attachConsoleMetrics } from "../src/lib/consoleMetrics.ts";

test("coalesced receives and split writes keep honest timing windows", () => {
  let now = 10;
  const metrics = new ConsoleMetrics(() => now);
  metrics.receive(2);
  now = 15;
  metrics.receive(5);
  now = 20;
  const first = metrics.emit(30, true);
  now = 21;
  const second = metrics.emit(10, false);
  assert.equal(metrics.snapshot().backlogBytes, 40);
  now = 25;
  first();
  first(); // xterm callback cannot double-count completion.
  now = 26;
  second();
  now = 30;
  metrics.render();
  const snapshot = metrics.snapshot();
  assert.equal(snapshot.wsBytes, 7);
  assert.equal(snapshot.wsChunks, 2);
  assert.equal(snapshot.writeBytes, 40);
  assert.equal(snapshot.backlogBytes, 0);
  assert.equal(snapshot.parsedWrites, 2);
  assert.equal(snapshot.samples[0].receivedAt, 10);
  assert.equal(snapshot.samples[0].receiveChunks, 2);
  assert.equal(snapshot.samples[1].receiveChunks, 0);
  assert.equal(snapshot.samples[1].correlation, "split emission from preceding receive window");
  assert.equal(snapshot.samples[1].visible, false);
  assert.equal(snapshot.latency.emitToParseCallback.meanMs, 5);
  assert.equal(snapshot.latency.receiveWindowToNextRenderEvent.meanMs, 20);
});

test("hidden render backlog and retained write samples stay bounded", () => {
  const metrics = new ConsoleMetrics(() => 1);
  for (let i = 0; i < 1000; i++) {
    metrics.receive(1);
    metrics.emit(1, false)();
  }
  const snapshot = metrics.snapshot();
  assert.equal(snapshot.writeCalls, 1000);
  assert.equal(snapshot.hiddenWriteCalls, 1000);
  assert.equal(snapshot.samples.length, 256);
  assert.equal(snapshot.awaitingRenderSamples, 256);
  assert.equal(snapshot.droppedRenderSamples, 744);
  assert.equal(snapshot.latency.parseCallbackToNextRenderEvent.count, 0);
});

test("snapshots are detached and reset/disposal invalidate old write callbacks", () => {
  const metrics = new ConsoleMetrics(() => 1);
  metrics.receive(5);
  const pending = metrics.emit(10, true);
  const snapshot = metrics.snapshot();
  snapshot.samples[0].bytes = 999;
  assert.equal(metrics.snapshot().samples[0].bytes, 10);
  metrics.reset();
  pending();
  assert.equal(metrics.snapshot().backlogBytes, 0);
  assert.equal(metrics.snapshot().parsedWrites, 0);
  const afterReset = metrics.emit(5, true);
  metrics.dispose();
  afterReset();
  assert.equal(metrics.snapshot().parsedWrites, 0);
});

test("browser diagnostics remain absent unless explicitly enabled", () => {
  const original = Object.getOwnPropertyDescriptor(globalThis, "window");
  const browser = { location: { search: "" } };
  Object.defineProperty(globalThis, "window", { configurable: true, value: browser });
  try {
    assert.equal(attachConsoleMetrics(1), undefined);
    assert.equal(Object.hasOwn(browser, "__iolboxConsoleMetrics"), false);
    browser.location.search = "?consoleMetrics=1";
    const handle = attachConsoleMetrics(1)!;
    const api = (browser as any).__iolboxConsoleMetrics;
    assert.equal(Object.isFrozen(api), true);
    handle.metrics.receive(20);
    assert.equal(api.snapshot().nodes[1].wsBytes, 20);
    api.reset(1);
    assert.equal(api.snapshot().nodes[1].wsBytes, 0);
    handle.dispose();
    assert.deepEqual(api.snapshot().nodes, {});
  } finally {
    if (original) Object.defineProperty(globalThis, "window", original);
    else Reflect.deleteProperty(globalThis, "window");
  }
});
