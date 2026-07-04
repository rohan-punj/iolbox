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

/** Packet-type subtypes per protocol key, mirroring the backend
 *  ClassifyDetailed subtype vocabulary EXACTLY (see docs/protocol.md
 *  link.stats protosSubtypeDir). Only keys with a meaningful sub-discriminator
 *  appear here; a row whose proto isn't listed shows no subtype dropdown.
 *  `ping` and `icmp` share the ICMP subtype set (both back onto the PING/ICMP
 *  labels). The strings must match the backend byte-for-byte — they key into
 *  protosSubtypeDir[label][subtype]. */
export const SUBTYPES: Partial<Record<ProtoKey, string[]>> = {
  ping: ["echo-request", "echo-reply", "unreachable", "time-exceeded", "redirect", "other"],
  icmp: ["echo-request", "echo-reply", "unreachable", "time-exceeded", "redirect", "other"],
  bgp: ["open", "update", "notification", "keepalive", "route-refresh"],
  ospf: ["hello", "db-desc", "ls-request", "ls-update", "ls-ack"],
  eigrp: ["hello", "update", "query", "reply", "request"],
  arp: ["request", "reply"],
  stp: ["config", "tcn", "rstp"],
};

/** Display label for a subtype value in the packet-type dropdown. Most
 *  subtype strings (e.g. "echo-request", "hello") are readable as-is, so this
 *  only overrides the STP BPDU-type abbreviations that read better cased
 *  (matching PNetLab's Config/TCN/RSTP naming); anything absent here falls
 *  back to the raw subtype string. */
export const SUBTYPE_LABELS: Partial<Record<string, string>> = {
  config: "Config",
  tcn: "TCN",
  rstp: "RSTP",
};

export interface WatcherRow {
  id: string;
  proto: ProtoKey;
  color: string;
  /** Chosen packet type within the protocol, or "any" (no subtype filter).
   *  Only meaningful when SUBTYPES[proto] exists. */
  subtype: string;
}

/** Sentinel subtype meaning "no subtype filter" (match the whole protocol). */
export const SUBTYPE_ANY = "any";

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
 *  watcher needs, independent of how labStore stores the rest of the entry.
 *  `protosSubtypeDir` (label → subtype → [ep0,ep1] fps) is consulted only when
 *  a row picks a specific subtype; "any" rows match off protosDir alone. */
export interface StatsForMatch {
  protosDir?: Record<string, [number, number]>;
  protosSubtypeDir?: Record<string, Record<string, [number, number]>>;
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
  rows = $state<WatcherRow[]>([{ id: newRowId(), proto: "all", color: PALETTE[0], subtype: SUBTYPE_ANY }]);

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
    this.rows = [...this.rows, { id: newRowId(), proto: "all", color: this.nextFreeColor(), subtype: SUBTYPE_ANY }];
  }

  removeRow(id: string) {
    if (this.rows.length <= 1) return; // always keep at least one row
    this.rows = this.rows.filter((r) => r.id !== id);
  }

  setProto(id: string, proto: ProtoKey) {
    const row = this.rows.find((r) => r.id === id);
    if (row) {
      row.proto = proto;
      // A stale subtype from the previous protocol would never match — reset to
      // "any" whenever the protocol changes.
      row.subtype = SUBTYPE_ANY;
    }
  }

  /** Set a row's packet-type subtype filter (or SUBTYPE_ANY). No-op if the
   *  row's protocol has no subtypes — the dropdown isn't shown in that case. */
  setSubtype(id: string, subtype: string) {
    const row = this.rows.find((r) => r.id === id);
    if (row) row.subtype = subtype;
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
   *  is enough; no need to re-check the fps values here.
   *
   *  When a row picks a specific packet-type subtype (row.subtype !== "any"),
   *  the match is drawn from protosSubtypeDir instead: the row matches a
   *  direction only if one of the row's labels carries THAT subtype in that
   *  direction. A subtype-filtered row on a sample with no protosSubtypeDir
   *  (older supervisor, or that subtype simply absent) matches nothing — we
   *  never silently widen a subtype filter back to the whole protocol. */
  matchFor(stats: StatsForMatch | null | undefined): RowMatch[] {
    if (!stats?.protosDir) return [];
    const dir = stats.protosDir;
    const subDir = stats.protosSubtypeDir;
    const out: RowMatch[] = [];
    for (const row of this.rows) {
      const spec = LABELS[row.proto].labels;
      const filterSub = row.subtype !== SUBTYPE_ANY && SUBTYPES[row.proto] !== undefined;
      let dir0 = false;
      let dir1 = false;
      if (filterSub) {
        // Subtype filter: consult protosSubtypeDir[label][subtype] for each of
        // the row's labels. Absent entry → no match (nonzero-only contract).
        if (subDir) {
          for (const label of spec ?? []) {
            const st = subDir[label]?.[row.subtype];
            if (!st) continue;
            if (st[0] > 0) dir0 = true;
            if (st[1] > 0) dir1 = true;
          }
        }
      } else {
        for (const [label, [f0, f1]] of Object.entries(dir)) {
          if (spec && !spec.includes(label)) continue;
          if (f0 > 0) dir0 = true;
          if (f1 > 0) dir1 = true;
        }
      }
      if (dir0 || dir1) out.push({ row, dir0, dir1 });
    }
    return out;
  }
}

export const watcherStore = new WatcherStore();
