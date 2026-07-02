<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { Terminal } from "@xterm/xterm";
  import { FitAddon } from "@xterm/addon-fit";
  import "@xterm/xterm/css/xterm.css";
  import { labStore } from "../labStore.svelte";

  let { nodeId, active }: { nodeId: number; active: boolean } = $props();

  let container: HTMLDivElement | undefined = $state();
  let term: Terminal | undefined;
  let fit: FitAddon | undefined;
  let resizeObserver: ResizeObserver | undefined;
  let promptLine = "";

  const PROMPT = "Router>";

  onMount(() => {
    term = new Terminal({
      convertEol: true,
      fontFamily: "var(--font-mono)",
      fontSize: 13,
      cursorBlink: true,
      theme: {
        background: "#08090c",
        foreground: "#d6dae3",
        cursor: "#4f8cff",
        selectionBackground: "rgba(79,140,255,0.35)",
      },
    });
    fit = new FitAddon();
    term.loadAddon(fit);
    if (container) {
      term.open(container);
      fit.fit();
    }

    // Replay any buffered mock console history.
    for (const line of labStore.mockTransport.getConsoleHistory(nodeId)) {
      term.write(line);
    }

    term.onData((data) => {
      handleInput(data);
    });

    resizeObserver = new ResizeObserver(() => fit?.fit());
    if (container) resizeObserver.observe(container);
  });

  onDestroy(() => {
    resizeObserver?.disconnect();
    term?.dispose();
  });

  $effect(() => {
    if (active) {
      queueMicrotask(() => fit?.fit());
      term?.focus();
    }
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

<div class="term-container" bind:this={container}></div>

<style>
  .term-container {
    width: 100%;
    height: 100%;
    padding: 4px 6px;
  }
  :global(.xterm) {
    height: 100%;
  }
</style>
