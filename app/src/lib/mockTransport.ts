// In-memory fake supervisor. Drives the whole UI with fake nodes/state
// transitions/console echo so the app is fully clickable with no backend.
import type { Request, Response, SupervisorEvent } from "./protocol";
import type {
  CaptureStartResult,
  ConfigResult,
  HelloResult,
  ImageListResult,
  ImageRegisterResult,
  LabLoadResult,
  LabStartResult,
  NodeRuntimeStatus,
  NodeSetImageResult,
  StatusResult,
} from "./protocol";
import type { LabDocument, LibraryImage, NodeState } from "./labTypes";
import type { IncomingFrame, Transport } from "./transport";

const MOCK_IMAGES: LibraryImage[] = [
  {
    id: "a1b2c3d4",
    filename: "i86bi_linux-adventerprisek9-ms.vm.bin",
    class: "l3",
    arch: "i386",
    sha256: "a1b2c3d4e5f6".padEnd(64, "0"),
    size: 52_428_800,
  },
  {
    id: "b2c3d4e5",
    filename: "i86bi_linux_l2-adventerprisek9-ms.bin",
    class: "l2",
    arch: "i386",
    sha256: "b2c3d4e5f6a7".padEnd(64, "0"),
    size: 61_865_984,
  },
  {
    id: "c3d4e5f6",
    filename: "iol-xe-17.12.bin",
    class: "l3",
    arch: "x86_64",
    sha256: "c3d4e5f6a7b8".padEnd(64, "0"),
    size: 104_857_600,
  },
];

interface MockNodeRuntime {
  id: number;
  state: NodeState;
  consolePort: number;
  pid: number;
  imageId?: string;
}

let consolePortCounter = 9001;

function delay(ms: number) {
  return new Promise((r) => setTimeout(r, ms));
}

export class MockTransport implements Transport {
  connected = false;
  private handlers = new Set<(frame: IncomingFrame) => void>();
  private lab: LabDocument | null = null;
  private nodes = new Map<number, MockNodeRuntime>();
  private images = [...MOCK_IMAGES];
  private captures = new Map<number, number>(); // linkId -> capturePort
  private captureCounter = 5501;
  private consoleBuffers = new Map<number, string[]>();
  private docs = new Map<string, LabDocument>(); // durable lab-doc store (mock)

  async connect(): Promise<void> {
    await delay(150);
    this.connected = true;
    this.startHostStats();
  }

  disconnect(): void {
    this.connected = false;
    this.stopLinkStats();
    this.stopHostStats();
    this.handlers.clear();
  }

  private hostStatsTimer: ReturnType<typeof setInterval> | null = null;

  /** Dev-only synthetic host.stats so the left-pane monitor animates without a
   *  real supervisor. Gentle random-walk around plausible values. */
  private startHostStats() {
    this.stopHostStats();
    const total = 8 * 1024 * 1024 * 1024;
    const disk = 40 * 1024 * 1024 * 1024;
    let cpu = 12;
    const tick = () => {
      cpu = Math.max(2, Math.min(98, cpu + (Math.random() - 0.5) * 18));
      const running = [...this.nodes.values()].filter((n) => n.state === "running").length;
      const memUsed = Math.min(total, (1.2 + running * 0.9) * 1024 * 1024 * 1024);
      this.emit({
        event: "host.stats",
        data: {
          cpuPct: Math.round(cpu * 10) / 10,
          memUsed: Math.round(memUsed),
          memTotal: total,
          diskUsed: Math.round(disk * 0.36),
          diskTotal: disk,
          cores: 8,
        },
      } as SupervisorEvent);
    };
    tick();
    this.hostStatsTimer = setInterval(tick, 2000);
  }

  private stopHostStats() {
    if (this.hostStatsTimer) {
      clearInterval(this.hostStatsTimer);
      this.hostStatsTimer = null;
    }
  }

  onMessage(handler: (frame: IncomingFrame) => void): () => void {
    this.handlers.add(handler);
    return () => this.handlers.delete(handler);
  }

  private emit(frame: IncomingFrame) {
    for (const h of this.handlers) h(frame);
  }

  private ok<R>(id: string, result: R) {
    this.emit({ id, ok: true, result } satisfies Response<R>);
  }

  private err(id: string, code: string, message: string) {
    this.emit({ id, ok: false, error: { code, message } } satisfies Response);
  }

