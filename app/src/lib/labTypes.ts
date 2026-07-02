// Hand-derived from contracts/lab.schema.json — keep property names in exact sync.
// Source of truth: J:\Claude code\iolab\contracts\lab.schema.json

export type NodeKind = "iol" | "vpcs";
export type ImageClass = "l2" | "l3" | "unknown";
export type LinkType = "p2p" | "segment";
export type CaptureMode = "live" | "file";
export type CanvasBackground = "grid" | "dots" | "blank";

/** Runtime node lifecycle state, per docs/protocol.md state machine. */
export type NodeState = "stopped" | "starting" | "running" | "crashed";

export interface ImageRef {
  /** Library image id (sha256 prefix). The node binds to THIS, enabling hot-swap. */
  id: string;
  /** Fallback filename if id is not found in the target library (import portability). */
  filename?: string;
  /** Cached hint; supervisor re-detects authoritatively. */
  class?: ImageClass;
}

export interface LabNode {
  /** Unique within lab. Also the NETMAP node index basis. */
  id: number;
  /** iol = Cisco IOL (L2 or L3). vpcs = virtual PC. */
  kind: NodeKind;
  name: string;
  x: number;
  y: number;
  /** Optional icon override; GUI defaults from image class. */
  icon?: string;
  /** IOL image reference. Required when kind=iol; ignored for vpcs. */
  image?: ImageRef;
  /** Megabytes. Default depends on image class. */
  ram?: number;
  /** Number of Ethernet adapter groups (each = 4 ports in IOL). */
  ethernet?: number;
  /** Number of Serial adapter groups (each = 4 ports). */
  serial?: number;
  /** Embedded day-0 config text (IOS CLI). Injected into NVRAM at boot. */
  startupConfig?: string;
  /** Reserved for kind-specific extras (e.g. vpcs canned commands). */
  config?: Record<string, unknown>;
}

export interface LabEndpoint {
  /** node.id */
  node: number;
  /** IOL: 'e0/0','s1/1' (adapter/port). VPCS: 'eth0'. */
  interface: string;
}

export interface LabLink {
  id: number;
  /** p2p = exactly 2 endpoints. segment = shared medium via userspace hub. */
  type?: LinkType;
  endpoints: LabEndpoint[];
  capture?: {
    enabled?: boolean;
    mode?: CaptureMode;
  };
}

export interface LabCanvas {
  zoom?: number;
  pan?: { x?: number; y?: number };
  background?: CanvasBackground;
}

export interface LabDocument {
  version: 1;
  /** Stable unique lab id (uuid-ish string). */
  id: string;
  name: string;
  description?: string;
  /** ISO8601; set by GUI. */
  created?: string;
  /** ISO8601; set by GUI. */
  modified?: string;
  canvas?: LabCanvas;
  nodes: LabNode[];
  links: LabLink[];
}

/** Library image metadata, as returned by image.list / image.register. */
export interface LibraryImage {
  id: string;
  filename: string;
  class: ImageClass;
  arch: "i386" | "x86_64" | string;
  sha256: string;
  size: number;
}

export function emptyLab(name = "Untitled lab"): LabDocument {
  const now = new Date().toISOString();
  return {
    version: 1,
    id: crypto.randomUUID(),
    name,
    description: "",
    created: now,
    modified: now,
    canvas: { zoom: 1, pan: { x: 0, y: 0 }, background: "dots" },
    nodes: [],
    links: [],
  };
}
