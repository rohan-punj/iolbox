// Central app state, Svelte 5 runes. One instance shared via module scope
// (simple singleton store — no context provider needed for a single-window app).
import { emptyLab, type Annotation, type LabDocument, type LabLink, type LabNode, type LibraryImage, type NodeState, type LinkFault } from "./labTypes";
import { uuid } from "./uid";
import { SupervisorClient } from "./supervisor";
import { MockTransport } from "./mockTransport";
import { selectTransport } from "./transportSelect";
import { consoleUiStore } from "./consoleUiStore.svelte";
import { CaptureTransport } from "./captureTransport";
import { encodePcapng, PcapngParser, type CapturedPacket, type ParsedPacket } from "./pcapng";
import { appendLensEvents, type EndpointAttribView, type LensAttribution, type LensEvent } from "./lens";
import type { LabStartResult, StatusResult, SupervisorEvent, ToolPackInfo } from "./protocol";

export type ProviderId = "vmware" | "wsl2" | "remote" | "qemu";
export type ProviderStatus = "unknown" | "connecting" | "connected" | "error";

export interface LogLine {
  ts: number;
  level: "debug" | "info" | "warn" | "error";
  message: string;
  node?: number;
}

/** "danger" is visually red like "error" (a deactivating/destructive action —
 *  stop, wipe, force-clean) but is a normal completion, not a failure: it
 *  keeps role="status"/aria-live="polite" instead of "error"'s assertive
 *  interrupt, which is reserved for actual RPC failures (see ToastStack). */
export type ToastSeverity = "success" | "info" | "warning" | "danger" | "error";

export interface ToastNotification {
  id: string;
  key?: string;
  severity: ToastSeverity;
  message: string;
  createdAt: number;
  duration: number;
  count?: number;
  dismissing?: boolean;
}

type ToastInput = Pick<ToastNotification, "severity" | "message"> & {
  key?: string;
  duration?: number;
  count?: number;
};

export interface CaptureSubscriber {
  onReset?(): void;
  onPackets(packets: readonly ParsedPacket[]): void;
  onUnavailable?(): void;
}

interface CaptureSession {
  transport: CaptureTransport;
  parser: PcapngParser;
  subscribers: Set<CaptureSubscriber>;
}

const AUTOSAVE_STORAGE_KEY = "iolbox.autosave.enabled";

function readAutoSaveEnabled(): boolean {
  try {
    const raw = localStorage.getItem(AUTOSAVE_STORAGE_KEY);
    return raw === null ? true : raw === "true";
  } catch {
    return true;
  }
}

class LabStore {
  lab = $state<LabDocument>(emptyLab("Untitled lab"));
  selectedNodeId = $state<number | null>(null);
  /** Which node's Inspector pane is open, independent of selectedNodeId — a
   *  plain click only selects/highlights a node (drag, delete-key, multi-op
   *  targeting), it does NOT pop the right-side editor open. Only an explicit
   *  "Edit…" (right-click menu or double-click) sets this. */
  inspectorNodeId = $state<number | null>(null);
  selectedLinkId = $state<number | null>(null);
  /** Selected canvas annotation (Excalidraw layer). Independent of node/link
   *  selection — annotations never open the node Inspector. */
  selectedAnnotationId = $state<string | null>(null);
  /** Live canvas viewport zoom, mirrored from SvelteFlow by CanvasInner. Used by
   *  annotation resize/drag grips to convert screen deltas to flow deltas. */
  canvasZoom = $state(1);
  /** Live canvas viewport translation, mirrored from SvelteFlow by CanvasInner. */
  canvasPan = $state({ x: 0, y: 0 });
  /** Client→flow coordinate projector, wired by CanvasInner (useSvelteFlow's
   *  screenToFlowPosition). Used by the line-endpoint grips. */
  screenToFlow: ((clientX: number, clientY: number) => { x: number; y: number }) | null = null;
  /** Flow-to-client coordinate projector, wired by CanvasInner. */
  flowToScreen: ((x: number, y: number) => { x: number; y: number }) | null = null;
  nodeStates = $state<Record<number, NodeState>>({});
  /** Per-node action lock (WS1): while an action is in flight on a node, sibling
   *  actions on THAT node are no-ops and the UI shows a lock/progress state.
   *  Set at the entry of start/stop/wipe/duplicate; cleared when the driving
   *  event/promise settles (start/stop → node.state event; wipe/duplicate →
   *  awaited RPC). A 60s safety timeout releases it so a lost event can't wedge
   *  a node permanently. */
  nodeLocks = $state<Record<number, { action: string; startedAt: number; holdUntilSettled?: boolean } | null>>({});
  private nodeLockTimers: Record<number, ReturnType<typeof setTimeout>> = {};
  consolePorts = $state<Record<number, number>>({});
  images = $state<LibraryImage[]>([]);
  /** True while the initial image.list round-trip after (re)connect is still
   *  in flight. On a real supervisor, image.list can take several (observed:
   *  10-15s) seconds to answer under TCG right after boot/reconnect — without
   *  this flag the GUI showed an empty "No images yet" library the whole time,
   *  which reads as "my registered images vanished" on a page refresh even
   *  though they were never actually gone server-side. Drives a "Loading
   *  images…" hint instead of a false empty state; never fake the list. */
  imagesLoading = $state(false);
  /** Installed learning-tool pack metadata for the palette and tool editor. */
  toolPacks = $state<ToolPackInfo[]>([]);
  toolPacksLoading = $state(false);
  toolPacksError = $state<string | null>(null);
  logs = $state<LogLine[]>([]);
  toasts = $state<ToastNotification[]>([]);
  /** Last user-visible failure (start/stop/load); shown in the top bar until
   *  the next successful action clears it. Never silently swallow errors. */
  lastError = $state<string | null>(null);
  providerStatus = $state<ProviderStatus>("unknown");
  activeProvider = $state<ProviderId | null>(null);
  labRunning = $derived(
    Object.values(this.nodeStates).some((s) => s === "running" || s === "starting")
  );
  openConsoleTabs = $state<number[]>([]);
  activeConsoleTab = $state<number | null>(null);
  showPreflight = $state(true);
  showImageManager = $state(false);
  showLabBrowser = $state(false);
  showSettings = $state(false);
  showLinkFault = $state<{ linkId: number } | null>(null);
  /** Tasks pane toggle (TopBar checklist). When on it takes precedence over the
   *  empty-selection auto-hide of the right pane. */
  showTasks = $state(false);
  /** Supervisor feature flags from the hello handshake (e.g. "natgw").
   *  Drives feature-gated palette entries. */
  features = $state<string[]>([]);
  /** Supervisor build version from the hello handshake (git describe, baked in
   *  via build-release.sh's -ldflags). Empty until connected; surfaced in the
   *  resource-bar footer so staleness is visible at a glance. */
  supervisorVersion = $state<string>("");
  /** WS6 — internet-egress capability from the hello handshake. "slirp" (QEMU
   *  user-mode NAT) terminates ICMP so ping/traceroute to the internet do not
   *  work through the NAT node; "routed" (or absent/unknown) is a full path.
   *  Drives the inform-only NAT-node warning badge. */
  egress = $state<"slirp" | "routed" | "">("");
  egressNote = $state<string>("");
  /** Per-link forwarded-throughput samples, keyed by link id. FloatingEdge reads
   *  these to drive the traffic glow; entries older than ~5s are treated stale.
   *  `protos` (Network Watcher) is the optional per-protocol fps breakdown;
   *  `protosDir` the directional one ([from endpoints[0], from endpoints[1]]
   *  fps per label) that drives the watcher's animated dash overlays.
   *  `protosSubtypeDir` drills one level deeper — label → subtype → directional
   *  fps — so the watcher can filter by packet type (e.g. BGP keepalive). */
  linkStats = $state<Record<number, {
    fps: number;
    bps: number;
    ts: number;
    protos?: Record<string, number>;
    protosDir?: Record<string, [number, number]>;
    protosSubtypeDir?: Record<string, Record<string, [number, number]>>;
    epAttrib?: EndpointAttribView[];
  }>>({});
  /** Authoritative runtime fault state, separate from link.stats because idle
   * and admin-down links do not emit throughput samples. */
  linkFaults = $state<Record<number, {
    fault: LinkFault | null;
    active: boolean;
    reason?: string;
  }>>({});
  /** Latest runtime-VM resource sample (host.stats), or null until the first
   *  event. Drives the left-pane host monitor. */
  hostStats = $state<{
    cpuPct: number;
    memUsed: number;
    memTotal: number;
    diskUsed: number;
    diskTotal: number;
    cores: number;
  } | null>(null);
  /** Coarse ~1s wall clock so glow readers can decay stats to zero when the
   *  events stop arriving (link.stats is silent for idle links). */
  nowTick = $state(Date.now());
  /** Open live-capture console tabs (feature 1), keyed by link id. Kept separate
   *  from node console tabs (openConsoleTabs) which are keyed by node id. */
  openCaptureTabs = $state<number[]>([]);
  /** Protocol Lens tabs are views over openCaptureTabs, never independent
   *  streams. Closing their capture closes the Lens too. */
  openLensTabs = $state<number[]>([]);
  /** Bounded, session-only Lens ring: last 2000 events per link. */
  lensEvents = $state<Record<number, LensEvent[]>>({});
  /** Live pcapng tee TCP port per capturing link, learned from capture.started
   *  events (mirrors how consolePorts tracks node.console). Drives the native
   *  Wireshark flip (wireshark -k -i TCP@<host>:<port>) and capture-tab
   *  reconnects; entries clear on capture.stopped / lab switch. */
  capturePorts = $state<Record<number, number>>({});
  /** Transient signal (feature: link-menu "Capture in Wireshark…"): set to a
   *  link id to ask Console.svelte to select that capture tab and flip it
   *  straight to the native-Wireshark overlay. Console.svelte resets this to
   *  null once it has acted on it — it is a one-shot request, not state to
   *  read back. */
  wiresharkOverlayFor = $state<number | null>(null);
  /** Ids of docs previously saved to the durable store — gates autosave (only
   *  autosave a lab the user has explicitly saved at least once). */
  private savedDocIds = new Set<string>();
  lastSavedAt = $state<number | null>(null);
  private autosaveTimer: ReturnType<typeof setTimeout> | null = null;
  /** User-facing toggle (Settings dialog) for scheduleAutosave — on by
   *  default so lab/node edits, exported configs, etc. are preserved
   *  without an explicit Save click. Persisted so it survives a reload. */
  autoSaveEnabled = $state(readAutoSaveEnabled());
  private toastQueue: ToastNotification[] = [];
  private toastTimers = new Map<string, ReturnType<typeof setTimeout>>();
  private toastExitTimers = new Map<string, ReturnType<typeof setTimeout>>();
  private toastTimerStartedAt = new Map<string, number>();
  private toastRemaining = new Map<string, number>();
  private toastPauseCounts = new Map<string, number>();
  private toastId = 0;
  private captureBursts = new Map<
    "started" | "stopped",
    { links: Set<number>; timer: ReturnType<typeof setTimeout> }
  >();
  private labStopPending = false;
  private labStopSuppressedUntil = 0;
  /** One parser and one WebSocket per open capture link. CaptureTerm and the
   *  Lens subscribe to this session instead of opening parallel streams. */
  private captureSessions = new Map<number, CaptureSession>();
  /** True while a lab.load round-trip is in flight. Guards against a second
   *  New/Open landing mid-load, which would otherwise interleave two
   *  loadLab() calls and let a slow first response's runtime state (console
   *  ports, node states) overwrite the second lab's after the fact. */
  labLoading = $state(false);

