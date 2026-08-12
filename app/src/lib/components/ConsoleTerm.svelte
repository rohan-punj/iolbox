<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { Terminal } from "@xterm/xterm";
  import { FitAddon } from "@xterm/addon-fit";
  import { SearchAddon } from "@xterm/addon-search";
  import "@xterm/xterm/css/xterm.css";
  import { labStore } from "../labStore.svelte";
  import { themeStore } from "../themeStore.svelte";
  import { consoleUiStore } from "../consoleUiStore.svelte";
  import { ConsoleTransport } from "../consoleTransport";
  import { ConsoleColorizer } from "../consoleColorizer";
  import type { ConsoleMark } from "../consoleUiStore.svelte";

  let {
    nodeId,
    visible,
    focused,
    searchOpen = false,
    onOpenSearch,
    onCloseSearch,
    marks = [],
  }: {
    nodeId: number;
    visible: boolean;
    focused: boolean;
    searchOpen?: boolean;
    onOpenSearch?: () => void;
    onCloseSearch?: () => void;
    marks?: ConsoleMark[];
  } = $props();

  let container: HTMLDivElement | undefined = $state();
  let term: Terminal | undefined;
  let fit: FitAddon | undefined;
  let searchAddon: SearchAddon | undefined;
  let resizeObserver: ResizeObserver | undefined;
  let promptLine = "";
  let realConsole: ConsoleTransport | undefined;
  let searchInput = $state<HTMLInputElement | undefined>();
  let searchQuery = $state("");
  let searchMatches = $state(0);
  let activeMatch = $state(0);
  let lastMarkId = 0;

  const PROMPT = "Router>";
  const DIM = "\x1b[2m";
  const RESET = "\x1b[0m";

  // Resolve theme tokens to concrete colours xterm can consume (it needs hex/
  // rgb, not CSS vars). Re-read when the theme flips.
  function readVar(name: string, fallback: string): string {
    const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
    return v || fallback;
  }
  function termTheme() {
    return {
      background: readVar("--term-bg", "#08090c"),
      foreground: readVar("--term-ink", "#d6dae3"),
      cursor: readVar("--accent", "#4bc6d1"),
      selectionBackground: readVar("--accent-muted", "rgba(75,198,209,0.3)"),
    };
  }

  function refitAndResize(): boolean {
    if (!fit || !container || container.clientWidth === 0 || container.clientHeight === 0) return false;
    fit.fit();
    if (term) realConsole?.sendResize(term.cols, term.rows);
    return true;
  }

  function searchOptions() {
    const accent = readVar("--accent", "#4bc6d1");
    return {
      caseSensitive: false,
      incremental: true,
      decorations: {
        matchBackground: "#26363b",
        matchBorder: accent,
        matchOverviewRuler: accent,
        activeMatchBackground: accent,
        activeMatchBorder: "#eaf0f7",
        activeMatchColorOverviewRuler: accent,
      },
    };
  }

  function runSearch(next = true) {
    if (!searchAddon || !searchQuery) {
      searchAddon?.clearDecorations();
      searchMatches = 0;
      activeMatch = 0;
      return;
    }
    const found = next
      ? searchAddon.findNext(searchQuery, searchOptions())
      : searchAddon.findPrevious(searchQuery, searchOptions());
    if (!found && searchMatches === 0) activeMatch = 0;
  }

  function closeSearch() {
    searchAddon?.clearDecorations();
    searchMatches = 0;
    activeMatch = 0;
    onCloseSearch?.();
  }

  function writeMark(mark: ConsoleMark) {
    term?.write(`\r\n${DIM}──── ${mark.label} ────${RESET}\r\n`);
  }

  onMount(() => {
    term = new Terminal({
      convertEol: true,
      fontFamily:
        '"Cascadia Code","JetBrains Mono",ui-monospace,"SF Mono",Consolas,monospace',
      fontSize: consoleUiStore.fontSize,
      lineHeight: 1.25,
      cursorBlink: true,
      scrollback: 5000,
      theme: termTheme(),
    });
    fit = new FitAddon();
    term.loadAddon(fit);
    searchAddon = new SearchAddon();
    term.loadAddon(searchAddon);
    searchAddon.onDidChangeResults((result) => {
      searchMatches = result.resultCount;
      activeMatch = result.resultIndex >= 0 ? result.resultIndex + 1 : 0;
    });
    term.attachCustomKeyEventHandler((event) => {
      if (focused && (event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "f") {
        onOpenSearch?.();
        return false;
      }
      if (focused && searchOpen && event.key === "Escape") {
        closeSearch();
        return false;
      }
      return true;
    });
    if (container) {
      term.open(container);
      refitAndResize();
      if (focused) term.focus();
    }

    if (labStore.transportKind === "ws") {
      // Real supervisor: pipe xterm <-> ws(s)://<host>/console/<nodeId>
      // (binary frames; see consoleTransport.ts / wsbridge.go).
      const decoder = new TextDecoder();
      // v2 colorizer emits through a sink (it may hold an incomplete line tail
      // for ~one frame — see consoleColorizer.ts); everything lands in term.
      const colorizer = new ConsoleColorizer((s) => term?.write(s));
      const rc = new ConsoleTransport(nodeId, {
        onData: (bytes) => {
          const text = decoder.decode(bytes, { stream: true });
          if (consoleUiStore.colorize) {
            colorizer.push(text);
          } else {
            // Toggled off mid-stream: release anything still held FIRST so
            // byte order is preserved, then write raw.
            colorizer.flushHeld();
            term?.write(text);
          }
        },
        onOpen: () => {
          colorizer.reset(); // fresh stream (reconnects replay recent context)
          if (term && container && container.clientWidth > 0 && container.clientHeight > 0) {
            rc.sendResize(term.cols, term.rows);
          }
        },
        onError: () => labStore.pushLog("error", `console ws error for node ${nodeId}`, nodeId),
      });
      rc.connect();
      realConsole = rc;
      term.onData((data) => rc.sendInput(data));
    } else {
      // Mock console: replay buffered history and drive a tiny fake shell.
      for (const line of labStore.mockTransport?.getConsoleHistory(nodeId) ?? []) {
        term.write(line);
      }
      term.onData((data) => handleInput(data));
    }

    resizeObserver = new ResizeObserver(() => {
      refitAndResize();
    });
    if (container) resizeObserver.observe(container);
  });

  onDestroy(() => {
    resizeObserver?.disconnect();
    realConsole?.disconnect();
    term?.dispose();
  });

  $effect(() => {
    if (visible) {
      requestAnimationFrame(() => refitAndResize());
    }
    if (focused) {
      term?.focus();
    }
  });

  // Refit + re-send NAWS when the dock side flips (bottom<->right changes the
  // pane's aspect ratio drastically). Depends on dockSide so it re-runs on
  // toggle; the microtask lets the new layout settle before measuring.
  $effect(() => {
    void consoleUiStore.dockSide;
    void consoleUiStore.layout;
    void visible;
    if (!visible) return;
    requestAnimationFrame(() => refitAndResize());
  });

  // Repaint the terminal palette when the app theme flips.
  $effect(() => {
    void themeStore.current;
    if (term) term.options.theme = termTheme();
  });

  // Apply the shared console font size live (A-/A+ control) and refit so the
  // grid reflows to the new cell metrics; re-send NAWS so the node wraps to the
  // new column count. Runs on every fontSize change, for every open terminal.
  $effect(() => {
    const px = consoleUiStore.fontSize;
    if (!term) return;
    term.options.fontSize = px;
    requestAnimationFrame(() => refitAndResize());
  });

  $effect(() => {
    const query = searchQuery;
    if (!searchOpen || !searchAddon) {
      if (!searchOpen) {
        searchAddon?.clearDecorations();
        searchMatches = 0;
        activeMatch = 0;
      }
      return;
    }
    if (!query) {
      searchAddon.clearDecorations();
      searchMatches = 0;
      activeMatch = 0;
      return;
    }
    searchAddon.findNext(query, searchOptions());
  });

  $effect(() => {
    if (searchOpen && focused && searchInput) {
      requestAnimationFrame(() => searchInput?.focus());
    }
  });

  $effect(() => {
    const latest = marks.length > 0 ? marks[marks.length - 1].id : 0;
    if (!term) return;
    if (!visible) {
      lastMarkId = latest;
      return;
    }
    for (const mark of marks) {
      if (mark.id > lastMarkId) writeMark(mark);
    }
    lastMarkId = latest;
  });

  // Collapse the reconnect backoff the moment this node reports running: its
  // console listener is bound before spawn returns, so a tab opened while the
  // lab was still starting (or across a node restart) reattaches immediately
  // instead of waiting out the backoff. The hub replays recent output.
  $effect(() => {
    const state = labStore.nodeStates[nodeId];
    if (state === "running" && realConsole) realConsole.retryNow();
  });

  function handleInput(data: string) {
    if (!term) return;
    const code = data.charCodeAt(0);
    if (data === "\r") {
      term.write("\r\n");
      respondToCommand(promptLine.trim());
      promptLine = "";
      return;
    }
    if (code === 127) {
      // backspace
      if (promptLine.length > 0) {
        promptLine = promptLine.slice(0, -1);
        term.write("\b \b");
      }
      return;
    }
    if (code < 32) return; // ignore other control chars in mock
    promptLine += data;
    term.write(data);
  }

  function respondToCommand(cmd: string) {
    if (!term) return;
    if (cmd.length === 0) {
      term.write(`${PROMPT}`);
      return;
    }
    let output = "";
    if (cmd === "?" || cmd === "help") {
      output = "show, enable, exit — mock console for demo purposes\r\n";
    } else if (cmd.startsWith("show")) {
      const node = labStore.lab.nodes.find((n) => n.id === nodeId);
      output = `${node?.name ?? "node"} — mocked output, no real IOL backend connected\r\n`;
    } else {
      output = `% Unknown command or unsupported (mock console): "${cmd}"\r\n`;
    }
    term.write(output);
    term.write(PROMPT);
  }
</script>

<div class="term-shell">
  <div class="term-container" bind:this={container}></div>
  {#if searchOpen && focused}
    <div class="find-bar" role="search" aria-label="Find in this console">
      <label for={`console-find-${nodeId}`}>Find in this console</label>
      <input
        id={`console-find-${nodeId}`}
        bind:this={searchInput}
        bind:value={searchQuery}
        placeholder="Search scrollback"
        aria-label="Search this console scrollback"
        onkeydown={(event) => {
          if (event.key === "Enter") {
            event.preventDefault();
            runSearch(!event.shiftKey);
          } else if (event.key === "Escape") {
            event.preventDefault();
            closeSearch();
          }
        }}
      />
      <span class="match-count" aria-live="polite">
        {searchQuery ? `${activeMatch}/${searchMatches}` : ""}
      </span>
      <button class="find-nav" title="Previous match" aria-label="Previous match" onclick={() => runSearch(false)}>↑</button>
      <button class="find-nav" title="Next match" aria-label="Next match" onclick={() => runSearch(true)}>↓</button>
      <button class="find-close" title="Close search" aria-label="Close search" onclick={closeSearch}>×</button>
    </div>
  {/if}
</div>

<style>
  .term-shell {
    position: relative;
    width: 100%;
    height: 100%;
    min-width: 0;
    min-height: 0;
  }
  .term-container {
    width: 100%;
    height: 100%;
    padding: 4px 6px;
    background: var(--term-bg);
  }
  .find-bar {
    position: absolute;
    top: 8px;
    right: 10px;
    z-index: 2;
    display: flex;
    align-items: center;
    gap: 5px;
    max-width: calc(100% - 20px);
    padding: 4px 6px;
    color: var(--text-secondary);
    background: color-mix(in oklab, var(--bg-2) 94%, transparent);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow-md);
    backdrop-filter: blur(8px);
  }
  .find-bar label {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }
  .find-bar input {
    width: min(260px, 38vw);
    min-width: 120px;
    padding: 4px 6px;
    color: var(--text-primary);
    background: var(--bg-1);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    font: 12px/1.2 var(--font-ui);
  }
  .find-bar input:focus {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .match-count {
    min-width: 42px;
    color: var(--text-tertiary);
    font: 11px/1 var(--font-mono);
    text-align: right;
  }
  .find-nav,
  .find-close {
    all: unset;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    color: var(--text-tertiary);
    border-radius: var(--radius-sm);
    cursor: pointer;
  }
  .find-nav:hover,
  .find-close:hover {
    color: var(--text-primary);
    background: var(--bg-hover);
  }
  :global(.xterm) {
    height: 100%;
  }
</style>