  send(req: Request): void {
    // Simulate network latency + async processing.
    void this.handle(req);
  }

  /** Console output feed for the mock terminal (used by Console.svelte). */
  getConsoleHistory(nodeId: number): string[] {
    return this.consoleBuffers.get(nodeId) ?? [];
  }

  /** Simulate typed input echoing back with a fake IOS-ish prompt. */
  writeConsole(nodeId: number, data: string) {
    const buf = this.consoleBuffers.get(nodeId) ?? [];
    buf.push(data);
    this.consoleBuffers.set(nodeId, buf);
    this.emit({
      event: "log",
      data: { level: "debug", message: `console echo`, node: nodeId },
    } as SupervisorEvent);
  }

  private async handle(req: Request): Promise<void> {
    await delay(80 + Math.random() * 120);
    const { id, op, args } = req as Request<any>;

    switch (op) {
      case "hello": {
        this.ok<HelloResult>(id, {
          supervisor: "0.1.0-mock",
          runtime: "debian-slim-12",
          arch: "x86_64",
          // mgmt is parked product-wide (macvlan MAC-filter limitation) and
          // never advertised — mirror the real supervisor so dev matches.
          features: ["nvram", "capture", "i386", "natgw"],
        });
        return;
      }

      case "image.list": {
        this.ok<ImageListResult>(id, { images: this.images });
        return;
      }

      case "image.register": {
        const filename = String(args?.path ?? "unknown.bin").split(/[\\/]/).pop()!;
        const newImage: LibraryImage = {
          id: Math.random().toString(16).slice(2, 10),
          filename,
          class: filename.includes("l2") ? "l2" : "l3",
          arch: "i386",
          sha256: Array.from({ length: 64 }, () =>
            "0123456789abcdef"[Math.floor(Math.random() * 16)]
          ).join(""),
          size: 40_000_000 + Math.floor(Math.random() * 60_000_000),
        };
        this.images.push(newImage);
        this.ok<ImageRegisterResult>(id, {
          id: newImage.id,
          class: newImage.class,
          arch: newImage.arch,
          sha256: newImage.sha256,
        });
        return;
      }

      case "image.remove": {
        this.images = this.images.filter((i) => i.id !== args?.id);
        this.ok(id, {});
        return;
      }

      case "lab.load": {
        const lab = args?.lab as LabDocument;
        this.lab = lab;
        this.nodes.clear();
        const nodes = lab.nodes.map((n) => {
          const rt: MockNodeRuntime = {
            id: n.id,
            state: "stopped",
            consolePort: consolePortCounter++,
            pid: 0,
            imageId: n.image?.id,
          };
          this.nodes.set(n.id, rt);
          return { id: n.id, consolePort: rt.consolePort };
        });
        this.ok<LabLoadResult>(id, { labId: lab.id, nodes, warnings: [] });
        return;
      }

      case "lab.start": {
        const targetIds: number[] | null = args?.nodes ?? null;
        const ids = targetIds ?? [...this.nodes.keys()];
        const started: NodeRuntimeStatus[] = [];
        for (const nid of ids) {
          const rt = this.nodes.get(nid);
          if (!rt) continue;
          void this.bootSequence(nid);
          started.push({ node: nid, consolePort: rt.consolePort, pid: rt.pid, state: rt.state });
        }
        this.ok<LabStartResult>(id, { started });
        // Dev-only: emit periodic link.stats for bridged links so the traffic
        // glow (feature 2) is observable without a real relay. Native IOL↔IOL
        // p2p links get none, matching production semantics.
        this.startLinkStats();
        return;
      }

      case "lab.stop": {
        const targetIds: number[] | null = args?.nodes ?? null;
        const ids = targetIds ?? [...this.nodes.keys()];
        for (const nid of ids) void this.stopSequence(nid);
        if (targetIds === null) this.stopLinkStats();
        this.ok(id, {});
        return;
      }

      case "node.start": {
        const nid = args?.node as number;
        void this.bootSequence(nid);
        const rt = this.nodes.get(nid);
        this.ok<NodeRuntimeStatus>(id, {
          node: nid,
          consolePort: rt?.consolePort ?? 0,
          pid: rt?.pid ?? 0,
          state: rt?.state ?? "starting",
        });
        return;
      }

      case "node.stop": {
        const nid = args?.node as number;
        void this.stopSequence(nid);
        const rt = this.nodes.get(nid);
        this.ok<NodeRuntimeStatus>(id, {
          node: nid,
          consolePort: rt?.consolePort ?? 0,
          pid: 0,
          state: "stopped",
        });
        return;
      }

      case "node.restart": {
        const nid = args?.node as number;
        void this.stopSequence(nid).then(() => this.bootSequence(nid));
        const rt = this.nodes.get(nid);
        this.ok<NodeRuntimeStatus>(id, {
          node: nid,
          consolePort: rt?.consolePort ?? 0,
          pid: rt?.pid ?? 0,
          state: "starting",
        });
        return;
      }

      case "node.setImage": {
        const nid = args?.node as number;
        const imageId = args?.imageId as string;
        const rt = this.nodes.get(nid);
        if (!rt) {
          this.err(id, "not_loaded", `node ${nid} not loaded`);
          return;
        }
        const img = this.images.find((i) => i.id === imageId);
        if (!img) {
          this.err(id, "image_not_found", `image ${imageId} not found`);
          return;
        }
        rt.imageId = imageId;
        this.ok<NodeSetImageResult>(id, { node: nid, imageId, class: img.class });
        return;
      }

      case "link.add":
      case "link.remove": {
        this.ok(id, {});
        return;
      }

      // Incremental node sync (mirrors the real supervisor's node.add/remove):
      // register a dropped node's runtime so node.start finds it without a
      // lab.load round-trip.
      case "node.add": {
        const n = args?.node as LabDocument["nodes"][number];
        if (this.nodes.has(n.id)) {
          this.err(id, "bad_request", `node ${n.id} already exists`);
          return;
        }
        const rt: MockNodeRuntime = {
          id: n.id,
          state: "stopped",
          consolePort: consolePortCounter++,
          pid: 0,
          imageId: n.image?.id,
        };
        this.nodes.set(n.id, rt);
        this.ok(id, { node: n.id, consolePort: rt.consolePort });
        return;
      }

      case "node.remove": {
        const nid = args?.node as number;
        this.nodes.delete(nid);
        this.ok(id, {});
        return;
      }

      case "capture.start": {
        const linkId = args?.link as number;
        const port = this.captureCounter++;
        this.captures.set(linkId, port);
        this.ok<CaptureStartResult>(id, { link: linkId, capturePort: port });
        setTimeout(() => {
          this.emit({
            event: "capture.started",
            data: { link: linkId, capturePort: port },
          } as SupervisorEvent);
        }, 50);
        return;
      }

      case "capture.stop": {
        const linkId = args?.link as number;
        this.captures.delete(linkId);
        this.ok(id, {});
        setTimeout(() => {
          this.emit({ event: "capture.stopped", data: { link: linkId } } as SupervisorEvent);
        }, 30);
        return;
      }

      case "config.save":
      case "config.extract": {
        const ids: number[] = args?.nodes ?? [...this.nodes.keys()];
        const configs = ids.map((nid) => ({
          node: nid,
          startupConfig: `! extracted config for node ${nid}\nhostname R${nid}\n!\nend\n`,
        }));
        this.ok<ConfigResult>(id, { configs });
        return;
      }

      case "lab.saveDoc": {
        const doc = args?.lab as LabDocument;
        this.docs.set(doc.id, JSON.parse(JSON.stringify(doc)));
        this.ok(id, { id: doc.id });
        return;
      }

      case "lab.listDocs": {
        this.ok(id, { labs: [...this.docs.values()] });
        return;
      }

      case "lab.getDoc": {
        const doc = this.docs.get(args?.labId as string);
        if (!doc) {
          this.err(id, "not_loaded", `lab ${args?.labId} not found`);
          return;
        }
        this.ok(id, { lab: doc });
        return;
      }

      case "lab.deleteDoc": {
        this.docs.delete(args?.labId as string);
        this.ok(id, {});
        return;
      }

      case "status": {
        const nodes = [...this.nodes.values()].map((rt) => ({
          id: rt.id,
          state: rt.state,
          consolePort: rt.consolePort,
          pid: rt.pid,
          image: rt.imageId,
        }));
        const links = (this.lab?.links ?? []).map((l) => ({
          id: l.id,
          capturePort: this.captures.get(l.id),
        }));
        this.ok<StatusResult>(id, { labId: this.lab?.id ?? null, nodes, links });
        return;
      }

      default:
        this.err(id, "unsupported", `unknown op ${op}`);
    }
  }