  client: SupervisorClient;
  /** Only set when the mock transport was actually selected; see mockTransport getter. */
  private mock: MockTransport | null = null;
  /** "ws" when talking to a real supervisor (browser build served by it); the
   *  Preflight provider-picker (Tauri-only, P2) is meaningless in that case. */
  readonly transportKind: "mock" | "ws";

  constructor() {
    const sel = selectTransport();
    this.transportKind = sel.kind;
    if (sel.kind === "mock") this.mock = sel.transport as MockTransport;
    this.client = new SupervisorClient(sel.transport);
    if (sel.kind === "ws") {
      // Served by the supervisor itself: it already IS the runtime, so skip
      // the desktop-only provider-detection modal and connect immediately.
      this.showPreflight = false;
      this.activeProvider = null;
      void this.connect();
    }
    // Drive glow decay: bump a coarse clock every second so FloatingEdge
    // re-evaluates staleness even when no new link.stats events arrive.
    setInterval(() => {
      this.nowTick = Date.now();
    }, 1000);
  }

  /** Only meaningful when transportKind === "mock"; null under a real ws transport. */
  get mockTransport(): MockTransport | null {
    return this.mock;
  }

  private toastDuration(severity: ToastSeverity): number {
    switch (severity) {
      case "success":
        return 4_000;
      case "info":
        return 4_500;
      case "warning":
        return 6_000;
      case "danger":
        return 6_000;
      case "error":
        return 8_000;
    }
  }

  enqueueToast(input: ToastInput): string {
    const now = Date.now();
    const duration = input.duration ?? this.toastDuration(input.severity);
    const existingIndex = input.key
      ? this.toasts.findIndex((toast) => toast.key === input.key)
      : -1;
    const queuedIndex = input.key
      ? this.toastQueue.findIndex((toast) => toast.key === input.key)
      : -1;

    if (existingIndex >= 0 || queuedIndex >= 0) {
      const visible = existingIndex >= 0;
      const existing = visible ? this.toasts[existingIndex] : this.toastQueue[queuedIndex];
      const updated: ToastNotification = {
        ...existing,
        severity: input.severity,
        message: input.message,
        createdAt: now,
        duration,
        count: input.count,
        dismissing: false,
      };
      this.clearToastExitTimer(existing.id);
      this.toastRemaining.set(existing.id, duration);
      this.clearToastTimer(existing.id);
      if (visible) {
        this.toasts = this.toasts.map((toast, index) => (index === existingIndex ? updated : toast));
        if (!this.toastPauseCounts.get(existing.id)) this.startToastTimer(existing.id);
      } else {
        this.toastQueue[queuedIndex] = updated;
      }
      return existing.id;
    }

    const toast: ToastNotification = {
      id: `toast-${now}-${++this.toastId}`,
      key: input.key,
      severity: input.severity,
      message: input.message,
      createdAt: now,
      duration,
      count: input.count,
    };
    while (this.toasts.length + this.toastQueue.length >= 20 && this.toastQueue.length > 0) {
      const dropIndex = this.toastQueue.findIndex((queued) => queued.severity !== "error");
      if (dropIndex < 0 && toast.severity !== "error") return toast.id;
      const [dropped] = this.toastQueue.splice(dropIndex >= 0 ? dropIndex : 0, 1);
      if (dropped) this.toastRemaining.delete(dropped.id);
    }
    this.toastRemaining.set(toast.id, duration);
    if (this.toasts.length < 4) {
      this.toasts = [toast, ...this.toasts];
      this.startToastTimer(toast.id);
    } else {
      this.toastQueue.push(toast);
    }
    return toast.id;
  }

  dismissToast(id: string) {
    const visibleIndex = this.toasts.findIndex((toast) => toast.id === id);
    if (visibleIndex < 0) {
      const queuedIndex = this.toastQueue.findIndex((toast) => toast.id === id);
      if (queuedIndex >= 0) this.toastQueue.splice(queuedIndex, 1);
      return;
    }
    const toast = this.toasts[visibleIndex];
    if (toast.dismissing) return;
    this.clearToastTimer(id);
    this.toastPauseCounts.delete(id);
    this.toasts = this.toasts.map((item, index) =>
      index === visibleIndex ? { ...item, dismissing: true } : item
    );
    this.clearToastExitTimer(id);
    this.toastExitTimers.set(
      id,
      setTimeout(() => {
        this.toastExitTimers.delete(id);
        this.toasts = this.toasts.filter((item) => item.id !== id);
        this.toastRemaining.delete(id);
        this.promoteToast();
      }, 120)
    );
  }

  pauseToast(id: string) {
    const toast = this.toasts.find((item) => item.id === id);
    if (!toast || toast.dismissing) return;
    const count = this.toastPauseCounts.get(id) ?? 0;
    this.toastPauseCounts.set(id, count + 1);
    if (count > 0) return;
    const startedAt = this.toastTimerStartedAt.get(id);
    if (startedAt !== undefined) {
      const remaining = this.toastRemaining.get(id) ?? toast.duration;
      this.toastRemaining.set(id, Math.max(0, remaining - (Date.now() - startedAt)));
    }
    this.clearToastTimer(id);
  }

  resumeToast(id: string) {
    const count = this.toastPauseCounts.get(id) ?? 0;
    if (count === 0) return;
    if (count <= 1) this.toastPauseCounts.delete(id);
    else this.toastPauseCounts.set(id, count - 1);
    if (count > 1) return;
    const toast = this.toasts.find((item) => item.id === id);
    if (toast && !toast.dismissing) this.startToastTimer(id);
  }

  resolveNodeName(id: number): string {
    return this.lab.nodes.find((node) => node.id === id)?.name ?? `Node ${id}`;
  }

  private clearToastTimer(id: string) {
    const timer = this.toastTimers.get(id);
    if (timer) clearTimeout(timer);
    this.toastTimers.delete(id);
    this.toastTimerStartedAt.delete(id);
  }

  private clearToastExitTimer(id: string) {
    const timer = this.toastExitTimers.get(id);
    if (timer) clearTimeout(timer);
    this.toastExitTimers.delete(id);
  }

  private startToastTimer(id: string) {
    const toast = this.toasts.find((item) => item.id === id);
    if (!toast || toast.dismissing || this.toastPauseCounts.get(id)) return;
    this.clearToastTimer(id);
    const remaining = this.toastRemaining.get(id) ?? toast.duration;
    if (remaining <= 0) {
      this.dismissToast(id);
      return;
    }
    this.toastTimerStartedAt.set(id, Date.now());
    this.toastTimers.set(id, setTimeout(() => this.dismissToast(id), remaining));
  }

  private promoteToast() {
    while (this.toasts.length < 4 && this.toastQueue.length > 0) {
      const next = this.toastQueue.shift()!;
      this.toasts = [...this.toasts, next];
      this.startToastTimer(next.id);
    }
  }

  private coalesceCapture(kind: "started" | "stopped", linkId: number) {
    if (kind === "stopped" && this.labStopReplaySuppressed()) return;
    const existing = this.captureBursts.get(kind);
    if (existing) {
      existing.links.add(linkId);
      return;
    }
    const links = new Set([linkId]);
    const timer = setTimeout(() => {
      this.captureBursts.delete(kind);
      const count = links.size;
      const word = kind === "started" ? "started" : "stopped";
      this.enqueueToast({
        severity: "info",
        message: count === 1 ? `Capture ${word} — link ${[...links][0]}` : `${count} captures ${word}`,
        count: count > 1 ? count : undefined,
      });
    }, 750);
    this.captureBursts.set(kind, { links, timer });
  }

  private labStopReplaySuppressed(): boolean {
    return this.labStopPending || Date.now() < this.labStopSuppressedUntil;
  }

