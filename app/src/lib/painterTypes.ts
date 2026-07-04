// Topology Painter (WS5b) — the exact JSON shape the `painter.collect` verb
// returns (WS5a backend contract), plus the one shared interface canonicalizer
// used on BOTH sides of the port↔endpoint reconciliation.
//
// Contract (from WS5a): a PainterResult carries `proto`, `dest`, and a `nodes[]`
// array; each node has at most ONE per-proto payload (matching `proto`), or
// `running:false` + `hint` when it has no live data. `omitempty` on the backend
// means absent fields arrive as `undefined` here.
//
// STP is VLAN-scoped (backend redesign): `painter.collect` for proto "stp"
// REQUIRES `vlan > 0` and the result echoes it back on `vlan`; `isRoot` is
// authoritative and AT MOST ONE node per collect carries `isRoot:true`. The
// node→VLAN discovery step is a separate verb, `painter.stpVlans`.

export type PainterProto = "stp" | "ospf" | "eigrp" | "bgp";

// ---- STP ----
export interface StpPort {
  interface: string;
  interfaceNorm: string;
  /** Root | Desg | Altn | Back */
  role: string;
  /** FWD | BLK | LRN | LIS | DIS */
  state: string;
  cost: number;
  prio: number;
  blocked: boolean;
  /** Only present on blocked ports — the student-readable "why". */
  reason?: string;
}
export interface StpData {
  /** The VLAN this snapshot's spanning-tree instance runs on. */
  vlan: number;
  rootId: string;
  bridgeId: string;
  isRoot: boolean;
  rootCost: number;
  rootPort: string;
  ports: StpPort[];
}

// ---- STP VLAN discovery (`painter.stpVlans`) ----
export interface StpVlan {
  id: number;
  name: string;
}
export interface StpVlansResult {
  node: number;
  running: boolean;
  vlans: StpVlan[];
  /** Non-empty when the node isn't running / has no STP / VLAN data. */
  hint: string;
}

// ---- OSPF ----
export interface OspfNeighbor {
  neighborId: string;
  state: string;
  /** DR | BDR | DROTHER */
  role: string;
  address: string;
  interface: string;
  interfaceNorm: string;
}
export interface OspfRoute {
  prefix: string;
  nextHop: string;
  interface: string;
  interfaceNorm: string;
  cost: number;
}
export interface OspfData {
  neighbors: OspfNeighbor[];
  route?: OspfRoute;
}

// ---- EIGRP ----
export interface EigrpPath {
  nextHop: string;
  interface: string;
  interfaceNorm: string;
  fd: number;
  rd: number;
  successor: boolean;
  feasibleSuccessor: boolean;
}
export interface EigrpData {
  prefix: string;
  fd: number;
  nextHop: string;
  paths: EigrpPath[];
}

// ---- BGP ----
export interface BgpPath {
  nextHop: string;
  asPath: string;
  /** i | e | ? */
  origin: string;
  weight: number;
  localPref: number;
  med: number;
  best: boolean;
}
export interface BgpData {
  prefix: string;
  bestNextHop: string;
  reason: string;
  paths: BgpPath[];
}

// ---- per-node envelope ----
export interface PainterNode {
  node: number;
  running: boolean;
  /** Non-empty when the node has no usable data (not running / still
   *  converging); the frontend shows this instead of faking a badge. */
  hint: string;
  stp?: StpData;
  ospf?: OspfData;
  eigrp?: EigrpData;
  bgp?: BgpData;
}

export interface PainterResult {
  proto: PainterProto;
  dest: string;
  /** Echoed back for proto "stp" — the VLAN this snapshot was collected for.
   *  Absent/0 for the routing protocols. */
  vlan?: number;
  nodes: PainterNode[];
}

/**
 * ONE robust interface canonicalizer, used on BOTH the painter port names and
 * the lab-doc endpoint interface names so they reconcile despite IOS naming
 * variance. The painter emits `Et0/0` / interfaceNorm `et0/0`; lab docs use the
 * short IOS-ish `e0/0`; full IOS is `Ethernet0/0`. All of these — plus
 * Serial/GigabitEthernet/Loopback variants — must map to the same token.
 *
 * Strategy: lowercase, strip a leading alpha run (the media prefix:
 * "ethernet", "et", "e", "serial", "se", "s", "gigabitethernet", "gi", "lo",
 * …), then keep the numeric adapter/port tail (e.g. "0/0", "1/1", "0"). Two
 * names are the same port iff their numeric tails match. We deliberately do NOT
 * try to distinguish media *type* (ethernet vs serial) because within one IOL
 * node the adapter/port tuple is already unique across media, and the painter
 * and lab doc always agree on the media for a given endpoint.
 */
export function canonIface(name: string | undefined | null): string {
  if (!name) return "";
  const s = String(name).trim().toLowerCase();
  // Strip a leading alpha media prefix; keep the numeric tail (digits + '/').
  const m = s.match(/^[a-z]*\s*([0-9].*)$/);
  const tail = (m ? m[1] : s).replace(/\s+/g, "");
  return tail;
}
