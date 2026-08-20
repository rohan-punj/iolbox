<script lang="ts">
  import { labStore } from "../labStore.svelte";
  import {
    consoleUiStore,
    type ConsoleMark,
    type PaneRef,
  } from "../consoleUiStore.svelte";
  import { captureTitle, nodeName } from "../paneLabels";
  import ConsoleTerm from "./ConsoleTerm.svelte";
  import CaptureTerm from "./CaptureTerm.svelte";
  import LensPane from "./LensPane.svelte";

  let {
    ref,
    visible,
    focused,
    tiled = false,
  }: { ref: PaneRef; visible: boolean; focused: boolean; tiled?: boolean } = $props();

  // In tabs mode, the dock's tab strip is the only visible pane and already
  // names it. In tiled/split mode multiple panes are on screen at once, but
  // only the *focused* one is highlighted in that shared strip — every other
  // tiled pane has no visible identity at all (no header, nothing in the
  // pane itself), so users can't tell which console/capture/lens a given
  // tile is without reading its scrollback content. Show a small label.
  function paneTitle(): string {
    if (ref.kind === "console") return nodeName(ref.node);
    if (ref.kind === "capture") return captureTitle(ref.link);
    return `Lens · ${captureTitle(ref.link)}`;
  }

  function isToolNode(id: number): boolean {
    return labStore.lab.nodes.find((node) => node.id === id)?.kind === "tool";
  }

  function captureAddr(linkId: number): { host: string; port: number } | null {
    const port = labStore.capturePorts[linkId];
    if (!port) return null;
    return { host: location.hostname || "localhost", port };
  }

  function wiresharkCmd(linkId: number): string | null {
    const addr = captureAddr(linkId);
    return addr ? `wireshark -k -i TCP@${addr.host}:${addr.port}` : null;
  }

  function wiresharkCmdFull(linkId: number): string | null {
    const addr = captureAddr(linkId);
    if (!addr) return null;
    const platform = typeof navigator === "undefined" ? "" : `${navigator.platform} ${navigator.userAgent}`;
    if (/Mac/i.test(platform)) {
      return `/Applications/Wireshark.app/Contents/MacOS/Wireshark -k -i TCP@${addr.host}:${addr.port}`;
    }
    return `"C:\\Program Files\\Wireshark\\Wireshark.exe" -k -i TCP@${addr.host}:${addr.port}`;
  }

  function fmtBytes(bytes: number): string {
    if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MiB`;
    return `${Math.max(1, Math.round(bytes / 1024))} KiB`;
  }

  function consoleAddr(nodeId: number): { host: string; port: number } | null {
    const port = labStore.consolePorts[nodeId];
    if (!port) return null;
    return { host: location.hostname || "localhost", port };
  }

  async function copyText(text: string, promptLabel: string) {
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text);
        labStore.pushLog("info", `copied ${text}`);
        return;
      }
    } catch {
      /* fall through to prompt */
    }
    window.prompt(promptLabel, text);
  }

  const SHARK =
    '<svg viewBox="0 0 16 16" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><path d="M2 12.5c3.5 0 4-7 8.5-8.5-.5 2 0 3.5 1 4.5s2 1.5 2.5 4z"/><path d="M1.5 14.5h13"/></svg>';

  function openSearch(nodeId: number) {
    if (ref.kind === "console" && ref.node === nodeId) consoleUiStore.setSearchOpenFor(nodeId);
  }

  function closeSearch() {
    consoleUiStore.setSearchOpenFor(null);
  }
</script>

<div class="pane-frame">
  {#if tiled}
    <div class="pane-title" title={paneTitle()}>{paneTitle()}</div>
  {/if}
  {#if ref.kind === "console"}
    {#if isToolNode(ref.node)}
      <iframe
        class="tool-frame"
        src={`/tool/${ref.node}/`}
        sandbox="allow-scripts allow-forms allow-same-origin"
        title={`${nodeName(ref.node)} proxied GUI`}
      ></iframe>
    {:else}
      <ConsoleTerm
        nodeId={ref.node}
        {visible}
        {focused}
        searchOpen={consoleUiStore.searchOpenFor === ref.node}
        onOpenSearch={() => openSearch(ref.node)}
        onCloseSearch={closeSearch}
        marks={consoleUiStore.marks}
      />
    {/if}
  {:else if ref.kind === "capture"}
    {@const linkId = ref.link}
    <CaptureTerm {linkId} {visible} {focused} marks={consoleUiStore.marks} />
    {#if consoleUiStore.nativeCapture[linkId]}
      {@const recorded = labStore.captureRecorded[linkId] ?? 0}
      <div class="native-hold overlay">
        <div class="native-card">
          <div class="native-title">Open in Wireshark</div>
          <div class="native-sub">
            Save the capture as a file and open it in Wireshark — no setup needed:
          </div>
          <button
            class="native-primary"
            disabled={recorded === 0}
            title={recorded === 0 ? "Waiting for packets on this link" : "Download .pcapng"}
            onclick={() => labStore.downloadCapture(linkId)}
          >
            {@html SHARK}
            Save .pcapng{recorded > 0 ? ` · ${fmtBytes(recorded)}` : ""}
          </button>
          {#if recorded === 0}
            <div class="native-hint">Waiting for packets — generate traffic on the link, then save.</div>
          {/if}
          <div class="native-div">or attach a live session</div>
          {#if wiresharkCmd(linkId)}
            <div class="native-sub">Run in a terminal where Wireshark is on your PATH:</div>
            <button
              class="addr-chip mono"
              title="Click to copy"
              onclick={() => copyText(wiresharkCmd(linkId)!, "Copy Wireshark command:")}
            >
              {wiresharkCmd(linkId)}
            </button>
            <div class="native-hint">Not on PATH? Use the full path (click to copy):</div>
            <button
              class="addr-chip mono"
              title="Click to copy"
              onclick={() => copyText(wiresharkCmdFull(linkId)!, "Copy Wireshark command:")}
            >
              {wiresharkCmdFull(linkId)}
            </button>
          {:else}
            <div class="native-sub">
              Live attach unlocks once the capture port is assigned (start the lab or the capture first).
            </div>
          {/if}
          <button class="native-back" onclick={() => consoleUiStore.toggleNativeCapture(linkId)}>
            Back to live summary
          </button>
        </div>
      </div>
    {/if}
  {:else}
    <LensPane linkId={ref.link} visible={visible} {focused} title={captureTitle(ref.link)} />
  {/if}
</div>

<style>
  .pane-frame {
    position: relative;
    width: 100%;
    height: 100%;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
    background: var(--term-bg);
  }
  /* Overlay, not layout — matches .find-bar/.native-hold below: identifies a
     tiled pane without reflowing (and re-fitting) the terminal beneath it. */
  .pane-title {
    position: absolute;
    top: 4px;
    left: 6px;
    z-index: 2;
    max-width: calc(100% - 12px);
    padding: 1px 7px;
    font: 600 10px/1.6 var(--font-ui);
    letter-spacing: 0.02em;
    color: var(--text-secondary);
    background: color-mix(in oklab, var(--bg-1) 78%, transparent);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-full);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    pointer-events: none;
  }
  .tool-frame {
    display: block;
    width: 100%;
    height: 100%;
    border: 0;
    background: white;
  }
  .addr-chip {
    all: unset;
    font-size: 11px;
    color: var(--text-secondary);
    cursor: pointer;
    padding: 2px 6px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-subtle);
    font-family: var(--font-mono);
    max-width: 100%;
    white-space: normal;
    overflow-wrap: anywhere;
    text-align: center;
  }
  .addr-chip:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
    border-color: var(--border);
  }
  .native-hold {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--term-bg);
  }
  .native-hold.overlay {
    background: color-mix(in oklab, var(--term-bg) 82%, transparent);
    backdrop-filter: blur(2px);
  }
  .native-card {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-3);
    padding: var(--sp-5) var(--sp-6);
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    max-width: min(90%, 560px);
  }
  .native-title {
    font-size: var(--fs-sm);
    font-weight: 650;
    color: var(--text-primary);
  }
  .native-sub {
    font-size: var(--fs-xs);
    color: var(--text-secondary);
    text-align: center;
  }
  .native-back,
  .native-primary {
    all: unset;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-size: var(--fs-sm);
    font-weight: 650;
    cursor: pointer;
    padding: 8px 16px;
    border-radius: var(--radius-sm);
  }
  .native-back {
    font-size: var(--fs-xs);
    font-weight: 600;
    color: var(--accent);
    padding: 4px 10px;
    border: 1px solid var(--accent-muted);
  }
  .native-back:hover {
    background: var(--bg-hover);
  }
  .native-primary {
    color: var(--accent-ink, #04120c);
    background: var(--accent);
  }
  .native-primary:hover {
    filter: brightness(1.08);
  }
  .native-primary[disabled] {
    opacity: 0.5;
    cursor: not-allowed;
    filter: none;
  }
  .native-primary :global(svg) {
    color: currentColor;
  }
  .native-hint {
    font-size: 11px;
    color: var(--text-tertiary);
    text-align: center;
    max-width: 100%;
    overflow-wrap: anywhere;
  }
  .native-div {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-tertiary);
    margin-top: 4px;
  }
</style>