  async connect() {
    this.providerStatus = "connecting";
    // Subscribe before the handshake settles so no push (node.state etc.)
    // arriving mid-connect is dropped.
    this.client.onEvent((evt) => this.handleEvent(evt));
    // A transport reconnect (network blip, or a burst of activity like Stop
    // all + Start all tripping the WS) means every push event sent during the
    // gap is gone for good — there's no server-side replay (see
    // broadcaster.publish). Without this, nodeStates/consolePorts/
    // capturePorts stay frozen at whatever they were right before the drop
    // until the user manually reloads the page; a bulk stop-then-restart
    // landing in that gap left every LED stuck non-green even though the
    // nodes were actually running. Re-query status() and refresh them instead
    // of waiting for a page reload to notice.
    this.client.onReconnect(() => void this.resyncAfterReconnect());
    try {
      const hello = await this.client.connect();
      this.features = hello.features ?? [];
      this.supervisorVersion = hello.supervisor ?? "";
      // WS6 — absent egress defaults to the permissive "routed" (no badge).
      this.egress = hello.egress ?? "routed";
      this.egressNote = hello.egressNote ?? "";
      this.providerStatus = "connected";
      // Real supervisor: the runtime provider is whatever process spawned it,
      // not a Windows-side choice — leave activeProvider as Preflight (mock
      // path) or unset (ws path) already set it, rather than hardcoding.
      if (this.transportKind === "mock") this.activeProvider = "vmware";
      this.imagesLoading = true;
      try {
        const { images } = await this.client.imageList();
        this.images = images;
      } finally {
        this.imagesLoading = false;
      }
      this.toolPacksLoading = true;
      this.toolPacksError = null;
      try {
        const { packs } = await this.client.listPacks();
        this.toolPacks = packs;
      } catch (e) {
        this.toolPacksError = (e as Error).message;
        this.pushLog("warn", `learning-tool packs unavailable: ${this.toolPacksError}`);
      } finally {
        this.toolPacksLoading = false;
      }
      // WS2 — "adopt, don't load": if the supervisor is already running a lab
      // (survived our disconnect — e.g. this is a browser refresh, not a fresh
      // supervisor boot), hydrate the GUI from its live status INSTEAD of
      // calling lab.load, which deliberately tears down the previously-loaded
      // lab first (the orphan-fix in handleLabLoad). Without this, every
      // refresh killed the user's running IOL nodes. Only fall back to the
      // restore/seed+load path when nothing is actually up.
      const status = await this.client.status();
      const anyUp = status.nodes.some((n) => n.state !== "stopped");
      const adopted = status.labId && anyUp ? await this.adoptRunningLab(status) : false;
      if (!adopted) {
        // Reload the last lab the user was working on (so a browser refresh
        // keeps their additions) instead of the throwaway seed. Falls back to
        // the seed when there's no remembered lab or it's gone from the store.
        const restored = await this.restoreLastActiveLab();
        if (!restored) {
          this.reconcileNodeImages();
          await this.loadLab(this.lab);
        }
      }
    } catch (e) {
      this.providerStatus = "error";
      this.lastError = `connect failed: ${(e as Error).message}`;
      this.pushLog("error", `connect failed: ${(e as Error).message}`);
    }
  }

  /** Refresh live node/link runtime state after a transport reconnect (see
   *  the onReconnect subscription in connect()). Only handles the common
   *  case — the supervisor still has THIS lab loaded, which is true for any
   *  network-level blip since the supervisor process itself never restarted.
   *  If it reports a different (or no) lab — the supervisor process itself
   *  restarted during the drop — this can't safely reconcile locally; ask for
   *  a manual reload rather than guess. */
  private async resyncAfterReconnect() {
    try {
      const status = await this.client.status();
      if (!status.labId || status.labId !== this.lab.id) {
        this.pushLog(
          "warn",
          "reconnected, but the supervisor is no longer tracking this lab (it may have restarted) — reload the page"
        );
        return;
      }
      const states: Record<number, NodeState> = {};
      for (const n of status.nodes ?? []) states[n.id] = n.state;
      for (const n of this.lab.nodes) if (!(n.id in states)) states[n.id] = "stopped";
      this.nodeStates = states;
      const consolePorts: Record<number, number> = {};
      for (const n of status.nodes ?? []) if (n.consolePort) consolePorts[n.id] = n.consolePort;
      this.consolePorts = consolePorts;
      const capPorts: Record<number, number> = {};
      for (const l of status.links ?? []) if (l.capturePort) capPorts[l.id] = l.capturePort;
      this.capturePorts = capPorts;
      this.pushLog("info", "reconnected to the supervisor — refreshed node/link state");
    } catch (e) {
      this.pushLog("warn", `reconnect: failed to refresh state: ${(e as Error).message}`);
    }
  }

  /**
   * WS2 — hydrate the GUI from a lab the supervisor is ALREADY running,
   * without calling lab.load (which tears the running lab down first). The
   * target is always `status.labId` — if the supervisor is running a
   * DIFFERENT lab than the one this browser last remembered, the supervisor
   * wins (it reflects reality; the GUI's memory does not) and we adopt
   * whatever it reports. Returns true on success; false only in the rare case
   * the doc can't be fetched from the durable store (e.g. it was deleted from
   * under a still-running lab), in which case the caller falls back to the
   * normal restore/seed+load path — that path still calls loadLab, but there
   * is no doc to adopt into, so there is nothing more graceful to do here.
   */
  private async adoptRunningLab(status: StatusResult): Promise<boolean> {
    let lab: LabDocument;
    try {
      const res = await this.client.labGetDoc(status.labId!);
      if (!res.lab || !Array.isArray(res.lab.nodes)) throw new Error("empty doc");
      lab = res.lab;
    } catch (e) {
      this.pushLog(
        "warn",
        `lab ${status.labId} is running but its stored doc could not be read (${(e as Error).message}) — falling back to the normal load path`
      );
      return false;
    }

    this.lab = lab;
    this.reconcileNodeImages();

    // Clear stale per-lab UI state exactly like loadLab does, EXCEPT the live
    // runtime maps (nodeStates/consolePorts/capturePorts), which we are about
    // to populate from the supervisor's actual status instead of resetting.
    this.linkStats = {};
    this.linkFaults = {};
    this.openConsoleTabs = [];
    this.closeAllCaptureSessions();
    this.openCaptureTabs = [];
    this.openLensTabs = [];
    this.captureBuffers.clear();
    this.captureRecorded = {};
    this.lensEvents = {};
    this.activeConsoleTab = null;

    const states: Record<number, NodeState> = {};
    const ports: Record<number, number> = {};
    for (const n of status.nodes ?? []) {
      states[n.id] = n.state;
      if (n.consolePort) ports[n.id] = n.consolePort;
    }
    // A node in the doc that the supervisor didn't report (shouldn't happen —
    // status always mirrors the loaded doc's node set — but keep the GUI from
    // showing an undefined state) defaults to stopped.
    for (const n of lab.nodes) {
      if (!(n.id in states)) states[n.id] = "stopped";
    }
    this.nodeStates = states;
    this.consolePorts = ports;

    const capPorts: Record<number, number> = {};
    for (const l of status.links ?? []) {
      if (l.capturePort) capPorts[l.id] = l.capturePort;
    }
    this.capturePorts = capPorts;

    this.savedDocIds.add(lab.id);
    this.rememberActiveLab(lab.id);
    this.pushLog("info", `adopted already-running lab "${lab.name}" — no restart`);
    return true;
  }

  /**
   * Remap node image ids against the connected supervisor's registry. A lab
   * doc can carry ids the backend doesn't know (the seeded demo topology's
   * placeholder ids, a doc from another machine, an image since deleted):
   * remap each unknown id to a registered image of the same class when one
   * exists. When none exists the reference is KEPT: lab.load tolerates
   * unknown image ids (only start resolves them) and the startLab pre-check
   * names the node, while the ref's class is what a later pass — e.g. after
   * the first upload on a fresh runtime — needs to remap by. Clearing it
   * (the old behavior) made lab.load itself fail schema_invalid on a fresh
   * runtime AND autosave persisted the damage.
   * Returns the ids of nodes whose image binding changed.
   */
  private reconcileNodeImages(): number[] {
    const changed: number[] = [];
    for (const node of this.lab.nodes) {
      if (node.kind !== "iol" || !node.image) continue;
      if (this.images.some((i) => i.id === node.image!.id)) continue;
      const substitute = this.images.find((i) => i.class === node.image!.class);
      if (substitute) {
        this.pushLog(
          "info",
          `node ${node.name}: image ${node.image.filename} not registered here — using ${substitute.filename}`
        );
        node.image = { id: substitute.id, filename: substitute.filename, class: substitute.class };
        changed.push(node.id);
      } else {
        this.pushLog(
          "warn",
          `node ${node.name}: image ${node.image.filename} is not registered and no ${node.image.class} image is available yet — upload one (Images)`
        );
      }
    }
    return changed;
  }

  /**
   * Called after the image registry changed (upload/register): adopt the new
   * list, heal any node refs that couldn't be mapped before, and push the
   * result to the supervisor. On a fresh runtime the seed lab's placeholder
   * ids only become startable at this moment — without this, starting kept
   * failing until a full browser refresh (or forever, when the cleared refs
   * had been autosaved).
   */
  async onImagesUpdated(images: LibraryImage[]) {
    this.images = images;
    const changed = this.reconcileNodeImages();
    if (changed.length === 0) return;
    this.scheduleAutosave();
    const busy = this.lab.nodes.some(
      (n) => this.nodeStates[n.id] === "running" || this.nodeStates[n.id] === "starting"
    );
    try {
      if (!busy) {
        // Nothing running: re-send the whole doc so the loaded lab (or a lab
        // whose load failed earlier) carries the healed image ids.
        await this.loadLab(this.lab);
      } else {
        // Something is running: hot-swap only the healed nodes (they were
        // imageless, so they are not among the running ones).
        for (const id of changed) {
          const node = this.lab.nodes.find((n) => n.id === id);
          if (node?.image) await this.client.nodeSetImage(this.lab.id, id, node.image.id);
        }
      }
    } catch (e) {
      this.lastError = `apply uploaded image: ${(e as Error).message}`;
      this.pushLog("error", this.lastError);
    }
  }

