// Opt-in diagnostics only. Times describe receive windows and xterm events,
// never a one-to-one mapping between WebSocket frames, writes, and screen pixels.
const SAMPLE_LIMIT = 256;

type WriteSample = {
  emittedAt: number;
  receivedAt: number | null;
  receiveChunks: number;
  correlation: "oldest pending receive window" | "split emission from preceding receive window" | "no receive window";
  bytes: number;
  visible: boolean;
  parsedAt: number | null;
  renderedAt: number | null;
};

function summary(values: number[]) {
  values.sort((a, b) => a - b);
  return {
    count: values.length,
    meanMs: values.length ? values.reduce((a, b) => a + b, 0) / values.length : null,
    p50Ms: values.length ? values[Math.floor((values.length - 1) * 0.5)] : null,
    p95Ms: values.length ? values[Math.floor((values.length - 1) * 0.95)] : null,
    maxMs: values.length ? values[values.length - 1] : null,
  };
}

export class ConsoleMetrics {
  private now: () => number;
  private epoch = 0;
  private alive = true;
  private samples: WriteSample[] = [];
  private awaitingRender: WriteSample[] = [];
  private pendingAt: number | null = null;
  private precedingAt: number | null = null;
  private pendingChunks = 0;
  private counters = this.emptyCounters();

  constructor(now: () => number = () => performance.now()) { this.now = now; }

  private emptyCounters() {
    return { wsBytes: 0, wsChunks: 0, writeCalls: 0, hiddenWriteCalls: 0, writeBytes: 0,
      parsedWrites: 0, renderEvents: 0, observedRenderedWrites: 0,
      backlogBytes: 0, maxBacklogBytes: 0, pendingReceiveBytes: 0,
      lastReceiveAt: null as number | null, droppedRenderSamples: 0 };
  }

  receive(bytes: number) {
    if (!this.alive) return;
    const at = this.now();
    this.pendingAt ??= at;
    this.pendingChunks++;
    this.counters.wsBytes += bytes;
    this.counters.wsChunks++;
    this.counters.pendingReceiveBytes += bytes;
    this.counters.lastReceiveAt = at;
  }

  // Called immediately before term.write. UTF-8 byte length includes inserted
  // ANSI styling and therefore is deliberately distinct from wsBytes.
  emit(bytes: number, visible: boolean): () => void {
    const epoch = this.epoch;
    const sample: WriteSample = {
      emittedAt: this.now(), receivedAt: this.pendingAt ?? this.precedingAt,
      receiveChunks: this.pendingChunks,
      correlation: this.pendingAt !== null ? "oldest pending receive window"
        : this.precedingAt !== null ? "split emission from preceding receive window" : "no receive window",
      bytes, visible, parsedAt: null, renderedAt: null,
    };
    if (this.pendingAt !== null) this.precedingAt = this.pendingAt;
    this.pendingAt = null;
    this.pendingChunks = 0;
    this.counters.pendingReceiveBytes = 0;
    this.counters.writeCalls++;
    if (!visible) this.counters.hiddenWriteCalls++;
    this.counters.writeBytes += bytes;
    this.counters.backlogBytes += bytes;
    this.counters.maxBacklogBytes = Math.max(this.counters.maxBacklogBytes, this.counters.backlogBytes);
    this.samples.push(sample);
    if (this.samples.length > SAMPLE_LIMIT) this.samples.shift();
    let completed = false;
    return () => {
      if (!this.alive || this.epoch !== epoch || completed) return;
      completed = true;
      sample.parsedAt = this.now();
      this.counters.parsedWrites++;
      this.counters.backlogBytes -= bytes;
      this.awaitingRender.push(sample);
      if (this.awaitingRender.length > SAMPLE_LIMIT) {
        this.awaitingRender.shift();
        this.counters.droppedRenderSamples++;
      }
    };
  }

  render() {
    if (!this.alive) return;
    const at = this.now();
    this.counters.renderEvents++;
    for (const sample of this.awaitingRender) sample.renderedAt = at;
    this.counters.observedRenderedWrites += this.awaitingRender.length;
    this.awaitingRender = [];
  }

  reconnect() {
    this.pendingAt = this.precedingAt = null;
    this.pendingChunks = this.counters.pendingReceiveBytes = 0;
  }

  reset() {
    this.epoch++;
    this.samples = [];
    this.awaitingRender = [];
    this.counters = this.emptyCounters();
    this.reconnect();
  }

  dispose() { this.alive = false; this.reset(); }

  snapshot() {
    const samples = this.samples.map(sample => ({ ...sample }));
    return {
      ...this.counters, sampleLimit: SAMPLE_LIMIT, samples,
      awaitingRenderSamples: this.awaitingRender.length,
      latency: {
        receiveWindowToEmit: summary(samples.flatMap(s => s.receivedAt === null ? [] : [s.emittedAt - s.receivedAt])),
        emitToParseCallback: summary(samples.flatMap(s => s.parsedAt === null ? [] : [s.parsedAt - s.emittedAt])),
        parseCallbackToNextRenderEvent: summary(samples.flatMap(s => s.parsedAt === null || s.renderedAt === null ? [] : [s.renderedAt - s.parsedAt])),
        receiveWindowToNextRenderEvent: summary(samples.flatMap(s => s.receivedAt === null || s.renderedAt === null ? [] : [s.renderedAt - s.receivedAt])),
      },
    };
  }
}

const nodes = new Map<number, ConsoleMetrics>();
const semantics = {
  receive: "ConsoleTransport binary data handler entry; excludes network and server time",
  correlation: "Oldest receive since previous emission; split emissions reuse that window. Frames and writes are not one-to-one.",
  parse: "xterm write callback: parsing complete, not screen paint",
  render: "First xterm onRender event after parse callback; may coalesce writes or reflect other terminal changes, not physical display time",
  backlog: "UTF-8 bytes (including styling) submitted to xterm with callbacks still pending",
  retention: "Latest 256 writes per mounted console; latency summaries cover retained samples only",
};
type MetricsAPI = { snapshot(): { semantics: typeof semantics; nodes: Record<string, ReturnType<ConsoleMetrics["snapshot"]>> }; reset(nodeId?: number): void };
declare global { interface Window { readonly __iolboxConsoleMetrics?: MetricsAPI } }

export function attachConsoleMetrics(nodeId: number): { metrics: ConsoleMetrics; dispose(): void } | undefined {
  if (typeof window === "undefined" || new URLSearchParams(window.location.search).get("consoleMetrics") !== "1") return;
  if (!window.__iolboxConsoleMetrics) {
    Object.defineProperty(window, "__iolboxConsoleMetrics", { configurable: true, value: Object.freeze({
      snapshot: () => ({ semantics: { ...semantics }, nodes: Object.fromEntries([...nodes].map(([id, metrics]) => [id, metrics.snapshot()])) }),
      reset: (id?: number) => { for (const [node, metrics] of nodes) if (id === undefined || id === node) metrics.reset(); },
    }) });
  }
  const metrics = new ConsoleMetrics();
  nodes.set(nodeId, metrics);
  return { metrics, dispose: () => { metrics.dispose(); if (nodes.get(nodeId) === metrics) nodes.delete(nodeId); } };
}
