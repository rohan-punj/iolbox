// Network Watcher (PNetLab-style) — rune-backed singleton, same idiom as
// consoleUiStore. Unlike the old per-link chip (consoleUiStore.watcher, now
// removed), this drives a floating filter panel: the user picks up to 4
// protocol rows, each with its own colour, and the canvas overlays directional
// animated dashes + a midpoint label pill on every link currently carrying
// that protocol.
//
// Session-only: rows/running reset on reload (there's no use case for a stale
// watcher silently re-arming with yesterday's filters). Only panelOpen would
// be worth persisting, but even that defaults closed — matches the old
// watcher pref's default-off opt-in feel.

/** Dropdown entries, in display order. "all" has no label filter (matches any
 *  traffic on the link); every other entry maps to one or more backend
 *  link.stats protosDir labels (see docs/protocol.md link.stats event). */
export type ProtoKey =
  | "all"
  | "arp"
  | "ospf"
  | "bgp"
  | "eigrp"
  | "isis"
  | "rip"
  | "ping"
  | "icmp"
  | "stp"
  | "cdp"
  | "lldp"
  | "vxlan"
  | "gre"
  | "ipsec"
  | "radius"
  | "tacacs"
  | "dot1q";

export const LABELS: Record<ProtoKey, { name: string; labels: string[] | null }> = {
  all: { name: "All traffic", labels: null },
  arp: { name: "ARP", labels: ["ARP"] },
  ospf: { name: "OSPF", labels: ["OSPF"] },
  bgp: { name: "BGP", labels: ["BGP"] },
  eigrp: { name: "EIGRP", labels: ["EIGRP"] },
  isis: { name: "IS-IS", labels: ["ISIS"] },
  rip: { name: "RIP", labels: ["RIP"] },
  ping: { name: "Ping (echo)", labels: ["PING"] },
  icmp: { name: "ICMP (all)", labels: ["PING", "ICMP", "ICMPv6"] },
  stp: { name: "STP / BPDU", labels: ["STP"] },
  cdp: { name: "CDP", labels: ["CDP"] },
  lldp: { name: "LLDP", labels: ["LLDP"] },
  vxlan: { name: "VXLAN", labels: ["VXLAN"] },
  gre: { name: "GRE", labels: ["GRE"] },
  ipsec: { name: "IPsec", labels: ["ESP", "AH"] },
  radius: { name: "RADIUS", labels: ["RADIUS"] },
  tacacs: { name: "TACACS+", labels: ["TACACS"] },
  dot1q: { name: "802.1Q tagged", labels: ["DOT1Q"] },
};

/** Dropdown order — Object.keys(LABELS) would work too, but an explicit list
 *  keeps the order stable regardless of how LABELS gets edited later. */
export const PROTO_ORDER: ProtoKey[] = [
  "all", "arp", "ospf", "bgp", "eigrp", "isis", "rip", "ping", "icmp",
  "stp", "cdp", "lldp", "vxlan", "gre", "ipsec", "radius", "tacacs", "dot1q",
];

export interface WatcherRow {
  id: string;
  proto: ProtoKey;
  color: string;
}

/** Four visually-distinct overlay colours (purple/cyan/amber/green) — chosen
 *  to read clearly as dashed strokes over the cable colour and against both
 *  themes, and to stay distinct from the existing traffic-glow accent tint. */
const PALETTE = ["#b478e0", "#4fc3d9", "#e0a63c", "#5fbf7a"];
const MAX_ROWS = 4;

let rowSeq = 0;
function newRowId(): string {
  return `row${rowSeq++}`;
}

/** Shape of one link.stats sample as read by matchFor — the fields the
 *  watcher needs, independent of how labStore stores the rest of the entry. */
export interface StatsForMatch {
  protosDir?: Record<string, [number, number]>;
}

export interface RowMatch {
  row: WatcherRow;
  dir0: boolean; // traffic sourced from endpoints[0] (edge "source")
  dir1: boolean; // traffic sourced from endpoints[1] (edge "target")
}

class WatcherStore {
  /** Floating panel visibility. Session-only, default closed. */
  panelOpen = $state(false);
  /** Whether overlays are actively drawn on the canvas. Distinct from
   *  panelOpen — the user can close the panel while leaving the watch running,
   *  or open the panel to edit rows without redrawing overlays yet. */
  running = $state(false);
  rows = $state<WatcherRow[]>([{ id: newRowId(), proto: "all", color: PALETTE[0] }]);

  get canAddRow(): boolean {
    return this.rows.length < MAX_ROWS;
  }

  /** First palette colour not already in use by another row, falling back to
   *  the first entry if all four are taken (shouldn't happen — MAX_ROWS caps
   *  at PALETTE.length). */
  private nextFreeColor(): string {
    const used = new Set(this.rows.map((r) => r.color));
    return PALETTE.find((c) => !used.has(c)) ?? PALETTE[0];
  }

  addRow() {
    if (!this.canAddRow) return;
    this.rows = [...this.rows, { id: newRowId(), proto: "all", color: this.nextFreeColor() }];
  }

  removeRow(id: string) {
    if (this.rows.length <= 1) return; // always keep at least one row
    this.rows = this.rows.filter((r) => r.id !== id);
  }

  setProto(id: string, proto: ProtoKey) {
    const row = this.rows.find((r) => r.id === id);
    if (row) row.proto = proto;
  }

  /** Set a row's overlay colour (native colour-picker input in the panel).
   *  New rows still auto-assign distinct PALETTE colours; this lets the user
   *  override to anything. */
  setColor(id: string, color: string) {
    const row = this.rows.find((r) => r.id === id);
    if (row) row.color = color;
  }

  start() {
    this.running = true;
  }

  /** Stop clears highlights — FloatingEdge gates all overlay rendering on
   *  `running`, so flipping this off is sufficient to remove them everywhere. */
  stop() {
    this.running = false;
  }

  togglePanel() {
    this.panelOpen = !this.panelOpen;
  }

  /** For a link's latest stats sample, which rows match and in which
   *  direction(s). "All traffic" (labels === null) matches as soon as ANY
   *  label carries traffic in that direction — an entry only appears in
   *  protosDir when its fps is nonzero (backend contract), so presence alone
   *  is enough; no need to re-check the fps values here. */
  matchFor(stats: StatsForMatch | null | undefined): RowMatch[] {
    if (!stats?.protosDir) return [];
    const dir = stats.protosDir;
    const out: RowMatch[] = [];
    for (const row of this.rows) {
      const spec = LABELS[row.proto].labels;
      let dir0 = false;
      let dir1 = false;
      for (const [label, [f0, f1]] of Object.entries(dir)) {
        if (spec && !spec.includes(label)) continue;
        if (f0 > 0) dir0 = true;
        if (f1 > 0) dir1 = true;
      }
      if (dir0 || dir1) out.push({ row, dir0, dir1 });
    }
    return out;
  }
}

export const watcherStore = new WatcherStore();