  private handleEvent(evt: SupervisorEvent) {
    switch (evt.event) {
      case "node.state":
        this.nodeStates = { ...this.nodeStates, [evt.data.node]: evt.data.state };
        // Release a start/stop lock once the node reaches a TERMINAL state
        // (WS1). The state machine (supervisor/internal/node/state.go) always
        // fires an intermediate node.state="starting" event the instant a
        // start begins — releasing on ANY node.state (as before) cleared the
        // lock within milliseconds of clicking Start, long before the node
        // actually finished booting. A user re-clicking Start in that window
        // re-acquired the lock, but the node was already in "starting" so the
        // backend's second start is a no-op state-machine transition that
        // never fires another event — wedging that lock until the real
        // running/crashed event (or the 60s safety timeout). Only "running",
        // "stopped", and "crashed" are terminal; "starting" must NOT release.
        if (evt.data.state !== "starting" && !this.nodeLocks[evt.data.node]?.holdUntilSettled) this.releaseNodeLock(evt.data.node);
        if (evt.data.state === "crashed") {
          this.enqueueToast({
            key: `node-crashed:${evt.data.node}`,
            severity: "error",
            message: `${this.resolveNodeName(evt.data.node)} crashed`,
          });
        }
        break;
      case "node.console":
        this.consolePorts = { ...this.consolePorts, [evt.data.node]: evt.data.consolePort };
        break;
      case "node.pcState": {
        const n = this.lab.nodes.find((item) => item.id === evt.data.node);
        if (n) n.config = { ...(n.config ?? {}), pc: { ...evt.data.state } };
        break;
      }
      case "capture.started":
        this.capturePorts = { ...this.capturePorts, [evt.data.link]: evt.data.capturePort };
        this.coalesceCapture("started", evt.data.link);
        break;
      case "capture.stopped": {
        const next = { ...this.capturePorts };
        delete next[evt.data.link];
        this.capturePorts = next;
        this.coalesceCapture("stopped", evt.data.link);
        break;
      }
      case "link.stats":
        // Replace the whole record so $derived/$effect readers re-run; carry a
        // receive timestamp so FloatingEdge can expire stale glow.
        const stats = evt.data as typeof evt.data & { epAttrib?: EndpointAttribView[] };
        this.linkStats = {
          ...this.linkStats,
          [evt.data.link]: {
            fps: evt.data.fps,
            bps: evt.data.bps,
            ts: Date.now(),
            protos: stats.protos,
            protosDir: stats.protosDir,
            protosSubtypeDir: stats.protosSubtypeDir,
            epAttrib: stats.epAttrib,
          },
        };
        break;
      case "link.fault": {
        this.linkFaults = {
          ...this.linkFaults,
          [evt.data.link]: {
            fault: evt.data.fault,
            active: evt.data.active,
            reason: evt.data.reason,
          },
        };
        // Fault definitions are part of the in-memory document so the normal
        // lab.saveDoc path persists the same state the user sees. Active is
        // deliberately runtime-only; inactive-on-reopen is represented by the
        // persisted definition plus the absence of an active event.
        const link = this.lab.links.find((l) => l.id === evt.data.link);
        if (link) {
          if (evt.data.fault) link.fault = { ...evt.data.fault };
          else delete link.fault;
        }
        if (evt.data.active && evt.data.fault) {
          this.enqueueToast({
            key: `link-fault:${evt.data.link}`,
            severity: "warning",
            message: `Link ${evt.data.link} impairment active`,
          });
        } else if (!evt.data.active && !evt.data.fault && !this.labStopReplaySuppressed()) {
          this.enqueueToast({
            key: `link-fault:${evt.data.link}`,
            severity: "success",
            message: `Link ${evt.data.link} restored`,
          });
        }
        break;
      }
      case "host.stats":
        this.hostStats = { ...evt.data };
        break;
      case "log":
        this.pushLog(evt.data.level, evt.data.message, evt.data.node);
        break;
      default:
        break;
    }
  }

  pushLog(level: LogLine["level"], message: string, node?: number) {
    this.logs = [...this.logs.slice(-199), { ts: Date.now(), level, message, node }];
  }

  async loadLab(lab: LabDocument) {
    // A pending autosave belongs to the PREVIOUS lab — left running, it fires
    // ~1.2s from now and saves whatever this.lab has become by then (i.e. the
    // lab we're about to switch to), silently persisting a brand-new/unedited
    // lab the user never asked to save. Cancel it before swapping docs.
    if (this.autosaveTimer) {
      clearTimeout(this.autosaveTimer);
      this.autosaveTimer = null;
    }
    this.labLoading = true;
    try {
      return await this.loadLabInner(lab);
    } finally {
      this.labLoading = false;
    }
  }

  private async loadLabInner(lab: LabDocument) {
    this.lab = lab;
    // Remap image ids against this supervisor's registry (same as connect()).
    this.reconcileNodeImages();
    // Clear any lingering runtime/glow state from the previous lab.
    this.linkStats = {};
    this.linkFaults = {};
    this.openConsoleTabs = [];
    this.closeAllCaptureSessions();
    this.openCaptureTabs = [];
    this.openLensTabs = [];
    this.capturePorts = {};
    this.captureBuffers.clear();
    this.captureRecorded = {};
    this.lensEvents = {};
    this.activeConsoleTab = null;
    const res = await this.client.labLoad(lab);
    const ports: Record<number, number> = {};
    for (const n of res.nodes ?? []) {
      ports[n.id] = n.consolePort;
    }
    this.consolePorts = ports;
    if (res.adopted) {
      // Server-side adopt (WS2): this happened because the lab we just "loaded"
      // was ALREADY running (e.g. a second browser tab opening the same lab) —
      // the supervisor serviced it without any teardown and handed back the
      // EXISTING console ports (already captured above). Resetting nodeStates
      // to all-"stopped" here would lie to this tab about a lab that is
      // actually up, so query real state instead.
      try {
        const status = await this.client.status();
        const states: Record<number, NodeState> = {};
        for (const n of status.nodes ?? []) states[n.id] = n.state;
        for (const n of lab.nodes) if (!(n.id in states)) states[n.id] = "stopped";
        this.nodeStates = states;
        const capPorts: Record<number, number> = {};
        for (const l of status.links ?? []) if (l.capturePort) capPorts[l.id] = l.capturePort;
        this.capturePorts = capPorts;
      } catch {
        // status() failing here is unusual (we just got a reply from the same
        // supervisor) — fall back to "stopped" rather than throw, same as the
        // non-adopted path below would have shown anyway.
        const states: Record<number, NodeState> = {};
        for (const n of lab.nodes) states[n.id] = "stopped";
        this.nodeStates = states;
      }
      return;
    }
    const states: Record<number, NodeState> = {};
    for (const n of lab.nodes) {
      states[n.id] = "stopped";
    }
    this.nodeStates = states;
  }

  // ---- durable lab-document store (feature 3) ----

  /** True when the current lab has unsaved content: it has nodes and was
   *  never saved to the durable store this session. Approximate (doesn't
   *  detect edits made after a save within the same session), but matches
   *  what the three former per-entry-point confirm() prompts already used —
   *  now centralized for SwitchLabDialog. */
  get currentLabDirty(): boolean {
    return this.lab.nodes.length > 0 && !this.savedDocIds.has(this.lab.id);
  }

  /** localStorage key holding the id of the lab the user last had open, so a
   *  refresh reopens it rather than the throwaway seed. */
  private static LAST_LAB_KEY = "iolbox.lastActiveLab";

  private rememberActiveLab(id: string) {
    try {
      localStorage.setItem(LabStore.LAST_LAB_KEY, id);
    } catch {
      /* private-mode / storage disabled — non-fatal */
    }
  }

  /** On (re)connect, reopen the lab the user last worked on from the durable
   *  store. Returns true if a stored lab was loaded; false to fall back to the
   *  seed. Any failure (no id, deleted, unreadable) falls back silently. */
  private async restoreLastActiveLab(): Promise<boolean> {
    let id: string | null = null;
    try {
      id = localStorage.getItem(LabStore.LAST_LAB_KEY);
    } catch {
      id = null;
    }
    if (!id) return false;
    try {
      const { lab } = await this.client.labGetDoc(id);
      if (!lab || !Array.isArray(lab.nodes)) return false;
      this.savedDocIds.add(lab.id);
      await this.loadLab(lab);
      this.reconcileNodeImages();
      return true;
    } catch {
      return false;
    }
  }

  /** Persist the current doc, stamping `modified`. Returns true on success. */
  async saveLab(notify = true): Promise<boolean> {
    const labName = this.lab.name;
    this.lab.modified = new Date().toISOString();
    let ok = false;
    await this.guarded("save lab", async () => {
      const synced = await this.client.pcSyncState(this.lab.id);
      for (const item of synced.states ?? []) {
        const n = this.lab.nodes.find((candidate) => candidate.id === item.node);
        if (n) n.config = { ...(n.config ?? {}), pc: { ...item.state } };
      }
      const res = await this.client.labSaveDoc($state.snapshot(this.lab) as LabDocument);
      const id = res.id ?? this.lab.id;
      this.savedDocIds.add(id);
      this.rememberActiveLab(id);
      this.lastSavedAt = Date.now();
      ok = true;
    });
    if (ok && notify) this.enqueueToast({ severity: "success", message: `Lab saved — ${labName}` });
    return ok;
  }

  async listLabs(): Promise<LabDocument[]> {
    const res = await this.client.labListDocs();
    // Any doc that came back from disk is, by definition, already saved.
    for (const l of res.labs) this.savedDocIds.add(l.id);
    return res.labs;
  }

  async deleteLab(labId: string, labName = labId) {
    const ok = await this.guarded("delete lab", async () => {
      await this.client.labDeleteDoc(labId);
      this.savedDocIds.delete(labId);
    });
    if (ok) this.enqueueToast({ severity: "info", message: `Lab deleted — ${labName}` });
  }

  /** Clone a stored lab under a fresh id + " (copy)" name and persist the copy,
   *  so the original (e.g. a built-in starter lab) stays pristine while the user
   *  edits the clone. Returns the new doc, or null on failure. Existing "copy"
   *  names get a numeric suffix so repeated clones don't collide. */
  async cloneLab(source: LabDocument): Promise<LabDocument | null> {
    let out: LabDocument | null = null;
    const ok = await this.guarded("clone lab", async () => {
      const now = new Date().toISOString();
      const existing = new Set((await this.client.labListDocs()).labs.map((l) => l.name));
      let name = `${source.name} (copy)`;
      for (let n = 2; existing.has(name); n++) name = `${source.name} (copy ${n})`;
      const copy: LabDocument = {
        ...structuredClone($state.snapshot(source) as LabDocument),
        id: uuid(),
        name,
        created: now,
        modified: now,
      };
      await this.client.labSaveDoc(copy);
      this.savedDocIds.add(copy.id);
      out = copy;
    });
    const copy = out as LabDocument | null;
    if (ok && copy) this.enqueueToast({ severity: "success", message: `Lab copy created — ${copy.name}` });
    return out;
  }

  /** Set when openLab is blocked on the user resolving a lab switch (see
   *  pendingSwitch doc below). Read by SwitchLabDialog to render itself. */
  pendingSwitch = $state<{ lab: LabDocument; fromStore: boolean } | null>(null);

