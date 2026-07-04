<script lang="ts">
  // First-run provider detection UI (docs/providers.md). Drives a Tauri
  // command `detect_providers`, stubbed here with mock data since the Rust
  // side has no real detection logic yet.
  import { labStore, type ProviderId } from "../labStore.svelte";

  let { onDismiss }: { onDismiss: () => void } = $props();

  interface ProviderDetection {
    id: ProviderId;
    name: string;
    available: boolean;
    recommended: boolean;
    detail: string;
    warning?: string;
  }

  let detecting = $state(true);
  let detections = $state<ProviderDetection[]>([]);
  let selected = $state<ProviderId | null>(null);

  async function detectProviders() {
    detecting = true;
    // TODO(P1): replace with `await invoke('detect_providers')` once the
    // Rust command does real detection (vmrun.exe presence, Hyper-V/VMP
    // feature state, WSL distro list, reachable remote host).
    await new Promise((r) => setTimeout(r, 900));
    detections = [
      {
        id: "vmware",
        name: "VMware Workstation / Player",
        available: true,
        recommended: true,
        detail: "vmrun.exe found. Headless appliance VM, host-only NIC. Free since Broadcom.",
      },
      {
        id: "wsl2",
        name: "WSL2",
        available: false,
        recommended: false,
        detail: "wsl.exe present, but the Windows Hypervisor Platform is not yet enabled.",
        warning:
          "Enabling Hyper-V/WHP for WSL2 will degrade VMware Workstation and disable nested virtualization. Not recommended on this machine.",
      },
      {
        id: "remote",
        name: "Remote (SSH)",
        available: false,
        recommended: false,
        detail: "No remote host configured yet. Point at an existing Linux box any time.",
      },
      {
        id: "qemu",
        name: "QEMU (compatibility)",
        available: true,
        recommended: false,
        detail: "Bundled, always available. Pure software emulation — slow, but conflicts with nothing.",
      },
    ];
    selected = detections.find((d) => d.recommended)?.id ?? detections.find((d) => d.available)?.id ?? null;
    detecting = false;
  }

  detectProviders();

  function confirm() {
    if (!selected) return;
    labStore.activeProvider = selected;
    onDismiss();
    void labStore.connect();
  }
</script>

<div class="scrim">
  <div class="modal" role="dialog" aria-label="Runtime provider setup">
    <div class="modal-header">
      <h2>Choose a runtime provider</h2>
      <p class="sub">
        iolbox needs a small Linux runtime to execute IOL/VPCS. Pick where it runs — this never
        changes a Windows system feature automatically.
      </p>
    </div>

    {#if detecting}
      <div class="detecting">
        <div class="spinner" aria-hidden="true"></div>
        Detecting available providers…
      </div>
    {:else}
      <div class="provider-list">
        {#each detections as p (p.id)}
          <label class="provider" class:disabled={!p.available} class:selected={selected === p.id}>
            <input
              type="radio"
              name="provider"
              value={p.id}
              disabled={!p.available}
              checked={selected === p.id}
              onchange={() => (selected = p.id)}
            />
            <div class="provider-body">
              <div class="provider-title-row">
                <span class="provider-name">{p.name}</span>
                {#if p.recommended}
                  <span class="badge recommended">Recommended</span>
                {/if}
                {#if !p.available}
                  <span class="badge unavailable">Unavailable</span>
                {/if}
              </div>
              <div class="provider-detail">{p.detail}</div>
              {#if p.warning}
                <div class="provider-warning">⚠ {p.warning}</div>
              {/if}
            </div>
          </label>
        {/each}
      </div>

      <div class="modal-footer">
        <button class="btn" onclick={detectProviders}>Re-scan</button>
        <button class="btn btn-primary" disabled={!selected} onclick={confirm}>
          Continue
        </button>
      </div>
    {/if}
  </div>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    background: var(--scrim);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 3000;
  }
  .modal {
    width: min(620px, 92vw);
    max-height: 86vh;
    display: flex;
    flex-direction: column;
    background: var(--bg-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    overflow: hidden;
  }
  .modal-header {
    padding: var(--sp-5) var(--sp-5) var(--sp-3);
    border-bottom: 1px solid var(--border);
  }
  .modal-header h2 {
    margin: 0 0 6px;
    font-size: var(--fs-xl);
  }
  .sub {
    margin: 0;
    font-size: var(--fs-sm);
    color: var(--text-secondary);
    line-height: 1.5;
  }
  .detecting {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: var(--sp-6);
    color: var(--text-secondary);
    font-size: var(--fs-sm);
    justify-content: center;
  }
  .spinner {
    width: 18px;
    height: 18px;
    border-radius: 50%;
    border: 2px solid var(--border-strong);
    border-top-color: var(--accent);
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  .provider-list {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    padding: var(--sp-4) var(--sp-5);
    overflow-y: auto;
  }
  .provider {
    display: flex;
    gap: var(--sp-3);
    padding: var(--sp-3);
    border: 1.5px solid var(--border);
    border-radius: var(--radius-md);
    cursor: pointer;
    transition: border-color var(--transition-fast), background var(--transition-fast);
  }
  .provider:hover:not(.disabled) {
    border-color: var(--border-strong);
  }
  .provider.selected {
    border-color: var(--accent);
    background: var(--accent-muted);
  }
  .provider.disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }
  .provider input {
    margin-top: 2px;
    accent-color: var(--accent);
  }
  .provider-body {
    flex: 1;
    min-width: 0;
  }
  .provider-title-row {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    margin-bottom: 3px;
  }
  .provider-name {
    font-weight: 600;
    font-size: var(--fs-md);
  }
  .badge {
    font-size: 9px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    padding: 2px 6px;
    border-radius: var(--radius-sm);
  }
  .badge.recommended {
    background: var(--success);
    color: var(--ground);
  }
  .badge.unavailable {
    background: var(--bg-3);
    color: var(--text-tertiary);
  }
  .provider-detail {
    font-size: var(--fs-xs);
    color: var(--text-secondary);
    line-height: 1.5;
  }
  .provider-warning {
    margin-top: 6px;
    font-size: var(--fs-xs);
    color: var(--warning);
    line-height: 1.5;
  }
  .modal-footer {
    display: flex;
    justify-content: flex-end;
    gap: var(--sp-2);
    padding: var(--sp-4) var(--sp-5);
    border-top: 1px solid var(--border);
  }
</style>
