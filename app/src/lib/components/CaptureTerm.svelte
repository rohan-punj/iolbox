<script lang="ts">
  // Live packet-summary view for one capturing link (feature 1). Subscribes to
  // labStore's single parsed /capture/<linkId> session and prints a read-only
  // tshark-ish one-line summary per packet into an xterm instance.
  import { onMount, onDestroy } from "svelte";
  import { Terminal } from "@xterm/xterm";
  import { FitAddon } from "@xterm/addon-fit";
  import "@xterm/xterm/css/xterm.css";
  import { labStore } from "../labStore.svelte";
  import { themeStore } from "../themeStore.svelte";
  import { summarize, type ParsedPacket } from "../pcapng";
  import type { ConsoleMark } from "../consoleUiStore.svelte";

  let {
    linkId,
    visible,
    focused: _focused,
    marks = [],
  }: { linkId: number; visible: boolean; focused: boolean; marks?: ConsoleMark[] } = $props();

  let container: HTMLDivElement | undefined = $state();
  let term: Terminal | undefined;
  let fit: FitAddon | undefined;
  let resizeObserver: ResizeObserver | undefined;
  let unsubscribeCapture: (() => void) | undefined;
  let sawData = false;
  let hintShown = false;
  let lastMarkId = 0;

  // SGR palette for the protocol column (mirrors consoleColorizer's approach).
  const ESC = "\x1b[";
  const RESET = "\x1b[0m";
  const DIM = `${ESC}2m`;
  const PROTO_COLOR: Record<string, string> = {
    TCP: `${ESC}38;5;75m`, // blue
    UDP: `${ESC}38;5;114m`, // green
    ICMP: `${ESC}38;5;213m`, // magenta
    ICMPv6: `${ESC}38;5;213m`,
    ARP: `${ESC}38;5;179m`, // amber
    STP: `${ESC}38;5;179m`, // amber (like ARP — L2 control)
    CDP: `${ESC}38;5;73m`, // teal (discovery/negotiation family)
    LLDP: `${ESC}38;5;73m`,
    DTP: `${ESC}38;5;73m`,
    VTP: `${ESC}38;5;73m`,
    LOOP: `${ESC}38;5;240m`, // dim grey (keepalive noise)
    LLC: `${ESC}38;5;244m`, // grey
    SNAP: `${ESC}38;5;244m`,
    IPv4: `${ESC}38;5;250m`,
    IPv6: `${ESC}38;5;250m`,
    OSPF: `${ESC}38;5;80m`,
    EIGRP: `${ESC}38;5;80m`,
  };

  function colorProto(proto: string): string {
    const c = PROTO_COLOR[proto] ?? `${ESC}38;5;244m`;
    return `${c}${proto.padEnd(7)}${RESET}`;
  }

  function readVar(name: string, fallback: string): string {
    const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
    return v || fallback;
  }
  function termTheme() {
    return {
      background: readVar("--term-bg", "#08090c"),
      foreground: readVar("--term-ink", "#d6dae3"),
      cursor: readVar("--term-bg", "#08090c"), // read-only: hide the cursor
    };
  }

  /** Is this link marked for capture in the doc AND is the lab running? */
  function linkBridged(): boolean {
    const link = labStore.lab.links.find((l) => l.id === linkId);
    return (link?.capture?.enabled ?? false) && labStore.labRunning;
  }

  /** One-shot idle hint. Honest: printed only while genuinely idle (no packets
   *  yet) and only once — the transport keeps retrying in the background, so
   *  the tab is never a tombstone; packets simply start appearing when the
   *  capture comes up. Every link is on the static-tap Linux-bridge fabric, so
   *  capture attaches live with no node restart — nothing else to explain. */
  function writeHint() {
    if (hintShown || sawData) return;
    hintShown = true;
    term?.write(
      `${DIM}Waiting for packets on this link…${RESET}\r\n\r\n`
    );
  }

  function writeHeader() {
    term?.write(`${DIM}   #   time      source > destination                proto   len  info${RESET}\r\n`);
  }

  function refit(): boolean {
    if (!fit || !container || container.clientWidth === 0 || container.clientHeight === 0) return false;
    fit.fit();
    return true;
  }

  function writeMark(mark: ConsoleMark) {
    term?.write(`\r\n${DIM}──── ${mark.label} ────${RESET}\r\n`);
  }

  onMount(() => {
    term = new Terminal({
      convertEol: true,
      disableStdin: true,
      fontFamily: '"Cascadia Code","JetBrains Mono",ui-monospace,"SF Mono",Consolas,monospace',
      fontSize: 12,
      lineHeight: 1.25,
      cursorBlink: false,
      theme: termTheme(),
    });
    fit = new FitAddon();
    term.loadAddon(fit);
    if (container) {
      term.open(container);
      refit();
    }
    writeHeader();

    if (labStore.transportKind === "ws") {
      // labStore owns the single transport/parser for this link. The capture
      // summary and Lens are subscribers to the same parsed packet batch.
      unsubscribeCapture = labStore.subscribeCapture(linkId, {
        onReset: () => writeHeader(),
        onPackets: (packets) => {
          sawData = true;
          for (const pkt of packets) writePacket(pkt);
        },
        onUnavailable: () => writeHint(),
      });
    } else {
      // Mock transport has no real byte stream — show the hint so the tab is
      // still meaningful in dev, plus a couple of synthetic lines.
      writeHint();
      mockDemoLines();
    }

    // If no data has arrived shortly after opening on a non-bridged link, hint.
    if (!linkBridged()) setTimeout(() => writeHint(), 400);

    resizeObserver = new ResizeObserver(() => refit());
    if (container) resizeObserver.observe(container);
  });

  onDestroy(() => {
    resizeObserver?.disconnect();
    unsubscribeCapture?.();
    term?.dispose();
  });

  $effect(() => {
    if (visible) requestAnimationFrame(() => refit());
  });

  $effect(() => {
    const latest = marks.length > 0 ? marks[marks.length - 1].id : 0;
    if (!term) return;
    for (const mark of marks) {
      if (mark.id > lastMarkId && mark.capturePos[linkId] !== undefined) writeMark(mark);
    }
    lastMarkId = latest;
  });

  $effect(() => {
    void themeStore.current;
    if (term) term.options.theme = termTheme();
  });

  // Collapse the reconnect backoff the moment the app KNOWS this capture is
  // live: a capture.started event recorded the port (capturePorts), or the lab
  // just started (auto-arm re-announces every capturing link on start).
  $effect(() => {
    const port = labStore.capturePorts[linkId];
    const running = labStore.labRunning;
    if (port || running) labStore.retryCapture(linkId);
  });

  function writePacket(pkt: ParsedPacket) {
    const s = summarize(pkt.data, pkt.origLen);
    const idx = String(pkt.index).padStart(4);
    const t = `+${pkt.tRel.toFixed(4)}`.padEnd(9);
    const addr = s.addr.length > 40 ? s.addr.slice(0, 39) + "…" : s.addr.padEnd(40);
    const len = String(s.len).padStart(4);
    const info = s.info ? ` ${DIM}${s.info}${RESET}` : "";
    term?.write(`${idx}  ${DIM}${t}${RESET} ${addr} ${colorProto(s.proto)} ${len}${info}\r\n`);
  }

  // Dev-only synthetic packets so the layout is visible under the mock transport.
  function mockDemoLines() {
    const demo = [
      { proto: "ARP", addr: "10.0.0.1 > 10.0.0.2", info: "Who has 10.0.0.2? Tell 10.0.0.1", len: 42 },
      { proto: "ICMP", addr: "10.0.0.1 > 10.0.0.2", info: "Echo request", len: 98 },
      { proto: "TCP", addr: "10.0.0.1:179 > 10.0.0.2:52344", info: "[SYN]", len: 74 },
    ];
    let i = 1;
    let tr = 0;
    for (const d of demo) {
      const idx = String(i++).padStart(4);
      tr += 0.0123;
      const t = `+${tr.toFixed(4)}`.padEnd(9);
      const addr = d.addr.padEnd(40);
      const len = String(d.len).padStart(4);
      term?.write(`${idx}  ${DIM}${t}${RESET} ${addr} ${colorProto(d.proto)} ${len} ${DIM}${d.info}${RESET}\r\n`);
    }
  }
</script>

<div class="cap-container" bind:this={container}></div>

<style>
  .cap-container {
    width: 100%;
    height: 100%;
    padding: 4px 6px;
    background: var(--term-bg);
  }
  :global(.xterm) {
    height: 100%;
  }
</style>