  /** Open a doc into the workspace (reuses loadLab's connect-time path). Pass
   *  fromStore=true when the doc came from the durable store (so it's treated as
   *  already-saved and eligible for autosave); false for New/Import of a doc the
   *  user hasn't persisted yet.
   *
   *  iolbox runs exactly one lab at a time: switching to a DIFFERENT lab id
   *  always closes/replaces whatever is currently loaded (the server's
   *  lab.load already stops every node and tears down the old lab's fabric —
   *  see handleLabLoad). Per product decision, that must never happen without
   *  the user being warned and given the chance to save first, so any
   *  cross-id call is intercepted here into pendingSwitch instead of
   *  proceeding straight to loadLab; SwitchLabDialog resolves it via
   *  resolveSwitch/cancelSwitch. This is the SINGLE funnel for every
   *  lab-switch entry point (Labs browser open/clone, New, Import) — callers
   *  should not roll their own confirm() before calling openLab. */
  async openLab(lab: LabDocument, fromStore = false, decision?: "save" | "discard") {
    // A load is already in flight (e.g. a double-click on New, or Open
    // clicked again before the first one settled) — let it finish rather
    // than interleaving two loadLab() calls, which can let a slow first
    // response's runtime state overwrite the second lab's after the fact.
    if (this.labLoading) return;
    if (!decision && this.lab.id !== lab.id) {
      this.pendingSwitch = { lab, fromStore };
      return;
    }
    this.pendingSwitch = null;
    await this.guarded("open lab", async () => {
      if (fromStore) this.savedDocIds.add(lab.id);
      else this.savedDocIds.delete(lab.id);
      await this.loadLab(lab);
      // This lab is now the workspace — a refresh should reopen it. (A brand-new
      // unsaved lab isn't in the store yet; its first edit autosaves + records
      // it, and New-lab intentionally isn't remembered until the user edits it.)
      if (fromStore) this.rememberActiveLab(lab.id);
    });
  }

  /** Resolve a pending lab switch (see pendingSwitch/openLab). "save" saves
   *  the OUTGOING lab first and aborts the switch if the save fails (the
   *  dialog stays up so the user can retry or cancel); "discard" proceeds
   *  straight to the switch, leaving the outgoing lab's unsaved edits
   *  behind. Either way loadLab itself already cancels the outgoing lab's
   *  pending autosave timer without flushing it, so "discard" can never be
   *  turned into an accidental save by a debounce firing mid-switch. */
  async resolveSwitch(decision: "save" | "discard") {
    const pending = this.pendingSwitch;
    if (!pending) return;
    if (decision === "save") {
      if (!(await this.saveLab())) return; // save failed: leave the dialog up
    }
    await this.openLab(pending.lab, pending.fromStore, decision);
  }

  /** Dismiss a pending lab switch with no action; the current lab stays open. */
  cancelSwitch() {
    this.pendingSwitch = null;
  }

  /** Debounced autosave after any topology/annotation edit (real supervisor
   *  only — mock has no durable store). Unconditional: the FIRST edit to a
   *  fresh working lab persists it too (and records it as the last-active lab),
   *  so a browser refresh keeps the user's additions instead of reloading the
   *  seed. Debounced so a burst of edits/drags coalesces into one save. */
  scheduleAutosave() {
    if (this.transportKind !== "ws") return;
    if (!this.autoSaveEnabled) return;
    if (this.autosaveTimer) clearTimeout(this.autosaveTimer);
    this.autosaveTimer = setTimeout(() => {
      this.autosaveTimer = null;
      void this.saveLab(false);
    }, 1200);
  }

  setAutoSaveEnabled(value: boolean) {
    this.autoSaveEnabled = value;
    try {
      localStorage.setItem(AUTOSAVE_STORAGE_KEY, String(value));
    } catch {
      /* localStorage may be unavailable (private mode) */
    }
    if (this.autosaveTimer) {
      clearTimeout(this.autosaveTimer);
      this.autosaveTimer = null;
    }
  }

  toggleAutoSaveEnabled() {
    this.setAutoSaveEnabled(!this.autoSaveEnabled);
  }

  // ---- live-capture console tabs (feature 1) ----

  private ensureCaptureSession(linkId: number) {
    if (this.transportKind !== "ws" || this.captureSessions.has(linkId)) return;
    let session: CaptureSession;
    const transport = new CaptureTransport(linkId, {
      onOpen: () => {
        // Every reconnect starts at a new SHB, so parser-local index/tRel state
        // must reset. The Lens ring intentionally does not reset.
        session.parser = new PcapngParser();
        for (const subscriber of [...session.subscribers]) subscriber.onReset?.();
      },
      onData: (bytes) => {
        const packets = session.parser.push(bytes);
        if (packets.length === 0) return;
        this.appendCapturePackets(linkId, packets);
        const firstSeq = consoleUiStore.advanceCaptureDelivery(linkId, packets.length);
        this.pushLensPackets(linkId, packets, firstSeq);
        for (const subscriber of [...session.subscribers]) subscriber.onPackets(packets);
      },
      onError: () => {
        for (const subscriber of [...session.subscribers]) subscriber.onUnavailable?.();
      },
      onClose: () => {
        for (const subscriber of [...session.subscribers]) subscriber.onUnavailable?.();
      },
    });
    session = { transport, parser: new PcapngParser(), subscribers: new Set() };
    this.captureSessions.set(linkId, session);
    transport.connect();
  }

  private closeCaptureSession(linkId: number) {
    const session = this.captureSessions.get(linkId);
    if (!session) return;
    this.captureSessions.delete(linkId);
    session.transport.disconnect();
  }

  private closeAllCaptureSessions() {
    for (const linkId of [...this.captureSessions.keys()]) this.closeCaptureSession(linkId);
  }

  subscribeCapture(linkId: number, subscriber: CaptureSubscriber): () => void {
    const session = this.captureSessions.get(linkId);
    if (!session) return () => {};
    session.subscribers.add(subscriber);
    return () => session.subscribers.delete(subscriber);
  }

  retryCapture(linkId: number) {
    this.captureSessions.get(linkId)?.transport.retryNow();
  }

  private lensAttribution(linkId: number): LensAttribution {
    const link = this.lab.links.find((item) => item.id === linkId);
    const stats = this.linkStats[linkId];
    return {
      endpoints: link?.endpoints.map((endpoint) => ({ node: endpoint.node })) ?? [],
      epAttrib: stats?.epAttrib,
      nodeName: (nodeId) => this.lab.nodes.find((node) => node.id === nodeId)?.name ?? `#${nodeId}`,
    };
  }

  private pushLensPackets(linkId: number, packets: readonly ParsedPacket[], firstSeq: number) {
    const current = this.lensEvents[linkId] ?? [];
    this.lensEvents = {
      ...this.lensEvents,
      [linkId]: appendLensEvents(current, packets, firstSeq, this.lensAttribution(linkId)),
    };
  }

  openLens(linkId: number) {
    this.openCapture(linkId);
    if (!this.openLensTabs.includes(linkId)) this.openLensTabs = [...this.openLensTabs, linkId];
    consoleUiStore.setFocused({ kind: "lens", link: linkId });
  }

  closeLens(linkId: number) {
    this.openLensTabs = this.openLensTabs.filter((id) => id !== linkId);
    const ref = { kind: "lens" as const, link: linkId };
    if (consoleUiStore.pinned?.kind === "lens" && consoleUiStore.pinned.link === linkId) {
      consoleUiStore.setPinned(null);
    }
    if (consoleUiStore.tiles.some((tile) => tile.kind === "lens" && tile.link === linkId)) {
      consoleUiStore.toggleTile(ref);
    }
    if (consoleUiStore.focused?.kind === "lens" && consoleUiStore.focused.link === linkId) {
      const capture = this.openCaptureTabs.includes(linkId);
      consoleUiStore.setFocused(capture ? { kind: "capture", link: linkId } : null);
    }
  }

  openCapture(linkId: number) {
    const link = this.lab.links.find((l) => l.id === linkId);
    if (link) {
      // Mark the link for capture so the next lab start bridges it. If it's not
      // already enabled and the lab is running natively, traffic won't flow
      // until restart — the tab surfaces that hint itself.
      link.capture = { enabled: true, mode: "live" };
    }
    const alreadyOpen = this.openCaptureTabs.includes(linkId);
    if (!alreadyOpen) {
      this.openCaptureTabs = [...this.openCaptureTabs, linkId];
      // Fresh recording and Lens ring for a newly opened capture session.
      this.captureBuffers.delete(linkId);
      const { [linkId]: _oldLens, ...restLens } = this.lensEvents;
      this.lensEvents = restLens;
    }
    // Ask the supervisor to start teeing this link (idempotent; harmless when
    // the lab is stopped — it'll bridge on next start).
    if (this.lab.id) void this.client.captureStart(this.lab.id, linkId).catch(() => {});
    this.ensureCaptureSession(linkId);
    this.scheduleAutosave();
  }

  // ---- capture recording (for "Save .pcapng" download) ----
  // Wireshark's live `-i TCP@host:port` attach works, but requires wireshark on
  // PATH; a downloaded .pcapng opens in Wireshark by double-click with zero
  // setup. We buffer the PARSED packets (frame + absolute ts) — NOT the raw
  // byte stream — because the live /capture WS spans reconnects (each restarts
  // its own SHB) and can drop mid-block, so concatenated raw bytes produce a
  // file Wireshark rejects. The download re-serializes one clean pcapng section
  // from these packets (see encodePcapng). Plain Map (not reactive).
  private captureBuffers = new Map<number, { packets: CapturedPacket[]; bytes: number }>();
  private static CAPTURE_BUF_CAP = 64 * 1024 * 1024;
  /** Reactive byte counter so the download button can show size / enable state. */
  captureRecorded = $state<Record<number, number>>({});

  /** Append PARSED packets from a link's live capture stream (called by
   *  CaptureTerm with the parser's output). Each packet's `data` is already a
   *  copy (PcapngParser slices it). Caps total buffered bytes. */
  appendCapturePackets(linkId: number, packets: CapturedPacket[]) {
    if (packets.length === 0) return;
    let b = this.captureBuffers.get(linkId);
    if (!b) {
      b = { packets: [], bytes: 0 };
      this.captureBuffers.set(linkId, b);
    }
    if (b.bytes >= LabStore.CAPTURE_BUF_CAP) return;
    for (const p of packets) {
      b.packets.push({ data: p.data, tsMicros: p.tsMicros });
      b.bytes += p.data.length;
    }
    this.captureRecorded = { ...this.captureRecorded, [linkId]: b.bytes };
  }

