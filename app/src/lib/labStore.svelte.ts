// Central app state, Svelte 5 runes. One instance shared via module scope
// (simple singleton store — no context provider needed for a single-window app).
import { emptyLab, type LabDocument, type LabLink, type LabNode, type LibraryImage, type NodeState } from "./labTypes";
import { SupervisorClient } from "./supervisor";
import { MockTransport } from "./mockTransport";
import { selectTransport } from "./transportSelect";
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
  nodeStates = $state<Record<number, NodeState>>({});
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
      await this.client.connect();
      this.providerStatus = "connected";
      // Real supervisor: the runtime provider is whatever process spawned it,
      // not a Windows-side choice — leave activeProvider as Preflight (mock
      // path) or unset (ws path) already set it, rather than hardcoding.
      if (this.transportKind === "mock") this.activeProvider = "vmware";
      const { images } = await this.client.imageList();
      this.images = images;
      this.reconcileNodeImages();
      await this.loadLab(this.lab);
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
   * exists, else clear it so the pre-start check flags the node instead of
   * lab.start failing opaquely with image_not_found.
   */
  private reconcileNodeImages() {
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
      } else {
        this.pushLog(
          "warn",
          `node ${node.name}: image ${node.image.filename} is not registered and no ${node.image.class} image is available`
        );
        node.image = undefined;
      }
    }
  }

  private handleEvent(evt: SupervisorEvent) {
    switch (evt.event) {
      case "node.state":
        this.nodeStates = { ...this.nodeStates, [evt.data.node]: evt.data.state };
        break;
      case "node.console":
        this.consolePorts = { ...this.consolePorts, [evt.data.node]: evt.data.consolePort };
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
    await this.guarded(`wipe node ${nodeId}`, () => this.client.labWipe(this.lab.id, [nodeId]));
  }

  async startNode(nodeId: number) {
    await this.guarded(`start node ${nodeId}`, () => this.client.nodeStart(this.lab.id, nodeId));
  }

  async stopNode(nodeId: number) {
    await this.guarded(`stop node ${nodeId}`, async () => {
      await this.client.nodeStop(this.lab.id, nodeId);
      this.openConsoleTabs = this.openConsoleTabs.filter((id) => id !== nodeId);
      if (this.activeConsoleTab === nodeId) {
        this.activeConsoleTab = this.openConsoleTabs[0] ?? null;
      }
    });
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

  closeConsole(nodeId: number) {
    this.openConsoleTabs = this.openConsoleTabs.filter((id) => id !== nodeId);
    if (this.activeConsoleTab === nodeId) {
      this.activeConsoleTab = this.openConsoleTabs[0] ?? null;
    }
  }

  addNode(node: LabNode) {
    this.lab.nodes = [...this.lab.nodes, node];
    this.nodeStates = { ...this.nodeStates, [node.id]: "stopped" };
  }

  removeNode(nodeId: number) {
    this.lab.nodes = this.lab.nodes.filter((n) => n.id !== nodeId);
    this.lab.links = this.lab.links.filter(
      (l) => !l.endpoints.some((e) => e.node === nodeId)
    );
    if (this.selectedNodeId === nodeId) this.selectedNodeId = null;
  }

  addLink(link: LabLink) {
    this.lab.links = [...this.lab.links, link];
  }

  removeLink(linkId: number) {
    this.lab.links = this.lab.links.filter((l) => l.id !== linkId);
    if (this.selectedLinkId === linkId) this.selectedLinkId = null;
  }

  nextNodeId(): number {
    return this.lab.nodes.length === 0 ? 0 : Math.max(...this.lab.nodes.map((n) => n.id)) + 1;
  }

  nextLinkId(): number {
    return this.lab.links.length === 0 ? 0 : Math.max(...this.lab.links.map((l) => l.id)) + 1;
  }

  async setNodeImage(nodeId: number, imageId: string) {
    await this.client.nodeSetImage(this.lab.id, nodeId, imageId);
    const node = this.lab.nodes.find((n) => n.id === nodeId);
    const img = this.images.find((i) => i.id === imageId);
    if (node && img) {
      node.image = { id: img.id, filename: img.filename, class: img.class };
    }
  }

  replaceImageEverywhere(fromId: string, toId: string) {
    const img = this.images.find((i) => i.id === toId);
    if (!img) return;
    for (const node of this.lab.nodes) {
      if (node.image?.id === fromId) {
        node.image = { id: img.id, filename: img.filename, class: img.class };
        void this.client.nodeSetImage(this.lab.id, node.id, img.id);
      }
    }
  }

  setNodeIcon(nodeId: number, iconKey: string) {
    const node = this.lab.nodes.find((n) => n.id === nodeId);
    if (node) node.icon = iconKey;
  }

  get selectedNode(): LabNode | null {
    return this.lab.nodes.find((n) => n.id === this.selectedNodeId) ?? null;
  }
}

export const labStore = new LabStore();
