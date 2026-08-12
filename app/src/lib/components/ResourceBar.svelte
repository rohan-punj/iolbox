<script lang="ts">
  import { chromeStore } from "../chromeStore.svelte";
  import { labStore } from "../labStore.svelte";

  type MeterState = "running" | "starting" | "crashed" | "stopped";

  const host = $derived(labStore.hostStats);
  const cpuPct = $derived(host ? Math.min(100, Math.round(host.cpuPct)) : null);
  const ramPct = $derived(
    host && host.memTotal > 0 ? Math.min(100, Math.round((host.memUsed / host.memTotal) * 100)) : null
  );

  function meterState(pct: number | null): MeterState {
    if (pct === null) return "stopped";
    return pct > 90 ? "crashed" : pct > 75 ? "starting" : "running";
  }

  function formatGB(bytes: number): string {
    return (bytes / 1024 / 1024 / 1024).toFixed(1);
  }

  const cpuValue = $derived(cpuPct === null ? "Waiting for host stats…" : `${cpuPct}%`);
  const ramValue = $derived(
    host === null ? "Waiting for host stats…" : `${formatGB(host.memUsed)}/${formatGB(host.memTotal)}G`
  );
  const cpuState = $derived(meterState(cpuPct));
  const ramState = $derived(meterState(ramPct));

  function providerName(provider: string | null): string {
    if (!provider) return "provider";
    return ({ vmware: "VMware", wsl2: "WSL2", remote: "Remote", qemu: "QEMU" } as Record<string, string>)[provider] ?? provider;
  }

  const connectionValue = $derived.by(() => {
    const provider = providerName(labStore.activeProvider);
    switch (labStore.providerStatus) {
      case "connected":
        return `Connected · ${provider}`;
      case "connecting":
        return `Connecting · ${provider}`;
      case "error":
        return "Error";
      default:
        return "Waiting";
    }
  });
  const connectionState = $derived<MeterState>(
    labStore.providerStatus === "connected"
      ? "running"
      : labStore.providerStatus === "connecting"
        ? "starting"
        : labStore.providerStatus === "error"
          ? "crashed"
          : "stopped"
  );

  let elapsedStartedAt = $state<number | null>(null);
  let previousLabRunning = false;

  // The existing labStore clock is the only ticker. This effect only records
  // observed start/stop edges and rereads nowTick once per second for display.
  $effect(() => {
    const running = labStore.labRunning;
    const now = labStore.nowTick;
    if (running && !previousLabRunning) elapsedStartedAt = now;
    else if (!running && previousLabRunning) elapsedStartedAt = null;
    previousLabRunning = running;
  });

  function formatElapsed(ms: number): string {
    const totalSeconds = Math.floor(ms / 1000);
    const hours = Math.floor(totalSeconds / 3600);
    const minutes = Math.floor((totalSeconds % 3600) / 60);
    const seconds = totalSeconds % 60;
    return [hours, minutes, seconds].map((part) => String(part).padStart(2, "0")).join(":");
  }

  const elapsedValue = $derived(
    formatElapsed(elapsedStartedAt === null ? 0 : Math.max(0, labStore.nowTick - elapsedStartedAt))
  );
</script>

<footer class="resource-bar" class:chrome-hidden={chromeStore.hidden} data-chrome-surface aria-label="Resource status">
  <div class="resource-cell">
    <span class="resource-label">CPU</span>
    <span class="resource-value">{cpuValue}</span>
    <span class={`resource-led ${cpuState}`} aria-hidden="true"></span>
  </div>

  <div class="resource-cell">
    <span class="resource-label">RAM</span>
    <span class="resource-value">{ramValue}</span>
    <span class={`resource-led ${ramState}`} aria-hidden="true"></span>
  </div>

  <div class="resource-cell">
    <span class="resource-label">Connection</span>
    <span class="resource-value">{connectionValue}</span>
    <span class={`resource-led ${connectionState}`} aria-hidden="true"></span>
  </div>

  <div class="resource-cell" title="Elapsed time since this page observed the lab start">
    <span class="resource-label">Elapsed</span>
    <span class="resource-value">{elapsedValue}</span>
    <span class={`resource-led ${labStore.labRunning ? "running" : "stopped"}`} aria-hidden="true"></span>
  </div>
</footer>

<style>
  .resource-bar {
    box-sizing: border-box;
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    width: 100%;
    height: var(--statusbar-h);
    min-height: var(--statusbar-h);
    flex: 0 0 var(--statusbar-h);
    padding: 0 var(--sp-3);
    overflow: hidden;
    background: var(--panel);
    border-top: 1px solid var(--border);
    -webkit-backdrop-filter: var(--blur);
    backdrop-filter: var(--blur);
    transition: transform var(--transition-fast), opacity var(--transition-fast);
  }
  .resource-bar.chrome-hidden {
    transform: translateY(100%);
    opacity: 0;
    pointer-events: none;
  }

  .resource-cell {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    min-width: 0;
    flex: 0 0 auto;
    height: 100%;
    white-space: nowrap;
  }

  .resource-label {
    flex: 0 0 auto;
    color: var(--ink-2);
    font-family: var(--font-ui);
    font-size: var(--fs-xs);
    font-weight: 650;
    letter-spacing: var(--ls-eyebrow);
    line-height: 1;
    text-transform: uppercase;
  }

  .resource-value {
    min-width: 0;
    overflow: hidden;
    color: var(--ink);
    font-family: var(--font-mono);
    font-size: var(--fs-sm);
    line-height: 1;
    text-overflow: ellipsis;
  }

  .resource-led {
    width: 7px;
    height: 7px;
    flex: 0 0 7px;
    border-radius: 50%;
  }

  .resource-led.running {
    background: var(--state-running);
    box-shadow: 0 0 0 3px color-mix(in oklab, var(--state-running) 22%, transparent);
  }

  .resource-led.starting {
    background: var(--state-starting);
    box-shadow: 0 0 0 3px color-mix(in oklab, var(--state-starting) 22%, transparent);
  }

  .resource-led.crashed {
    background: var(--state-crashed);
    box-shadow: 0 0 0 3px color-mix(in oklab, var(--state-crashed) 22%, transparent);
  }

  .resource-led.stopped {
    background: var(--state-stopped);
  }
  @media (prefers-reduced-motion: reduce) {
    .resource-bar {
      transition: none;
    }
  }
</style>