  /** Trigger a browser download of the buffered pcapng for a link. The file
   *  opens directly in Wireshark (no PATH/command needed). */
  downloadCapture(linkId: number) {
    const b = this.captureBuffers.get(linkId);
    if (!b || b.packets.length === 0) {
      this.pushLog("warn", "no packets captured yet — wait for traffic on this link");
      return;
    }
    // Rebuild ONE clean pcapng section from the parsed packets (never a raw
    // concat of the reconnect-spanning stream, which Wireshark rejects).
    const bytes = encodePcapng(b.packets);
    const blob = new Blob([bytes as BlobPart], { type: "application/vnd.tcpdump.pcap" });
    const link = this.lab.links.find((l) => l.id === linkId);
    const ep = link?.endpoints ?? [];
    const nm = (i: number) => this.lab.nodes.find((n) => n.id === ep[i]?.node)?.name ?? `n${ep[i]?.node}`;
    const base = link ? `${this.lab.name}-${nm(0)}-${nm(1)}` : `capture-link${linkId}`;
    const fname = `${base}.pcapng`.replace(/[^\w.\-]+/g, "_");
    const url = URL.createObjectURL(blob);
    try {
      const a = document.createElement("a");
      a.href = url;
      a.download = fname;
      document.body.appendChild(a);
      a.click();
      a.remove();
    } finally {
      setTimeout(() => URL.revokeObjectURL(url), 4000);
    }
    this.pushLog("info", `saved ${fname} (${(b.bytes / 1024).toFixed(0)} KiB)`);
  }

  closeCapture(linkId: number) {
    this.openCaptureTabs = this.openCaptureTabs.filter((id) => id !== linkId);
    this.openLensTabs = this.openLensTabs.filter((id) => id !== linkId);
    this.closeCaptureSession(linkId);
    this.captureBuffers.delete(linkId);
    const { [linkId]: _dropLens, ...restLens } = this.lensEvents;
    this.lensEvents = restLens;
    if (this.captureRecorded[linkId] !== undefined) {
      const { [linkId]: _drop, ...rest } = this.captureRecorded;
      this.captureRecorded = rest;
    }
    // Withdraw the doc-level capture intent too (openCapture set it): the
    // supervisor auto-arms every capture-enabled doc link on lab start, so
    // leaving the flag behind would keep re-arming a capture whose tab the
    // user closed. Symmetric with openCapture; autosaved like any doc edit.
    const link = this.lab.links.find((l) => l.id === linkId);
    if (link?.capture?.enabled) {
      link.capture = { ...link.capture, enabled: false };
      this.scheduleAutosave();
    }
    if (this.lab.id) void this.client.captureStop(this.lab.id, linkId).catch(() => {});
  }

  async startLab() {
    if (this.lab.nodes.length === 0) {
      this.enqueueToast({ severity: "info", message: "Lab has no nodes to start" });
      return;
    }
    // Pre-flight what the supervisor would reject anyway, with a message that
    // names the node instead of an opaque image_not_found.
    const missing = this.lab.nodes.find(
      (n) => n.kind === "iol" && (!n.image || !this.images.some((i) => i.id === n.image!.id))
    );
    if (missing) {
      this.lastError = `${missing.name} has no registered image — assign one (Images)`;
      this.pushLog("error", this.lastError);
      return;
    }
    let result: LabStartResult | undefined;
    const ok = await this.guarded("start lab", async () => {
      result = await this.client.labStart(this.lab.id);
    });
    if (ok && result) {
      const failedCount = result.failed?.length ?? 0;
      if (failedCount > 0) {
        this.enqueueToast({
          severity: "error",
          message: `Lab start incomplete — ${failedCount} of ${this.lab.nodes.length} nodes failed`,
        });
      } else if (result.started.length > 0) {
        const count = result.started.length;
        this.enqueueToast({
          severity: "success",
          message: `Lab started — ${count} ${count === 1 ? "node" : "nodes"} running`,
        });
      }
    }
    this.reportStartFailures("start lab", result);
  }

  /** Force-clean orphaned runtime state: ask the supervisor to stop every
   *  tracked node + all relays/bridges/captures (regardless of labId), then
   *  reset local runtime state so the GUI matches. Use when nodes still show
   *  running or host CPU stays high after a normal stop. */
  async forceClean() {
    let reapedCount = 0;
    const ok = await this.guarded("force clean", async () => {
      const res = await this.client.labReap();
      reapedCount = res?.reaped ?? 0;
      // Reset local runtime view: everything is stopped now.
      const states: Record<number, NodeState> = {};
      for (const n of this.lab.nodes) states[n.id] = "stopped";
      this.nodeStates = states;
      this.openConsoleTabs = [];
      this.activeConsoleTab = null;
      this.closeAllCaptureSessions();
      this.openCaptureTabs = [];
      this.openLensTabs = [];
      this.capturePorts = {};
      this.captureBuffers.clear();
      this.captureRecorded = {};
      this.lensEvents = {};
      this.linkStats = {};
      this.linkFaults = {};
      this.pushLog("info", `force clean: stopped ${reapedCount} node(s) and all relays`);
    });
    if (ok) {
      this.enqueueToast({
        severity: "danger",
        message: `Force clean complete — stopped ${reapedCount} ${reapedCount === 1 ? "node" : "nodes"} and cleared relays`,
      });
    }
  }

  async stopLab() {
    const nodeCount = this.lab.nodes.length;
    this.labStopPending = true;
    let ok = false;
    try {
      ok = await this.guarded("stop lab", async () => {
        await this.client.labStop(this.lab.id);
        this.openConsoleTabs = [];
        this.activeConsoleTab = null;
      });
    } finally {
      this.labStopPending = false;
    }
    if (ok) {
      this.labStopSuppressedUntil = Date.now() + 1_500;
      this.enqueueToast({
        severity: "danger",
        message: nodeCount === 0
          ? "Lab stopped — no nodes were running"
          : `Lab stopped — all ${nodeCount} ${nodeCount === 1 ? "node" : "nodes"} stopped`,
      });
    }
  }

  async setLinkFault(linkId: number, fault: LinkFault | null, afterSec?: number, forSec?: number) {
    const ok = await this.guarded(`set fault on link ${linkId}`, () =>
      this.client.linkSetFault(this.lab.id, linkId, fault, afterSec, forSec)
    );
    if (!ok) return;
    if (fault) {
      this.enqueueToast({
        key: `link-fault:${linkId}`,
        severity: afterSec && afterSec > 0 ? "info" : "warning",
        message: afterSec && afterSec > 0
          ? `Link ${linkId} impairment scheduled`
          : `Link ${linkId} impairment active`,
      });
    } else {
      this.enqueueToast({
        key: `link-fault:${linkId}`,
        severity: "success",
        message: `Link ${linkId} restored`,
      });
    }
  }

  /** Takes a link administratively down until explicitly resumed — a
   *  one-click "unplug the cable" for testing STP convergence, DHCP
   *  renewal, etc. Pair with resumeLink() to "replug" it. */
  async terminateLink(linkId: number) {
    await this.setLinkFault(linkId, { down: true });
  }

  /** Clears an administrative-down fault set by terminateLink() (or the
   *  "Administratively down" checkbox in the fault dialog) — the manual
   *  "replug the cable" counterpart. */
  async resumeLink(linkId: number) {
    await this.setLinkFault(linkId, null);
  }

  /** True when any link touching this node currently has an active fault
   *  (admin-down or egress impairment) — drives the red indicator on the
   *  node face so an impacted link is visible even when the edge itself
   *  is off-screen or hard to notice at the current zoom level. */
  nodeHasFault(nodeId: number): boolean {
    for (const link of this.lab.links) {
      if (link.endpoints.some((ep) => ep.node === nodeId) && this.linkFaults[link.id]?.active) {
        return true;
      }
    }
    return false;
  }

  /** Deletes saved configs/state for every node in the lab. Destructive —
   *  callers (UI) must confirm with the user before invoking this. Under the
   *  mock transport lab.wipe isn't implemented, so a rejection here is logged
   *  and swallowed rather than surfaced as a hard error. */
  async wipeLab() {
    let wipedCount = 0;
    const ok = await this.guarded("wipe lab", async () => {
      const result = await this.client.labWipe(this.lab.id, null);
      wipedCount = result.wiped.length;
    });
    if (ok) {
      this.enqueueToast({
        severity: "danger",
        message: `Lab wiped — saved state cleared for ${wipedCount} ${wipedCount === 1 ? "node" : "nodes"}`,
      });
    }
  }

  /** Wipe saved configs/state for a single node. Destructive — the caller (UI)
   *  must confirm with the user first. Mirrors wipeLab: mock lab.wipe isn't
   *  implemented, so a rejection is logged + swallowed via guarded(). */
  async wipeNode(nodeId: number) {
    const nodeName = this.resolveNodeName(nodeId);
    let wiped = false;
    // RPC-ack only (no driving node.state) → release when the awaited call
    // settles (guarded never rethrows, so the finally always runs).
    if (!this.acquireNodeLock(nodeId, "wiping")) return;
    try {
      const ok = await this.guarded(`wipe node ${nodeId}`, async () => {
        const result = await this.client.labWipe(this.lab.id, [nodeId]);
        wiped = result.wiped.includes(nodeId);
      });
      if (ok && wiped) {
        this.enqueueToast({
          severity: "danger",
          message: `${nodeName} wiped — saved state cleared`,
        });
      }
    } finally {
      this.releaseNodeLock(nodeId);
    }
  }

  async startNode(nodeId: number) {
    const nodeName = this.resolveNodeName(nodeId);
    // Lock the node until its next node.state event (released in handleEvent) —
    // that's the success path, left exactly as before. But when the RPC itself
    // is REJECTED (e.g. a fabric-prep error that never reaches this node's own
    // spawn/state-machine step, so no node.state event for it will ever come),
    // nothing would otherwise clear the lock until the 60s safety timeout —
    // release explicitly on that path only, same as wipeNode/restartNode below.
    if (!this.acquireNodeLock(nodeId, "starting")) return;
    let rpcRejected = false;
    let result: LabStartResult | undefined;
    const ok = await this.guarded(`start node ${nodeId}`, async () => {
      try {
        result = await this.client.nodeStart(this.lab.id, nodeId);
      } catch (e) {
        rpcRejected = true;
        throw e;
      }
    });
    if (rpcRejected) this.releaseNodeLock(nodeId);
    if (ok && result?.started.some((started) => started.node === nodeId) &&
      !result.failed?.some((failure) => failure.node === nodeId)) {
      this.enqueueToast({ severity: "success", message: `${nodeName} started` });
    }
    this.reportStartFailures(`start node ${nodeId}`, result);
    if (result?.failed?.some((failure) => failure.node === nodeId)) this.releaseNodeLock(nodeId);
  }

