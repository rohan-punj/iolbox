// Topology Painter (WS5b) — rune-backed singleton, sibling to watcherStore.
// On-demand SNAPSHOT overlays that show how STP / OSPF / EIGRP / BGP arrive at
// their decisions, painted from live device state (verb `painter.collect`).
// Unlike the watcher (a live poll off link.stats), the painter is a one-shot:
// the user hits Paint, we call the backend once, stash the PainterResult, and
// FloatingEdge reads it to draw badges/highlights until the user re-paints or
// clears. No polling loop lives here.
//
// Session-only: a snapshot never survives a reload (a stale topology overlay
// would mislead). panelOpen defaults closed — opt-in like the watcher.

import type { PainterProto, PainterResult, PainterNode } from "./painterTypes";
import { canonIface } from "./painterTypes";
import { labStore } from "./labStore.svelte";

/** The four paintable protocols, in display order. */
export const PAINTER_PROTOS: PainterProto[] = ["stp", "ospf", "eigrp", "bgp"];

export const PAINTER_PROTO_NAMES: Record<PainterProto, string> = {
  stp: "STP",
  ospf: "OSPF",
  eigrp: "EIGRP",
  bgp: "BGP",
};

/** Does this protocol need a destination? eigrp/bgp require it; ospf takes it
 *  optionally (enables best-path highlight); stp ignores it. Mirrors the
 *  backend contract exactly. */
export function destRequired(proto: PainterProto): boolean {
  return proto === "eigrp" || proto === "bgp";
}
export function destUsed(proto: PainterProto): boolean {
  return proto !== "stp";
}

/** A per-endpoint painter datum resolved onto ONE link endpoint. FloatingEdge
 *  asks the store for the STP badge (role/state/blocked/reason) at a given
 *  (nodeId, interface) so the badge lands on the right port. */
export interface StpEndpointBadge {
  /** Compact role token: R (Root) / D (Desg) / ALT (Altn) / BAK (Back). */
  role: string;
  /** Port state token: FWD / BLK / LRN / LIS / DIS. */
  state: string;
  blocked: boolean;
  /** Populated only for blocked ports — the student-readable "why". */
  reason?: string;
}

/** Which links a routing paint lit as best-path, plus the metric label to show.
 *  Keyed by link id. `dir` records which endpoint the winning next-hop points
 *  AT (the downstream neighbour) so FloatingEdge can arrow the edge correctly. */
export interface RoutingEdgePaint {
  linkId: number;
  /** endpoint index (0/1) that is the next-hop (downstream) end of this edge. */
  toEndpoint: 0 | 1;
  /** Short metric label, e.g. "cost 20", "FD 3072", "AS 65001 65002". */
  label: string;
}

class PainterStore {
  /** Floating panel visibility. Session-only, default closed. */
  panelOpen = $state(false);

  /** Chosen protocol for the next paint. */
  proto = $state<PainterProto>("stp");

  /** Destination for routing paints. Either a typed prefix/host string, or a
   *  node id the user picked (resolved to an address at paint time). Stored raw
   *  as a string; when a node was picked we keep its id in `destNodeId` so the
   *  <select> reflects the choice. */
  destText = $state("");
  destNodeId = $state<number | null>(null);

  /** The displayed snapshot, or null when nothing is painted. */
  result = $state<PainterResult | null>(null);
  /** Wall-clock ms when `result` was captured — shown in the panel so the user
   *  knows how fresh the overlay is. */
  paintedAt = $state<number | null>(null);
  /** In-flight paint (disables the button, shows a spinner). */
  busy = $state(false);
  /** Last paint error, shown inline in the panel. Cleared on the next paint. */
  error = $state<string | null>(null);

  togglePanel() {
    this.panelOpen = !this.panelOpen;
  }

  setProto(p: PainterProto) {
    this.proto = p;
    // A destination from a previous protocol still applies (same address space),
    // so it is intentionally NOT reset here.
  }