  private statsTimer: ReturnType<typeof setInterval> | null = null;

  /** Dev-only synthetic link.stats for bridged links (any link touching a VPCS
   *  node, or with capture enabled — mirrors "bridged links only" from the wire
   *  spec). Native IOL↔IOL p2p links stay silent. */
  private startLinkStats() {
    this.stopLinkStats();
    const isBridged = (l: { endpoints: { node: number }[]; capture?: { enabled?: boolean } }) => {
      if (l.capture?.enabled) return true;
      const kinds = l.endpoints.map(
        (e) => this.lab?.nodes.find((n) => n.id === e.node)?.kind
      );
      return kinds.some((k) => k === "vpcs" || k === "nat" || k === "mgmt");
    };
    // Network Watcher demo — fixed control-plane mixes so the overlays are
    // demo-able with predictable direction: link 0 shows one-way STP (from
    // endpoints[0], i.e. R1) plus two-way CDP, link 1 two-way pings. Other
    // bridged links get the two-way default. Aggregate `protos` mirrors the
    // per-direction sums so both consumers stay consistent.
    const DEMO_DIR: Record<number, Record<string, [number, number]>> = {
      0: { STP: [0.5, 0], CDP: [0.1, 0.1] },
      1: { PING: [1, 1] },
    };
    const DEMO_DIR_DEFAULT: Record<string, [number, number]> = { CDP: [0.1, 0.1], ICMP: [1, 1] };
    this.statsTimer = setInterval(() => {
      for (const l of this.lab?.links ?? []) {
        // Links with a scripted demo mix emit even when "native" (the seeded
        // demo lab's link 0 is IOL↔IOL) — otherwise the watcher's one-way
        // animation couldn't be exercised in dev at all. Other native links
        // keep production semantics (silent).
        if (!isBridged(l) && !(l.id in DEMO_DIR)) continue;
        // Random-walk-ish traffic so the glow visibly modulates.
        const fps = Math.round((5 + Math.random() * 400) * 10) / 10;
        const protosDir = DEMO_DIR[l.id] ?? DEMO_DIR_DEFAULT;
        const protos = Object.fromEntries(
          Object.entries(protosDir).map(([k, [a, b]]) => [k, a + b])
        );
        this.emit({
          event: "link.stats",
          data: {
            link: l.id,
            fps,
            bps: Math.round(fps * (64 + Math.random() * 1400)),
            protos,
            protosDir,
          },
        } as SupervisorEvent);
      }
    }, 2000);
  }