  private reportStartFailures(action: string, result: LabStartResult | undefined) {
    if (!result?.failed?.length) return;
    const details = result.failed.map((failure) => `node ${failure.node}: ${failure.error}`).join("; ");
    this.lastError = `${action}: ${result.failed.length} node(s) failed — ${details}`;
    this.pushLog("error", this.lastError);
  }

  async stopNode(nodeId: number) {
    const nodeName = this.resolveNodeName(nodeId);
    if (!this.acquireNodeLock(nodeId, "stopping")) return;
    const ok = await this.guarded(`stop node ${nodeId}`, async () => {
      await this.client.nodeStop(this.lab.id, nodeId);
      this.openConsoleTabs = this.openConsoleTabs.filter((id) => id !== nodeId);
      if (this.activeConsoleTab === nodeId) {
        this.activeConsoleTab = this.openConsoleTabs[0] ?? null;
      }
    });
    if (ok) this.enqueueToast({ severity: "danger", message: `${nodeName} stopped` });
  }

  async restartNode(nodeId: number) {
    const nodeName = this.resolveNodeName(nodeId);
    if (!this.acquireNodeLock(nodeId, "restarting", { holdUntilSettled: true })) return;
    let result: LabStartResult | undefined;
    let ok = false;
    try {
      ok = await this.guarded(`restart node ${nodeId}`, async () => {
        result = await this.client.nodeRestart(this.lab.id, nodeId);
      });
    } finally {
      this.releaseNodeLock(nodeId);
    }
    if (ok && result?.started.some((started) => started.node === nodeId) &&
      !result.failed?.some((failure) => failure.node === nodeId)) {
      this.enqueueToast({ severity: "success", message: `${nodeName} restarted` });
    }
    this.reportStartFailures(`restart node ${nodeId}`, result);
  }

  // ---- per-node action lock (WS1) ----

  /** Acquire the per-node action lock. Returns false if the node is already
   *  locked (caller must no-op). Arms a 60s safety timeout so a lost driving
   *  event can't wedge the node permanently. */
  private acquireNodeLock(nodeId: number, action: string, options?: { holdUntilSettled?: boolean }): boolean {
    if (this.nodeLocks[nodeId]) return false;
    this.nodeLocks = { ...this.nodeLocks, [nodeId]: { action, startedAt: Date.now(), ...options } };
    if (this.nodeLockTimers[nodeId]) clearTimeout(this.nodeLockTimers[nodeId]);
    this.nodeLockTimers[nodeId] = setTimeout(() => {
      this.pushLog("warn", `node ${nodeId}: ${action} did not settle in 60s — releasing lock`, nodeId);
      this.releaseNodeLock(nodeId);
    }, 60_000);
    return true;
  }

  /** Release the per-node action lock and clear its safety timeout. Idempotent. */
  private releaseNodeLock(nodeId: number) {
    if (this.nodeLockTimers[nodeId]) {
      clearTimeout(this.nodeLockTimers[nodeId]);
      delete this.nodeLockTimers[nodeId];
    }
    if (this.nodeLocks[nodeId]) {
      const { [nodeId]: _drop, ...rest } = this.nodeLocks;
      this.nodeLocks = rest;
    }
  }

  /** Run a supervisor action; surface failure in the top bar + log instead of
   *  letting the rejection vanish into an unhandled promise. */
  private async guarded(what: string, fn: () => Promise<unknown>): Promise<boolean> {
    try {
      await fn();
      this.lastError = null;
      return true;
    } catch (e) {
      this.lastError = `${what} failed: ${(e as Error).message}`;
      this.pushLog("error", this.lastError);
      return false;
    }
  }

  openConsole(nodeId: number) {
    if (!this.openConsoleTabs.includes(nodeId)) {
      this.openConsoleTabs = [...this.openConsoleTabs, nodeId];
    }
    this.activeConsoleTab = nodeId;
  }

  /** Open a node's console in the OS telnet client via the telnet:// scheme,
   *  WITHOUT opening a web tab. Uses a transient anchor click so the OS handler
   *  is invoked without navigating the app: a `location=`/`window.open(_,"_self")`
   *  navigation unloads the page and drops the supervisor WebSocket, which is
   *  what previously broke every *other* console once one was opened natively. */
  openNativeConsole(nodeId: number) {
    const port = this.consolePorts[nodeId];
    const name = this.lab.nodes.find((n) => n.id === nodeId)?.name ?? `#${nodeId}`;
    if (!port) {
      this.pushLog("warn", `${name}: console port not assigned yet`, nodeId);
      return;
    }
    const host = location.hostname || "localhost";
    const url = `telnet://${host}:${port}`;
    try {
      const a = document.createElement("a");
      a.href = url;
      a.style.display = "none";
      document.body.appendChild(a);
      a.click();
      a.remove();
    } catch {
      /* no telnet:// handler registered — non-fatal; the log shows the address */
    }
    this.pushLog("info", `native console for ${name} → ${url}`, nodeId);
  }

  /** Open a node's console honouring the global console mode (consoleUiStore):
   *  a web tab in "web" mode, the native telnet client in "native" mode. */
  openConsoleByMode(nodeId: number) {
    if (consoleUiStore.consoleMode === "native") this.openNativeConsole(nodeId);
    else this.openConsole(nodeId);
  }

  /** "Console all" (left pane): open a console for every currently-running node,
   *  honouring the global console mode. In native mode this fires one
   *  telnet:// anchor click per node — browsers throttle a burst of
   *  programmatic external-protocol launches from a single click handler, so
   *  the clicks are staggered ~300ms apart instead of firing synchronously. */
  openAllConsoles() {
    const ids = this.lab.nodes.filter((n) => this.nodeStates[n.id] === "running").map((n) => n.id);
    ids.forEach((id, i) => {
      if (i === 0) this.openConsoleByMode(id);
      else setTimeout(() => this.openConsoleByMode(id), i * 300);
    });
  }

  closeConsole(nodeId: number) {
    this.openConsoleTabs = this.openConsoleTabs.filter((id) => id !== nodeId);
    if (this.activeConsoleTab === nodeId) {
      this.activeConsoleTab = this.openConsoleTabs[0] ?? null;
    }
  }

  /** Close the whole console dock: every node console tab and every capture
   *  tab (captures stop via the same per-tab close semantics). The dock hides
   *  itself once no tabs remain (App.svelte's showConsole derivation). */
  closeAllConsoles() {
    this.openConsoleTabs = [];
    this.activeConsoleTab = null;
    for (const linkId of [...this.openCaptureTabs]) this.closeCapture(linkId);
  }

  /** Add a node locally AND register it with the supervisor's loaded lab
   *  (node.add) so it can start without a page refresh — the supervisor only
   *  learns topology at lab.load otherwise, and a freshly dropped node was
   *  "unknown node" to node.start (NAT was the visible victim: it's the kind
   *  you drop and start mid-session). The returned promise resolves once the
   *  supervisor ack'd (with the console port recorded); callers that want to
   *  auto-start the node await it first. */
  async addNode(node: LabNode): Promise<void> {
    this.lab.nodes = [...this.lab.nodes, node];
    this.nodeStates = { ...this.nodeStates, [node.id]: "stopped" };
    this.scheduleAutosave();
    await this.guarded(`add node ${node.name}`, async () => {
      const res = await this.client.nodeAdd(this.lab.id, $state.snapshot(node) as LabNode);
      if (res?.consolePort) {
        this.consolePorts = { ...this.consolePorts, [node.id]: res.consolePort };
      }
    });
  }

  removeNode(nodeId: number) {
    this.lab.nodes = this.lab.nodes.filter((n) => n.id !== nodeId);
    this.lab.links = this.lab.links.filter(
      (l) => !l.endpoints.some((e) => e.node === nodeId)
    );
    if (this.selectedNodeId === nodeId) this.selectedNodeId = null;
    if (this.inspectorNodeId === nodeId) this.inspectorNodeId = null;
    this.scheduleAutosave();
    // Mirror into the loaded lab (stops the node + drops its links there too).
    // Fire-and-forget: the local doc is authoritative for the GUI either way.
    void this.client.nodeRemove(this.lab.id, nodeId).catch(() => {});
  }

  /** Clone a node: fresh id, same config, offset +40/+40, NO links. Names get a
   *  numeric suffix — "R1" → "R1-2", "R1-2" → "R1-3" — incremented until unique.
   *  Returns the new node id (selected by the caller), or null if src missing. */
  duplicateNode(nodeId: number): number | null {
    const src = this.lab.nodes.find((n) => n.id === nodeId);
    if (!src) return null;
    // Duplication does not touch the SOURCE node's runtime state at all (it
    // stays whatever it was — running/stopped/etc.), so it must show no
    // lock/progress effect on it. The clone is a brand-new node added via the
    // normal addNode path (which itself does not lock), so no lock is needed
    // on either node here.
    const id = this.nextNodeId();
    const clone: LabNode = {
      ...structuredClone($state.snapshot(src)),
      id,
      name: this.uniqueDuplicateName(src.name),
      x: src.x + 40,
      y: src.y + 40,
    };
    void this.addNode(clone);
    return id;
  }

  /** Derive a unique duplicate name. Strips a trailing "-<n>" (or appends "-2"
   *  when absent), then bumps the counter until the name is free in the lab. */
  private uniqueDuplicateName(name: string): string {
    const m = name.match(/^(.*?)-(\d+)$/);
    const base = m ? m[1] : name;
    let n = m ? parseInt(m[2], 10) + 1 : 2;
    const taken = new Set(this.lab.nodes.map((x) => x.name));
    let candidate = `${base}-${n}`;
    while (taken.has(candidate)) {
      n += 1;
      candidate = `${base}-${n}`;
    }
    return candidate;
  }

  addLink(link: LabLink) {
    this.lab.links = [...this.lab.links, link];
    this.scheduleAutosave();
  }

  removeLink(linkId: number) {
    this.lab.links = this.lab.links.filter((l) => l.id !== linkId);
    if (this.selectedLinkId === linkId) this.selectedLinkId = null;
    this.scheduleAutosave();
  }

  /** Called after a node-drag settles to persist positions (autosaved). */
  notifyTopologyChanged() {
    this.scheduleAutosave();
  }

  // ---- canvas annotations (Excalidraw-style) ----

