// Types for docs/protocol.md — the NDJSON control protocol spoken to the supervisor.
import type { ImageClass, LabDocument, LabLink, LibraryImage, NodeState } from "./labTypes";

export interface Request<A = unknown> {
  id: string;
  op: string;
  args?: A;
}

export interface ResponseOk<R = unknown> {
  id: string;
  ok: true;
  result: R;
}

export interface ResponseErr {
  id: string;
  ok: false;
  error: { code: string; message: string };
}

export type Response<R = unknown> = ResponseOk<R> | ResponseErr;

export type ErrorCode =
  | "schema_invalid"
  | "image_not_found"
  | "image_arch_mismatch"
  | "iourc_failed"
  | "node_spawn_failed"
  | "port_unavailable"
  | "nvram_codec_failed"
  | "not_loaded"
  | "unsupported";

// ---- Events (server -> GUI push) ----

export interface NodeStateEvent {
  event: "node.state";
  data: { node: number; state: NodeState };
}
export interface NodeConsoleEvent {
  event: "node.console";
  data: { node: number; consolePort: number };
}
export interface LinkUpEvent {
  event: "link.up";
  data: { link: number };
}
export interface LinkDownEvent {
  event: "link.down";
  data: { link: number };
}
export interface CaptureStartedEvent {
  event: "capture.started";
  data: { link: number; capturePort: number };
}
export interface CaptureStoppedEvent {
  event: "capture.stopped";
  data: { link: number };
}
export interface LogEvent {
  event: "log";
  data: { level: "debug" | "info" | "warn" | "error"; message: string; node?: number };
}
/** Per-link forwarded throughput over the last ~2s. Bridged (relay-backed)
 *  links only — native IOL↔IOL links never emit this (see docs/protocol.md).
 *  `protos` (Network Watcher) is a frames-per-second breakdown by protocol
 *  label (e.g. "STP", "CDP", "ICMP", "OSPF", "TCP"); non-zero entries only,
 *  capped at 6 by the sender — optional so older supervisors keep working.
 *  `protosDir` is the directional variant: per label, [fps of frames sourced
 *  from the link's doc endpoints[0], fps sourced from endpoints[1]] — nonzero
 *  labels only. Drives the watcher's directional dash overlays.
 *  `protosSubtypeDir` is the same directional breakdown one level deeper:
 *  label → subtype (e.g. BGP "keepalive", ICMP "echo-request") → [ep0, ep1]
 *  fps — nonzero only. Lets the watcher filter by packet type. */
export interface LinkStatsEvent {
  event: "link.stats";
  data: {
    link: number;
    fps: number;
    bps: number;
    protos?: Record<string, number>;
    protosDir?: Record<string, [number, number]>;
    protosSubtypeDir?: Record<string, Record<string, [number, number]>>;
  };
}
/** Runtime VM resource utilisation, pushed every ~2s for the host monitor. */
export interface HostStatsEvent {
  event: "host.stats";
  data: {
    cpuPct: number;
    memUsed: number;
    memTotal: number;
    diskUsed: number;
    diskTotal: number;
    cores: number;
  };
}

export type SupervisorEvent =
  | NodeStateEvent
  | NodeConsoleEvent
  | LinkUpEvent
  | LinkDownEvent
  | CaptureStartedEvent
  | CaptureStoppedEvent
  | LinkStatsEvent
  | HostStatsEvent
  | LogEvent;

// ---- Result shapes per verb ----

export interface HelloResult {
  supervisor: string;
  runtime: string;
  arch: string;
  features: string[];
  // WS6 — internet-egress capability of the runtime's NAT path. "slirp" (QEMU
  // user-mode NAT) terminates ICMP → ping/traceroute to the internet do NOT
  // work through the NAT node; "routed" is a real host NAT/bridge (full).
  // Always present on the WS6 supervisor; treat absence as "routed".
  egress?: "slirp" | "routed";
  // Human-readable note; only sent when egress === "slirp".
  egressNote?: string;
}

export interface ImageListResult {
  images: LibraryImage[];
}

export interface ImageRegisterResult {
  id: string;
  class: ImageClass;
  arch: string;
  sha256: string;
}

/** Wire-safe metadata returned by tool.listPacks. Keep these fields aligned
 * with supervisor/internal/protocol/verbs.go; module fields and mitigations
 * intentionally stay inside the proxied pack GUI for this frontend slice. */
export interface ToolListPacksResult {
  packs: ToolPackInfo[];
}

export interface ToolPackInfo {
  id: string;
  name: string;
  icon: string;
  transport: string;
  groups: string[];
  modules: ToolModuleInfo[];
}

export interface ToolModuleInfo {
  key: string;
  label: string;
  group: string;
}

export interface LabLoadResult {
  labId: string;
  nodes: { id: number; consolePort: number }[];
  warnings: string[];
  /** WS2: true when the supervisor matched this load against the ALREADY
   *  RUNNING lab (same id, same topology) and serviced it without any
   *  teardown — the returned node console ports are the EXISTING runtime's,
   *  not freshly allocated. loadLab() must not reset nodeStates to all-
   *  "stopped" in this case (see labStore.svelte.ts). Absent/false on every
   *  ordinary load. */
  adopted?: boolean;
}

export interface NodeRuntimeStatus {
  node: number;
  consolePort: number;
  pid: number;
  state: NodeState;
}

export interface LabStartResult {
  started: NodeRuntimeStatus[];
}

export interface LabWipeResult {
  wiped: number[];
}

export interface NodeSetImageResult {
  node: number;
  imageId: string;
  class: ImageClass;
}

export interface CaptureStartResult {
  link: number;
  capturePort: number;
  file?: string;
}

export interface ConfigResult {
  configs: { node: number; startupConfig: string }[];
}

export interface StatusResult {
  labId: string | null;
  nodes: {
    id: number;
    state: NodeState;
    consolePort?: number;
    pid?: number;
    ram?: number;
    image?: string;
  }[];
  links: { id: number; capturePort?: number }[];
}

export interface LinkAddArgs {
  labId: string;
  link: LabLink;
}

// ---- Durable lab-document store (lab.saveDoc / listDocs / getDoc / deleteDoc) ----
// Distinct from the runtime lab.load path: these persist full docs to disk.
export interface LabSaveDocResult {
  id: string;
}
export interface LabListDocsResult {
  labs: LabDocument[];
}
export interface LabGetDocResult {
  lab: LabDocument;
}