  private stopLinkStats() {
    if (this.statsTimer) {
      clearInterval(this.statsTimer);
      this.statsTimer = null;
    }
  }

  private async bootSequence(nodeId: number) {
    const rt = this.nodes.get(nodeId);
    if (!rt) return;
    rt.state = "starting";
    this.emit({ event: "node.state", data: { node: nodeId, state: "starting" } });
    await delay(700 + Math.random() * 900);
    // Small chance of a crash to demo the state visually, but keep it rare.
    if (Math.random() < 0.04) {
      rt.state = "crashed";
      this.emit({ event: "node.state", data: { node: nodeId, state: "crashed" } });
      return;
    }
    rt.pid = 10000 + Math.floor(Math.random() * 50000);
    rt.state = "running";
    this.emit({ event: "node.state", data: { node: nodeId, state: "running" } });
    this.emit({
      event: "node.console",
      data: { node: nodeId, consolePort: rt.consolePort },
    });
    const buf = this.consoleBuffers.get(nodeId) ?? [];
    buf.push(
      `\r\n`,
      `IOS bootstrap loader... booting node ${nodeId}\r\n`,
      `Restricted Rights Legend\r\n\r\n`,
      `\r\nPress RETURN to get started!\r\n\r\n`,
      `Router>`
    );
    this.consoleBuffers.set(nodeId, buf);
  }

  private async stopSequence(nodeId: number) {
    const rt = this.nodes.get(nodeId);
    if (!rt) return;
    rt.pid = 0;
    rt.state = "stopped";
    this.emit({ event: "node.state", data: { node: nodeId, state: "stopped" } });
  }
}