  /** Add a new annotation and return its id (so the caller can arm inline edit). */
  addAnnotation(anno: Annotation): string {
    this.lab.annotations = [...(this.lab.annotations ?? []), anno];
    this.scheduleAutosave();
    return anno.id;
  }

  /** Shallow-merge a patch into one annotation, preserving its discriminant. */
  updateAnnotation(id: string, patch: Partial<Annotation>) {
    const list = this.lab.annotations;
    if (!list) return;
    const idx = list.findIndex((a) => a.id === id);
    if (idx < 0) return;
    // Merge in place; the caller only ever patches fields valid for the type.
    list[idx] = { ...list[idx], ...patch } as Annotation;
    this.scheduleAutosave();
  }

  removeAnnotation(id: string) {
    if (!this.lab.annotations) return;
    this.lab.annotations = this.lab.annotations.filter((a) => a.id !== id);
    if (this.selectedAnnotationId === id) this.selectedAnnotationId = null;
    this.scheduleAutosave();
  }

  newAnnotationId(): string {
    return uuid();
  }

  /** Clone an annotation: fresh id, same content, offset +40/+40 (both
   *  endpoints for a line) so the copy doesn't sit exactly on top of the
   *  source. Mirrors duplicateNode. Returns the new annotation's id, or null
   *  if src is missing. */
  duplicateAnnotation(id: string): string | null {
    const src = this.lab.annotations?.find((a) => a.id === id);
    if (!src) return null;
    const clone = { ...structuredClone($state.snapshot(src)), id: this.newAnnotationId() } as Annotation;
    if (clone.type === "line") {
      clone.x1 += 40;
      clone.y1 += 40;
      clone.x2 += 40;
      clone.y2 += 40;
    } else {
      clone.x += 40;
      clone.y += 40;
    }
    return this.addAnnotation(clone);
  }

  // ---- lab tasks ----

  /** Replace the lab task text (from the Tasks pane editor / checkbox toggles). */
  setTasks(text: string) {
    this.lab.tasks = text;
    this.scheduleAutosave();
  }

  nextNodeId(): number {
    return this.lab.nodes.length === 0 ? 0 : Math.max(...this.lab.nodes.map((n) => n.id)) + 1;
  }

  nextLinkId(): number {
    return this.lab.links.length === 0 ? 0 : Math.max(...this.lab.links.map((l) => l.id)) + 1;
  }

  async setNodeImage(nodeId: number, imageId: string) {
    const node = this.lab.nodes.find((n) => n.id === nodeId);
    const img = this.images.find((i) => i.id === imageId);
    if (!node || !img) return;
    // Local doc first, and persist it: the user's choice must stick even when
    // the supervisor sync needs the fallback below.
    node.image = { id: img.id, filename: img.filename, class: img.class };
    this.scheduleAutosave();
    try {
      await this.client.nodeSetImage(this.lab.id, nodeId, imageId);
    } catch {
      // Typically "unknown lab": the doc never loaded (e.g. it predates the
      // image upload and lab.load failed). Now that it carries a valid image
      // the full doc can load — re-send it when nothing is running.
      await this.resyncAfterImageChange();
    }
  }

  replaceImageEverywhere(fromId: string, toId: string) {
    const img = this.images.find((i) => i.id === toId);
    if (!img) return;
    let failed = false;
    const applied: Promise<unknown>[] = [];
    for (const node of this.lab.nodes) {
      if (node.image?.id === fromId) {
        node.image = { id: img.id, filename: img.filename, class: img.class };
        applied.push(this.client.nodeSetImage(this.lab.id, node.id, img.id).catch(() => (failed = true)));
      }
    }
    if (applied.length === 0) return;
    this.scheduleAutosave();
    void Promise.all(applied).then(() => {
      if (failed) return this.resyncAfterImageChange();
    });
  }

  /** Fallback sync when node.setImage was rejected (lab not loaded at the
   *  supervisor): re-send the whole doc, unless nodes are running. */
  private async resyncAfterImageChange() {
    const busy = this.lab.nodes.some(
      (n) => this.nodeStates[n.id] === "running" || this.nodeStates[n.id] === "starting"
    );
    if (busy) {
      this.lastError = "image change saved locally — restart the lab to apply it";
      this.pushLog("warn", this.lastError);
      return;
    }
    try {
      await this.loadLab(this.lab);
    } catch (e) {
      this.lastError = `apply image change: ${(e as Error).message}`;
      this.pushLog("error", this.lastError);
    }
  }

  setNodeIcon(nodeId: number, iconKey: string) {
    const node = this.lab.nodes.find((n) => n.id === nodeId);
    if (node) node.icon = iconKey;
  }

  get selectedNode(): LabNode | null {
    return this.lab.nodes.find((n) => n.id === this.selectedNodeId) ?? null;
  }

  /** The node the Inspector pane is showing — see inspectorNodeId. */
  get inspectorNode(): LabNode | null {
    return this.lab.nodes.find((n) => n.id === this.inspectorNodeId) ?? null;
  }

  // ---- per-node / lab config extraction into the doc (feature 4) ----

  /** Extract NVRAM startup-config for one node into node.startupConfig. Runs on
   *  the node's SAVED config, so the user must `write memory` first. */
  async saveNodeConfig(nodeId: number) {
    const nodeName = this.resolveNodeName(nodeId);
    const ok = await this.guarded(`save config for node ${nodeId}`, async () => {
      const res = await this.client.configExtract(this.lab.id, [nodeId]);
      this.applyExtractedConfigs(res.configs);
    });
    this.scheduleAutosave();
    if (ok) this.enqueueToast({ severity: "success", message: `Config saved — ${nodeName}` });
  }

  /** Extract startup-configs for every running IOL node into the doc. */
  async saveAllConfigs() {
    const targets = this.lab.nodes
      .filter((n) => n.kind === "iol" && this.nodeStates[n.id] === "running")
      .map((n) => n.id);
    if (targets.length === 0) {
      this.pushLog("warn", "no running IOL nodes to extract config from");
      return;
    }
    const ok = await this.guarded("save configs", async () => {
      const res = await this.client.configExtract(this.lab.id, targets);
      this.applyExtractedConfigs(res.configs);
    });
    this.scheduleAutosave();
    if (ok) {
      this.enqueueToast({
        severity: "success",
        message: `Configs saved — ${targets.length} ${targets.length === 1 ? "node" : "nodes"}`,
      });
    }
  }

  private applyExtractedConfigs(configs: { node: number; startupConfig: string }[]) {
    for (const c of configs ?? []) {
      const node = this.lab.nodes.find((n) => n.id === c.node);
      if (node) node.startupConfig = c.startupConfig;
    }
  }

  /** Extract NVRAM startup-config for one node (same as saveNodeConfig) and
   *  trigger a browser download of it as a .txt file — for pulling a config
   *  out of the lab entirely, not just into the doc. */
  async exportNodeConfig(nodeId: number) {
    const nodeName = this.resolveNodeName(nodeId);
    const ok = await this.guarded(`export config for node ${nodeId}`, async () => {
      const res = await this.client.configExtract(this.lab.id, [nodeId]);
      this.applyExtractedConfigs(res.configs);
    });
    this.scheduleAutosave();
    if (!ok) return;
    const node = this.lab.nodes.find((n) => n.id === nodeId);
    const text = node?.startupConfig ?? "";
    const blob = new Blob([text], { type: "text/plain" });
    const fname = `${nodeName}-startup-config.txt`.replace(/[^\w.\-]+/g, "_");
    const url = URL.createObjectURL(blob);
    try {
      const a = document.createElement("a");
      a.href = url;
      a.download = fname;
      document.body.appendChild(a);
      a.click();
      a.remove();
    } finally {
      setTimeout(() => URL.revokeObjectURL(url), 4000);
    }
    this.enqueueToast({ severity: "success", message: `Config exported — ${nodeName}` });
  }

  /** Push a node's in-memory edits (config.pack, config.net, ram, ethernet,
   *  serial, ...) to the supervisor's loaded lab. The Inspector's
   *  update*() handlers (updatePack/updateNet/updateRam/updateEthernet/
   *  updateSerial/...) only mutate the LOCAL doc — the supervisor keeps its
   *  own copy from the last lab.load/node.add and never re-reads the
   *  frontend's state on its own, so without this any of those edits was
   *  pure UI with no effect on what actually starts. Re-sends the whole doc
   *  via lab.load.
   *
   *  Most fields (pack, net, ram, name, icon, startupConfig, ...) aren't
   *  part of the supervisor's sameNodeShape check, so the ADOPT path always
   *  takes over server-side for them — it swaps in the new doc's fields in
   *  place without touching any already-running node elsewhere in the lab
   *  (see tryAdoptLoad). Ethernet/serial adapter COUNTS are part of
   *  sameNodeShape, though: changing them is a real topology change, so
   *  adoption is refused and the supervisor instead runs its normal
   *  teardown-and-reload — which stops every node in the WHOLE lab, not
   *  just this one. The two cases get distinctly different log messages
   *  below so that isn't a silent surprise. */
  async applyNodeConfig(nodeId: number) {
    let adopted = true;
    await this.guarded(`apply config for node ${nodeId}`, async () => {
      const res = await this.client.labLoad($state.snapshot(this.lab) as LabDocument);
      adopted = !!res.adopted;
      if (!adopted) {
        // Adapter-count change (or some other real topology change): the
        // supervisor already ran the full teardown-and-reload server-side,
        // stopping every node in the lab. Resync local state to match
        // rather than let it drift from what's actually running now.
        const status = await this.client.status();
        const states: Record<number, NodeState> = {};
        for (const n of status.nodes ?? []) states[n.id] = n.state;
        for (const n of this.lab.nodes) if (!(n.id in states)) states[n.id] = "stopped";
        this.nodeStates = states;
      }
    });
    this.scheduleAutosave();
    const running = this.nodeStates[nodeId] === "running" || this.nodeStates[nodeId] === "starting";
    const name = this.lab.nodes.find((n) => n.id === nodeId)?.name ?? `#${nodeId}`;
    if (!adopted) {
      this.pushLog(
        "warn",
        `${name}: adapter count changed — this reloaded the WHOLE lab, every other node was stopped too`,
        nodeId
      );
      return;
    }
    this.pushLog(
      running ? "warn" : "info",
      running
        ? `${name}: changes saved — stop and restart the node to apply them`
        : `${name}: changes applied`,
      nodeId
    );
  }
}

export const labStore = new LabStore();