  /** Resolve the chosen destination to the STRING the backend expects. When the
   *  user picked a node, prefer that node's first configured interface/loopback
   *  address if we can infer one; otherwise fall back to the typed text. The
   *  frontend does NOT have authoritative addresses (lab docs don't carry L3
   *  config parsed out), so a node pick without a known address yields "" and
   *  the panel nudges the user to type a prefix. */
  resolveDest(): string {
    const t = this.destText.trim();
    if (t) return t;
    return "";
  }

  /** Run one snapshot. Guards non-running lab + missing-dest. Populates
   *  `result`/`paintedAt` on success, `error` on failure. */
  async paint(): Promise<void> {
    if (this.busy) return;
    const proto = this.proto;
    const dest = destUsed(proto) ? this.resolveDest() : "";
    if (destRequired(proto) && !dest) {
      this.error = `${PAINTER_PROTO_NAMES[proto]} needs a destination prefix or host.`;
      return;
    }
    this.error = null;
    this.busy = true;
    try {
      const res = await labStore.client.painterCollect(labStore.lab.id, proto, dest || undefined);
      this.result = res;
      this.paintedAt = Date.now();
    } catch (e) {
      this.error = (e as Error).message || "paint failed";
    } finally {
      this.busy = false;
    }
  }

  /** Clear the painted overlay (FloatingEdge gates all painter rendering on
   *  `result`, so nulling it removes every badge/highlight). */
  clear() {
    this.result = null;
    this.paintedAt = null;
    this.error = null;
  }

  // ---- overlay lookups (read by FloatingEdge) ----

  /** True while an STP snapshot is displayed. */
  get isStp(): boolean {
    return this.result?.proto === "stp";
  }
  /** True while a routing snapshot (ospf/eigrp/bgp) is displayed. */
  get isRouting(): boolean {
    const p = this.result?.proto;
    return p === "ospf" || p === "eigrp" || p === "bgp";
  }

  /** node id → PainterNode for the current snapshot (indexed lazily). */
  private nodeIndex = $derived.by(() => {
    const m = new Map<number, PainterNode>();
    for (const n of this.result?.nodes ?? []) m.set(n.node, n);
    return m;
  });

  /** The node ids that are the root bridge in the current STP snapshot (for the
   *  crown badge on the node face). */
  get stpRootNodeIds(): number[] {
    if (!this.isStp) return [];
    const out: number[] = [];
    for (const n of this.result?.nodes ?? []) if (n.stp?.isRoot) out.push(n.node);
    return out;
  }

  /** STP port badge for a given link endpoint. Reconciles the painter's port
   *  interface name (e.g. "Et0/0") against the lab doc's endpoint interface
   *  (e.g. "e0/0") via canonIface. Returns null if this endpoint has no painted
   *  STP port (node not running, or interface absent from the snapshot). */
  stpBadgeFor(nodeId: number, iface: string): StpEndpointBadge | null {
    if (!this.isStp) return null;
    const n = this.nodeIndex.get(nodeId);
    if (!n?.stp) return null;
    const want = canonIface(iface);
    for (const p of n.stp.ports) {
      // Prefer the backend's interfaceNorm, but canonicalize BOTH sides so
      // "Ethernet0/0" ≡ "Et0/0" ≡ "et0/0" ≡ "e0/0" all reconcile.
      if (canonIface(p.interfaceNorm || p.interface) === want) {
        return {
          role: STP_ROLE_TOKEN[p.role] ?? p.role,
          state: p.state,
          blocked: p.blocked,
          reason: p.reason,
        };
      }
    }
    return null;
  }

  /** Per-node "not running / no data" hint for a link endpoint, so FloatingEdge
   *  can surface the backend's guidance rather than fake a badge. */
  hintFor(nodeId: number): string | null {
    const n = this.nodeIndex.get(nodeId);
    if (n && !n.running) return n.hint || "node not running";
    return null;
  }

