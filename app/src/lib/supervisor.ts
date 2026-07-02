// Typed client implementing docs/protocol.md over a pluggable Transport.
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
  SupervisorEvent,
} from "./protocol";
import type { LabDocument, LabLink } from "./labTypes";
import { isEvent, isResponse, type Transport } from "./transport";

type PendingEntry = {
  resolve: (v: any) => void;
  reject: (e: Error) => void;
};

export class SupervisorError extends Error {
  code: string;
  constructor(code: string, message: string) {
    super(message);
    this.code = code;
    this.name = "SupervisorError";
  }
}

export class SupervisorClient {
  private pending = new Map<string, PendingEntry>();
  private eventHandlers = new Set<(evt: SupervisorEvent) => void>();
  private unsubscribe: (() => void) | null = null;

  constructor(private transport: Transport) {}

  get connected() {
    return this.transport.connected;
  }

  async connect(): Promise<HelloResult> {
    this.unsubscribe = this.transport.onMessage((frame) => this.onFrame(frame));
    await this.transport.connect();
    return this.call<HelloResult>("hello", { client: "iolab-gui/0.1.0" });
  }

  disconnect() {
    this.unsubscribe?.();
    this.unsubscribe = null;
    this.transport.disconnect();
  }

  onEvent(handler: (evt: SupervisorEvent) => void): () => void {
    this.eventHandlers.add(handler);
    return () => this.eventHandlers.delete(handler);
  }

  private onFrame(frame: Parameters<Parameters<Transport["onMessage"]>[0]>[0]) {
    if (isResponse(frame)) {
      const entry = this.pending.get(frame.id);
      if (!entry) return;
      this.pending.delete(frame.id);
      if (frame.ok) entry.resolve(frame.result);
      else entry.reject(new SupervisorError(frame.error.code, frame.error.message));
      return;
    }
    if (isEvent(frame)) {
      for (const h of this.eventHandlers) h(frame);
    }
  }

  private call<R>(op: string, args?: unknown): Promise<R> {
    const id = crypto.randomUUID();
    return new Promise<R>((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.transport.send({ id, op, args });
    });
  }

  // ---- verbs ----

  imageList() {
    return this.call<ImageListResult>("image.list");
  }

  imageRegister(path: string) {
    return this.call<ImageRegisterResult>("image.register", { path });
  }

  imageRemove(id: string) {
    return this.call<void>("image.remove", { id });
  }

  labLoad(lab: LabDocument) {
    return this.call<LabLoadResult>("lab.load", { lab });
  }

  labStart(labId: string, nodes: number[] | null = null) {
    return this.call<LabStartResult>("lab.start", { labId, nodes });
  }

  labStop(labId: string, nodes: number[] | null = null) {
    return this.call<void>("lab.stop", { labId, nodes });
  }

  nodeStart(labId: string, node: number) {
    return this.call<NodeRuntimeStatus>("node.start", { labId, node });
  }

  nodeStop(labId: string, node: number) {
    return this.call<NodeRuntimeStatus>("node.stop", { labId, node });
  }

  nodeRestart(labId: string, node: number) {
    return this.call<NodeRuntimeStatus>("node.restart", { labId, node });
  }

  nodeSetImage(labId: string, node: number, imageId: string) {
    return this.call<NodeSetImageResult>("node.setImage", { labId, node, imageId });
  }

  linkAdd(labId: string, link: LabLink) {
    return this.call<void>("link.add", { labId, link });
  }

  linkRemove(labId: string, link: number) {
    return this.call<void>("link.remove", { labId, link });
  }

  captureStart(labId: string, link: number, mode: "live" | "file" = "live", file?: string) {
    return this.call<CaptureStartResult>("capture.start", { labId, link, mode, file });
  }

  captureStop(labId: string, link: number) {
    return this.call<void>("capture.stop", { labId, link });
  }

  configSave(labId: string, nodes: number[] | null = null) {
    return this.call<ConfigResult>("config.save", { labId, nodes });
  }

  configExtract(labId: string, nodes: number[] | null = null) {
    return this.call<ConfigResult>("config.extract", { labId, nodes });
  }

  status() {
    return this.call<StatusResult>("status");
  }
}
