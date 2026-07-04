// Central app state, Svelte 5 runes. One instance shared via module scope
// (simple singleton store — no context provider needed for a single-window app).
import { emptyLab, type Annotation, type LabDocument, type LabLink, type LabNode, type LibraryImage, type NodeState } from "./labTypes";
import { uuid } from "./uid";
import { SupervisorClient } from "./supervisor";
import { MockTransport } from "./mockTransport";
import { selectTransport } from "./transportSelect";
import { consoleUiStore } from "./consoleUiStore.svelte";
import type { SupervisorEvent } from "./protocol";

export type ProviderId = "vmware" | "wsl2" | "remote" | "qemu";
export type ProviderStatus = "unknown" | "connecting" | "connected" | "error";

export interface LogLine {
  ts: number;
  level: "debug" | "info" | "warn" | "error";
  message: string;
  node?: number;
}

class LabStore {
  lab = $state<LabDocument>(emptyLab("Demo Topology"));
  selectedNodeId = $state<number | null>(null);
  selectedLinkId = $state<number | null>(null);
  /** Selected canvas annotation (Excalidraw layer). Independent of node/link
   *  selection — annotations never open the node Inspector. */
  selectedAnnotationId = $state<string | null>(null);
  /** Live canvas viewport zoom, mirrored from SvelteFlow by CanvasInner. Used by
   *  annotation resize/drag grips to convert screen deltas to flow deltas. */
  canvasZoom = $state(1);
  /** Client→flow coordinate projector, wired by CanvasInner (useSvelteFlow's
   *  screenToFlowPosition). Used by the line-endpoint grips. */
  screenToFlow: ((clientX: number, clientY: number) => { x: number; y: number }) | null = null;
  nodeStates = $state<Record<number, NodeState>>({});
  /** Per-node action lock (WS1): while an action is in flight on a node, sibling
   *  actions on THAT node are no-ops and the UI shows a lock/progress state.
   *  Set at the entry of start/stop/wipe/duplicate; cleared when the driving
   *  event/promise settles (start/stop → node.state event; wipe/duplicate →
   *  awaited RPC). A 60s safety timeout releases it so a lost event can't wedge
   *  a node permanently. */
  nodeLocks = $state<Record<number, { action: string; startedAt: number } | null>>({});
  private nodeLockTimers: Record<number, ReturnType<typeof setTimeout>> = {};
  consolePorts = $state<Record<number, number>>({});
  images = $state<LibraryImage[]>([]);
  logs = $state<LogLine[]>([]);
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
  /** Tasks pane toggle (TopBar checklist). When on it takes precedence over the
   *  empty-selection auto-hide of the right pane. */
  showTasks = $state(false);
  /** Supervisor feature flags from the hello handshake (e.g. "natgw").
   *  Drives feature-gated palette entries. */
  features = $state<string[]>([]);
  /** Supervisor build version from the hello handshake (git describe, baked in
   *  via build-release.sh's -ldflags). Empty until connected; surfaced in the
   *  Palette host-monitor footer so staleness is visible at a glance. */
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
    this.seedDemoLab();
    // Drive glow decay: bump a coarse clock every second so FloatingEdge
    // re-evaluates staleness even when no new link.stats events arrive.
    setInterval(() => {
      this.nowTick = Date.now();
    }, 1000);
  }

  /** Small starter topology so the canvas isn't empty on first launch. */
  private seedDemoLab() {
    this.lab.nodes = [
      {
        id: 0,
        kind: "iol",
        name: "R1",
        x: 120,
        y: 140,
        ram: 1024,
        ethernet: 2,
        serial: 1,
        image: { id: "a1b2c3d4", filename: "i86bi_linux-adventerprisek9-ms.vm.bin", class: "l3" },
      },
      {
        id: 1,
        kind: "iol",
        name: "SW1",
        x: 420,
        y: 140,
        ram: 1024,
        ethernet: 4,
        serial: 0,
        image: { id: "b2c3d4e5", filename: "i86bi_linux_l2-adventerprisek9-ms.bin", class: "l2" },
      },
      {
        id: 2,
        kind: "vpcs",
        name: "PC1",
        x: 420,
        y: 340,
      },
    ];
    this.lab.links = [
      {
        id: 0,
        type: "p2p",
        endpoints: [
          { node: 0, interface: "e0/0" },
          { node: 1, interface: "e0/0" },
        ],
      },
      {
        id: 1,
        type: "p2p",
        endpoints: [
          { node: 1, interface: "e0/1" },
          { node: 2, interface: "eth0" },
        ],
      },
    ];
    for (const n of this.lab.nodes) this.nodeStates[n.id] = "stopped";
  }

  /** Only meaningful when transportKind === "mock"; null under a real ws transport. */
  get mockTransport(): MockTransport | null {
    return this.mock;
  }

  async connect() {
    this.providerStatus = "connecting";
    // Subscribe before the handshake settles so no push (node.state etc.)
    // arriving mid-connect is dropped.
    this.client.onEvent((evt) => this.handleEvent(evt));
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
      const { images } = await this.client.imageList();
      this.images = images;
      // Reload the last lab the user was working on (so a browser refresh keeps
      // their additions) instead of the throwaway seed. Falls back to the seed
      // when there's no remembered lab or it's gone from the store.
      const restored = await this.restoreLastActiveLab();
      if (!restored) {
        this.reconcileNodeImages();
        await this.loadLab(this.lab);
      }
    } catch (e) {
      this.providerStatus = "error";
      this.lastError = `connect failed: ${(e as Error).message}`;
      this.pushLog("error", `connect failed: ${(e as Error).message}`);
    }
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
        // Release a start/stop lock now that the node reached a real state
        // (WS1). wipe/duplicate locks are released on their own RPC settle, not
        // here — but they don't emit node.state, so this never fires for them.
        this.releaseNodeLock(evt.data.node);
        break;
      case "node.console":
        this.consolePorts = { ...this.consolePorts, [evt.data.node]: evt.data.consolePort };
        break;
      case "capture.started":
        this.capturePorts = { ...this.capturePorts, [evt.data.link]: evt.data.capturePort };
        break;
      case "capture.stopped": {
        const next = { ...this.capturePorts };
        delete next[evt.data.link];
        this.capturePorts = next;
        break;
      }
      case "link.stats":
        // Replace the whole record so $derived/$effect readers re-run; carry a
        // receive timestamp so FloatingEdge can expire stale glow.
        this.linkStats = {
          ...this.linkStats,
          [evt.data.link]: {
            fps: evt.data.fps,
            bps: evt.data.bps,
            ts: Date.now(),
            protos: evt.data.protos,
            protosDir: evt.data.protosDir,
            protosSubtypeDir: evt.data.protosSubtypeDir,
          },
        };
        break;
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
    this.lab = lab;
    // Remap image ids against this supervisor's registry (same as connect()).
    this.reconcileNodeImages();
    // Clear any lingering runtime/glow state from the previous lab.
    this.linkStats = {};
    this.openConsoleTabs = [];
    this.openCaptureTabs = [];
    this.capturePorts = {};
    this.captureBuffers.clear();
    this.captureRecorded = {};
    this.activeConsoleTab = null;
    const res = await this.client.labLoad(lab);
    const ports: Record<number, number> = {};
    const states: Record<number, NodeState> = {};
    for (const n of res.nodes) {
      ports[n.id] = n.consolePort;
    }
    for (const n of lab.nodes) {
      states[n.id] = "stopped";
    }
    this.consolePorts = ports;
    this.nodeStates = states;
  }

  // ---- durable lab-document store (feature 3) ----

  /** True once the current lab has been saved to the durable store this session. */
  get currentLabSaved(): boolean {
    return this.savedDocIds.has(this.lab.id);
  }

  /** localStorage key holding the id of the lab the user last had open, so a
   *  refresh reopens it rather than the throwaway seed. */
  private static LAST_LAB_KEY = "iolab.lastActiveLab";

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
  async saveLab(): Promise<boolean> {
    this.lab.modified = new Date().toISOString();
    let ok = false;
    await this.guarded("save lab", async () => {
      const res = await this.client.labSaveDoc($state.snapshot(this.lab) as LabDocument);
      const id = res.id ?? this.lab.id;
      this.savedDocIds.add(id);
      this.rememberActiveLab(id);
      this.lastSavedAt = Date.now();
      ok = true;
    });
    return ok;
  }

  async listLabs(): Promise<LabDocument[]> {
    const res = await this.client.labListDocs();
    // Any doc that came back from disk is, by definition, already saved.
    for (const l of res.labs) this.savedDocIds.add(l.id);
    return res.labs;
  }

  async deleteLab(labId: string) {
    await this.guarded("delete lab", async () => {
      await this.client.labDeleteDoc(labId);
      this.savedDocIds.delete(labId);
    });
  }

  /** Clone a stored lab under a fresh id + " (copy)" name and persist the copy,
   *  so the original (e.g. a built-in starter lab) stays pristine while the user
   *  edits the clone. Returns the new doc, or null on failure. Existing "copy"
   *  names get a numeric suffix so repeated clones don't collide. */
  async cloneLab(source: LabDocument): Promise<LabDocument | null> {
    let out: LabDocument | null = null;
    await this.guarded("clone lab", async () => {
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
    return out;
  }

  /** Open a doc into the workspace (reuses loadLab's connect-time path). Pass
   *  fromStore=true when the doc came from the durable store (so it's treated as
   *  already-saved and eligible for autosave); false for New/Import of a doc the
   *  user hasn't persisted yet. */
  async openLab(lab: LabDocument, fromStore = false) {
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

  /** Debounced autosave after any topology/annotation edit (real supervisor
   *  only — mock has no durable store). Unconditional: the FIRST edit to a
   *  fresh working lab persists it too (and records it as the last-active lab),
   *  so a browser refresh keeps the user's additions instead of reloading the
   *  seed. Debounced so a burst of edits/drags coalesces into one save. */
  scheduleAutosave() {
    if (this.transportKind !== "ws") return;
    if (this.autosaveTimer) clearTimeout(this.autosaveTimer);
    this.autosaveTimer = setTimeout(() => {
      this.autosaveTimer = null;
      void this.saveLab();
    }, 1200);
  }

  // ---- live-capture console tabs (feature 1) ----

  openCapture(linkId: number) {
    const link = this.lab.links.find((l) => l.id === linkId);
    if (link) {
      // Mark the link for capture so the next lab start bridges it. If it's not
      // already enabled and the lab is running natively, traffic won't flow
      // until restart — the tab surfaces that hint itself.
      link.capture = { enabled: true, mode: "live" };
    }
    if (!this.openCaptureTabs.includes(linkId)) {
      this.openCaptureTabs = [...this.openCaptureTabs, linkId];
    }
    // Ask the supervisor to start teeing this link (idempotent; harmless when
    // the lab is stopped — it'll bridge on next start).
    if (this.lab.id) void this.client.captureStart(this.lab.id, linkId).catch(() => {});
    // Fresh recording buffer for the "Save .pcapng" download each time a tab is
    // (re)opened, so a saved file isn't polluted by a prior session's bytes.
    this.captureBuffers.delete(linkId);
    this.scheduleAutosave();
  }

  // ---- raw capture recording (for "Save .pcapng" download) ----
  // Wireshark's live `-i TCP@host:port` attach works, but requires wireshark on
  // PATH; a downloaded .pcapng opens in Wireshark by double-click with zero
  // setup, so we buffer the raw pcapng byte stream a CaptureTerm receives and
  // offer it as a file. Plain Map (not reactive) — binary, appended per frame.
  private captureBuffers = new Map<number, { chunks: Uint8Array[]; bytes: number }>();
  private static CAPTURE_BUF_CAP = 64 * 1024 * 1024;
  /** Reactive byte counter so the download button can show size / enable state. */
  captureRecorded = $state<Record<number, number>>({});

  /** Append raw pcapng bytes from a link's live capture stream (called by
   *  CaptureTerm). Copies the view (the transport reuses its buffer) and caps
   *  total size; the stream stays a valid pcapng (a truncated trailing block is
   *  ignored by readers). */
  appendCaptureBytes(linkId: number, bytes: Uint8Array) {
    let b = this.captureBuffers.get(linkId);
    if (!b) {
      b = { chunks: [], bytes: 0 };
      this.captureBuffers.set(linkId, b);
    }
    if (b.bytes >= LabStore.CAPTURE_BUF_CAP) return;
    b.chunks.push(bytes.slice());
    b.bytes += bytes.length;
    this.captureRecorded = { ...this.captureRecorded, [linkId]: b.bytes };
  }

  /** Trigger a browser download of the buffered pcapng for a link. The file
   *  opens directly in Wireshark (no PATH/command needed). */
  downloadCapture(linkId: number) {
    const b = this.captureBuffers.get(linkId);
    if (!b || b.chunks.length === 0) {
      this.pushLog("warn", "no packets captured yet — wait for traffic on this link");
      return;
    }
    const blob = new Blob(b.chunks as BlobPart[], { type: "application/vnd.tcpdump.pcap" });
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
    this.captureBuffers.delete(linkId);
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
    await this.guarded("start lab", async () => {
      await this.client.labStart(this.lab.id);
    });
  }

  /** Force-clean orphaned runtime state: ask the supervisor to stop every
   *  tracked node + all relays/bridges/captures (regardless of labId), then
   *  reset local runtime state so the GUI matches. Use when nodes still show
   *  running or host CPU stays high after a normal stop. */
  async forceClean() {
    await this.guarded("force clean", async () => {
      const res = await this.client.labReap();
      // Reset local runtime view: everything is stopped now.
      const states: Record<number, NodeState> = {};
      for (const n of this.lab.nodes) states[n.id] = "stopped";
      this.nodeStates = states;
      this.openConsoleTabs = [];
      this.activeConsoleTab = null;
      this.openCaptureTabs = [];
      this.capturePorts = {};
      this.captureBuffers.clear();
      this.captureRecorded = {};
      this.linkStats = {};
      this.pushLog("info", `force clean: stopped ${res?.reaped ?? 0} node(s) and all relays`);
    });
  }

  async stopLab() {
    await this.guarded("stop lab", async () => {
      await this.client.labStop(this.lab.id);
      this.openConsoleTabs = [];
      this.activeConsoleTab = null;
    });
  }

  /** Deletes saved configs/state for every node in the lab. Destructive —
   *  callers (UI) must confirm with the user before invoking this. Under the
   *  mock transport lab.wipe isn't implemented, so a rejection here is logged
   *  and swallowed rather than surfaced as a hard error. */
  async wipeLab() {
    await this.guarded("wipe lab", () => this.client.labWipe(this.lab.id, null));
  }

  /** Wipe saved configs/state for a single node. Destructive — the caller (UI)
   *  must confirm with the user first. Mirrors wipeLab: mock lab.wipe isn't
   *  implemented, so a rejection is logged + swallowed via guarded(). */
  async wipeNode(nodeId: number) {
    // RPC-ack only (no driving node.state) → release when the awaited call
    // settles (guarded never rethrows, so the finally always runs).
    if (!this.acquireNodeLock(nodeId, "wiping")) return;
    try {
      await this.guarded(`wipe node ${nodeId}`, () => this.client.labWipe(this.lab.id, [nodeId]));
    } finally {
      this.releaseNodeLock(nodeId);
    }
  }

  async startNode(nodeId: number) {
    // Lock the node until its next node.state event (released in handleEvent).
    // Already-locked → no-op (that IS the lock).
    if (!this.acquireNodeLock(nodeId, "starting")) return;
    await this.guarded(`start node ${nodeId}`, () => this.client.nodeStart(this.lab.id, nodeId));
  }

  async stopNode(nodeId: number) {
    if (!this.acquireNodeLock(nodeId, "stopping")) return;
    await this.guarded(`stop node ${nodeId}`, async () => {
      await this.client.nodeStop(this.lab.id, nodeId);
      this.openConsoleTabs = this.openConsoleTabs.filter((id) => id !== nodeId);
      if (this.activeConsoleTab === nodeId) {
        this.activeConsoleTab = this.openConsoleTabs[0] ?? null;
      }
    });
  }

  // ---- per-node action lock (WS1) ----

  /** Acquire the per-node action lock. Returns false if the node is already
   *  locked (caller must no-op). Arms a 60s safety timeout so a lost driving
   *  event can't wedge the node permanently. */
  private acquireNodeLock(nodeId: number, action: string): boolean {
    if (this.nodeLocks[nodeId]) return false;
    this.nodeLocks = { ...this.nodeLocks, [nodeId]: { action, startedAt: Date.now() } };
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
  private async guarded(what: string, fn: () => Promise<unknown>) {
    try {
      await fn();
      this.lastError = null;
    } catch (e) {
      this.lastError = `${what} failed: ${(e as Error).message}`;
      this.pushLog("error", this.lastError);
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
    // Lock the SOURCE node until the async supervisor sync settles. Already
    // locked → no-op.
    if (!this.acquireNodeLock(nodeId, "duplicating")) return null;
    const id = this.nextNodeId();
    const clone: LabNode = {
      ...structuredClone($state.snapshot(src)),
      id,
      name: this.uniqueDuplicateName(src.name),
      x: src.x + 40,
      y: src.y + 40,
    };
    // supervisor sync happens async; id is final now. addNode is guarded (never
    // rethrows), so release the source lock once it settles either way.
    void this.addNode(clone).finally(() => this.releaseNodeLock(nodeId));
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

  // ---- per-node / lab config extraction into the doc (feature 4) ----

  /** Extract NVRAM startup-config for one node into node.startupConfig. Runs on
   *  the node's SAVED config, so the user must `write memory` first. */
  async saveNodeConfig(nodeId: number) {
    await this.guarded(`save config for node ${nodeId}`, async () => {
      const res = await this.client.configExtract(this.lab.id, [nodeId]);
      this.applyExtractedConfigs(res.configs);
    });
    this.scheduleAutosave();
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
    await this.guarded("save configs", async () => {
      const res = await this.client.configExtract(this.lab.id, targets);
      this.applyExtractedConfigs(res.configs);
    });
    this.scheduleAutosave();
  }

  private applyExtractedConfigs(configs: { node: number; startupConfig: string }[]) {
    for (const c of configs) {
      const node = this.lab.nodes.find((n) => n.id === c.node);
      if (node) node.startupConfig = c.startupConfig;
    }
  }
}

export const labStore = new LabStore();
