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
import { labFromText } from "./yaml";
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
  private docs = new Map<string, string>(); // durable lab-doc store (mock; YAML text like the real wire)

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
          features: ["nvram", "capture", "i386", "natgw"],
          // WS6 — default matches a real host NAT (no badge). To exercise the
          // NAT-node slirp warning badge in dev, set egress to "slirp" and add
          // an egressNote:
          //   egress: "slirp",
          //   egressNote: "QEMU user-mode slirp: DHCP & outbound TCP work through NAT, but ping/traceroute to the internet do not. Use the bridged VMware/OVA appliance or WSL2 for real internet.",
          egress: "routed",
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

      case "painter.collect": {
        const proto = String(args?.proto ?? "stp") as
          | "stp" | "ospf" | "eigrp" | "bgp";
        const dest = typeof args?.dest === "string" ? args.dest : "";
        this.ok(id, this.mockPainter(proto, dest));
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
        // The wire carries the doc as YAML text (see supervisor.ts).
        const text = args?.lab as string;
        const docId = labFromText(text).id;
        this.docs.set(docId, text);
        this.ok(id, { id: docId });
        return;
      }

      case "lab.listDocs": {
        this.ok(id, { labs: [...this.docs.values()] });
        return;
      }

      case "lab.getDoc": {
        const text = this.docs.get(args?.labId as string);
        if (!text) {
          this.err(id, "not_loaded", `lab ${args?.labId} not found`);
          return;
        }
        this.ok(id, { lab: text });
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

  /** Dev-only synthetic painter.collect snapshot so the Topology Painter panel
   *  + overlays are exercisable with no backend. Keyed off whatever lab is
   *  loaded: only RUNNING IOL nodes get data; everything else returns a
   *  running:false hint (never faked), matching the real contract. The
   *  interface names use the painter's `Et0/0` form (interfaceNorm `et0/0`) so
   *  the frontend canonicalizer is genuinely exercised against the lab doc's
   *  short `e0/0` endpoints. */
  private mockPainter(
    proto: "stp" | "ospf" | "eigrp" | "bgp",
    dest: string
  ): unknown {
    const iolIds = (this.lab?.nodes ?? [])
      .filter((n) => n.kind === "iol")
      .map((n) => n.id);
    // Painter-style names from the lab doc's short endpoint names on THIS node.
    const portsOf = (nodeId: number): { iface: string; norm: string }[] => {
      const seen = new Set<string>();
      const out: { iface: string; norm: string }[] = [];
      for (const l of this.lab?.links ?? []) {
        for (const e of l.endpoints) {
          if (e.node !== nodeId) continue;
          if (seen.has(e.interface)) continue;
          seen.add(e.interface);
          // e0/0 -> Et0/0 / et0/0 (only ethernet ports carry STP/OSPF here).
          const mm = e.interface.match(/^e(\d+\/\d+)$/i);
          if (!mm) continue;
          out.push({ iface: `Et${mm[1]}`, norm: `et${mm[1]}` });
        }
      }
      return out;
    };
    const running = (nodeId: number) => this.nodes.get(nodeId)?.state === "running";

    const nodes = iolIds.map((nid, i) => {
      if (!running(nid)) {
        return { node: nid, running: false, hint: "start the lab — node is not running" };
      }
      const ports = portsOf(nid);
      const base: Record<string, unknown> = { node: nid, running: true, hint: "" };
      if (proto === "stp") {
        const isRoot = i === 0; // lowest-index IOL node plays the root bridge.
        const rootId = `32768.aabb.cc00.0100`;
        base.stp = {
          rootId,
          bridgeId: isRoot ? rootId : `${32768 + nid}.aabb.cc00.0${nid}00`,
          isRoot,
          rootCost: isRoot ? 0 : 100,
          rootPort: isRoot || ports.length === 0 ? "" : ports[0].iface,
          ports: ports.map((p, pi) => {
            // First port toward root = Root/FWD; a redundant 2nd port blocks.
            const blocked = !isRoot && pi === 1;
            const role = isRoot ? "Desg" : pi === 0 ? "Root" : blocked ? "Altn" : "Desg";
            return {
              interface: p.iface,
              interfaceNorm: p.norm,
              role,
              state: blocked ? "BLK" : "FWD",
              cost: 100,
              prio: 128,
              blocked,
              ...(blocked
                ? {
                    reason:
                      "Alternate port: a superior BPDU was received on the root " +
                      "port, so the root is already reachable at lower cost. This " +
                      "redundant port is blocked to break the loop.",
                  }
                : {}),
            };
          }),
        };
      } else if (proto === "ospf") {
        // Neighbour on each ethernet port; a route toward dest via the first.
        base.ospf = {
          neighbors: ports.map((p, pi) => ({
            neighborId: `10.0.0.${nid + 1}`,
            state: "FULL",
            role: pi === 0 && i === 0 ? "DR" : "DROTHER",
            address: `10.${nid}.${pi}.2`,
            interface: p.iface,
            interfaceNorm: p.norm,
          })),
          ...(dest && ports.length && i !== 0
            ? {
                route: {
                  prefix: dest,
                  // Point at the lower-index node's advertised address so the
                  // best-path resolver lights the edge toward it.
                  nextHop: `10.${nid - 1}.0.2`,
                  interface: ports[0].iface,
                  interfaceNorm: ports[0].norm,
                  cost: 10 * (i + 1),
                },
              }
            : {}),
        };
      } else if (proto === "eigrp") {
        base.eigrp = {
          prefix: dest || "0.0.0.0/0",
          fd: 3072 * (i + 1),
          nextHop: i === 0 ? "" : `10.${nid - 1}.0.2`,
          paths: ports.map((p, pi) => ({
            nextHop: `10.${nid}.${pi}.2`,
            interface: p.iface,
            interfaceNorm: p.norm,
            fd: 3072 * (i + 1),
            rd: 2816 * (i + 1),
            successor: pi === 0,
            feasibleSuccessor: pi === 1,
          })),
        };
      } else {
        base.bgp = {
          prefix: dest || "0.0.0.0/0",
          bestNextHop: i === 0 ? "" : `10.${nid - 1}.0.2`,
          reason:
            i === 0
              ? "Locally originated / best."
              : "Best path: highest local-preference (150) beats the alternate (100).",
          paths: [
            {
              nextHop: `10.${nid - 1}.0.2`,
              asPath: `6500${nid}`,
              origin: "i",
              weight: 0,
              localPref: 150,
              med: 0,
              best: true,
            },
          ],
        };
      }
      return base;
    });

    return { proto, dest, nodes };
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
      return kinds.some((k) => k === "vpcs" || k === "nat");
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
    // Per-subtype demo split so the packet-type dropdown is exercisable in dev:
    // link 1's PING is half echo-request / half echo-reply, one per direction.
    const DEMO_SUBDIR: Record<number, Record<string, Record<string, [number, number]>>> = {
      1: { PING: { "echo-request": [1, 0], "echo-reply": [0, 1] } },
    };
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
            protosSubtypeDir: DEMO_SUBDIR[l.id],
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