  /** Best-path edge paints for the current routing snapshot, keyed by link id.
   *  Resolves each running node's winning next-hop IP to a neighbour node via
   *  OSPF neighbor addresses, then maps (thisNode → neighbourNode) onto the lab
   *  link that connects them and records which endpoint is the downstream end.
   *
   *  Next-hop → neighbour resolution:
   *   - OSPF: `route.nextHop` is matched against each node's ospf.neighbors[].address.
   *   - EIGRP/BGP: the successor/best next-hop IP is matched the same way against
   *     the DOWNSTREAM node's interface address when the painter provides one; we
   *     fall back to matching the next-hop against ANY node's advertised neighbour
   *     address so a directly-connected next-hop still lights its edge.
   *  A next-hop we can't resolve to a node simply lights no edge (never faked). */
  private routingIndex = $derived.by(() => {
    const out = new Map<number, RoutingEdgePaint>();
    const res = this.result;
    if (!res || !(res.proto === "ospf" || res.proto === "eigrp" || res.proto === "bgp")) {
      return out;
    }

    // Build an address → nodeId map from every node's OSPF neighbour list AND
    // its own advertised addresses, so a next-hop IP can be traced to the node
    // that owns it. OSPF neighbours carry the PEER's address on the shared
    // segment, which is exactly the next-hop a route points at.
    const addrToNode = new Map<string, number>();
    for (const n of res.nodes) {
      for (const nb of n.ospf?.neighbors ?? []) {
        if (nb.address) addrToNode.set(nb.address, n.node);
      }
    }

    // For each running node with a winning next-hop, find the neighbour node and
    // the lab link joining them, then record the paint.
    for (const n of res.nodes) {
      const nextHop = winningNextHop(n);
      if (!nextHop) continue;
      const neighbourNode = addrToNode.get(nextHop);
      if (neighbourNode === undefined) continue;
      const link = findLink(n.node, neighbourNode);
      if (!link) continue;
      // Which endpoint is the downstream (next-hop) end?
      const toEndpoint: 0 | 1 = link.endpoints[1]?.node === neighbourNode ? 1 : 0;
      out.set(link.id, { linkId: link.id, toEndpoint, label: metricLabel(res.proto, n) });
    }
    return out;
  });

  /** Best-path paint for a link, or null if this link isn't on the winning path.
   *  FloatingEdge dims links that return null while a routing snapshot is up. */
  routingPaintFor(linkId: number): RoutingEdgePaint | null {
    if (!this.isRouting) return null;
    return this.routingIndex.get(linkId) ?? null;
  }

  /** BGP tiebreak reason for the painted prefix (shown once, in the panel). */
  get bgpReason(): string | null {
    if (this.result?.proto !== "bgp") return null;
    for (const n of this.result.nodes) if (n.bgp?.reason) return n.bgp.reason;
    return null;
  }
}

/** Painter role words → compact overlay tokens. */
const STP_ROLE_TOKEN: Record<string, string> = {
  Root: "R",
  Desg: "D",
  Altn: "ALT",
  Back: "BAK",
};

/** The winning next-hop IP for a node under the current routing proto, or "". */
function winningNextHop(n: PainterNode): string {
  if (n.ospf?.route?.nextHop) return n.ospf.route.nextHop;
  if (n.eigrp?.nextHop) return n.eigrp.nextHop;
  if (n.bgp?.bestNextHop) return n.bgp.bestNextHop;
  return "";
}

/** Short metric label for the winning path at a node. */
function metricLabel(proto: PainterProto, n: PainterNode): string {
  if (proto === "ospf" && n.ospf?.route) return `cost ${n.ospf.route.cost}`;
  if (proto === "eigrp" && n.eigrp) return `FD ${n.eigrp.fd}`;
  if (proto === "bgp" && n.bgp) {
    const best = n.bgp.paths.find((p) => p.best) ?? n.bgp.paths[0];
    const asp = best?.asPath?.trim();
    return asp ? `AS ${asp}` : "best";
  }
  return "";
}

/** Find the lab link joining two node ids (either endpoint order), or null. */
function findLink(a: number, b: number) {
  for (const l of labStore.lab.links) {
    const ns = l.endpoints.map((e) => e.node);
    if (ns.includes(a) && ns.includes(b)) return l;
  }
  return null;
}

export const painterStore = new PainterStore();
