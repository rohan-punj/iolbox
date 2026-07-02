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

export type SupervisorEvent =
  | NodeStateEvent
  | NodeConsoleEvent
  | LinkUpEvent
  | LinkDownEvent
  | CaptureStartedEvent
  | CaptureStoppedEvent
  | LogEvent;

// ---- Result shapes per verb ----

export interface HelloResult {
  supervisor: string;
  runtime: string;
  arch: string;
  features: string[];
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

export interface LabLoadResult {
  labId: string;
  nodes: { id: number; consolePort: number }[];
  warnings: string[];
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
