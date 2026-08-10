// Typed client implementing docs/protocol.md over a pluggable Transport.
import type {
  CaptureStartResult,
  ConfigResult,
  HelloResult,
  ImageListResult,
  ImageRegisterResult,
  LabGetDocResult,
  LabListDocsResult,
  LabLoadResult,
  LabSaveDocResult,
  LabStartResult,
  LabWipeResult,
  NodeSetImageResult,
  StatusResult,
  SupervisorEvent,
  ToolListPacksResult,
} from "./protocol";
import type { LabDocument, LabLink, LabNode } from "./labTypes";
import type { PainterProto, PainterResult, StpVlansResult } from "./painterTypes";
import { labToYaml, labFromText } from "./yaml";
import { uuid } from "./uid";
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
  private reconnectHandlers = new Set<() => void>();
  private unsubscribe: (() => void) | null = null;
  private unsubscribeReconnect: (() => void) | null = null;

  constructor(private transport: Transport) {}

  get connected() {
    return this.transport.connected;
  }

  async connect(): Promise<HelloResult> {
    this.unsubscribe = this.transport.onMessage((frame) => this.onFrame(frame));
    this.unsubscribeReconnect = this.transport.onReconnect(() => this.handleTransportReconnect());
    await this.transport.connect();
    return this.call<HelloResult>("hello", { client: "iolbox-gui/0.1.0" });
  }

  disconnect() {
    this.unsubscribe?.();
    this.unsubscribe = null;
    this.unsubscribeReconnect?.();
    this.unsubscribeReconnect = null;
    this.transport.disconnect();
  }

  onEvent(handler: (evt: SupervisorEvent) => void): () => void {
    this.eventHandlers.add(handler);
    return () => this.eventHandlers.delete(handler);
  }

  /** Subscribe to transport reconnects (see Transport.onReconnect): any push
   *  event the server sent during the drop is gone for good, so a subscriber
   *  must re-sync its view of server state from scratch (re-query status,
   *  etc.) rather than assume it's still current. Runs AFTER stale pending
   *  calls are rejected (see onReconnect below), so a resync handler that
   *  itself calls back into this client never collides with a doomed
   *  pre-drop request sharing state. */
  onReconnect(handler: () => void): () => void {
    this.reconnectHandlers.add(handler);
    return () => this.reconnectHandlers.delete(handler);
  }

  private handleTransportReconnect() {
    // Every RPC sent before the drop is answered by a connection that no
    // longer exists — the response, if the server even finished computing
    // it, was written to a socket nobody is reading anymore. Without this,
    // those callers hang forever (call() never times out on its own).
    const stale = [...this.pending.values()];
    this.pending.clear();
    for (const entry of stale) {
      entry.reject(new SupervisorError("disconnected", "connection dropped and reconnected before this call completed"));
    }
    for (const h of this.reconnectHandlers) h();
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
    const id = uuid();
    return new Promise<R>((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.transport.send({ id, op, args });
    });
  }

  // ---- verbs ----

  imageList() {
    return this.call<ImageListResult>("image.list");
  }

  listPacks() {
    return this.call<ToolListPacksResult>("tool.listPacks");
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

  /** Deletes saved configs/state for the given nodes (or the whole lab when
   *  nodes is null). Destructive — callers must confirm with the user first. */
  labWipe(labId: string, nodes: number[] | null = null) {
    return this.call<LabWipeResult>("lab.wipe", { labId, nodes });
  }

  /** Force-clean: stop every tracked node + all relays/bridges/captures on the
   *  supervisor, regardless of labId. Clears orphaned runtime state (leaked
   *  relays, nodes still shown running) that a normal lab.stop might miss. */
  labReap() {
    return this.call<{ reaped: number }>("lab.reap", {});
  }

  // node.start/stop/restart all reply with the same {started:[...]} shape as
  // lab.start (see docs/protocol.md "Same shape as above" and
  // handleNodeStart/Stop/Restart -> startNodes in
  // supervisor/internal/server/handlers.go, which literally return
  // protocol.StartResult) — NOT a bare NodeRuntimeStatus. Actual node state
  // is driven by the pushed node.state/node.console events, not this reply.
  /** Register a node the GUI just added with the LOADED lab (incremental
   *  topology sync, the node counterpart of link.add). Without this, a node
   *  dropped after lab.load was unknown to the supervisor and could never
   *  start until a refresh. Returns the node's allocated console port. */
  nodeAdd(labId: string, node: LabNode) {
    return this.call<{ node: number; consolePort: number }>("node.add", { labId, node });
  }

  /** node.add's inverse: stop + deregister a node (and its links) from the
   *  loaded lab. */
  nodeRemove(labId: string, node: number) {
    return this.call<void>("node.remove", { labId, node });
  }

  nodeStart(labId: string, node: number) {
    return this.call<LabStartResult>("node.start", { labId, node });
  }

  nodeStop(labId: string, node: number) {
    return this.call<LabStartResult>("node.stop", { labId, node });
  }

  nodeRestart(labId: string, node: number) {
    return this.call<LabStartResult>("node.restart", { labId, node });
  }

  nodeSetImage(labId: string, node: number, imageId: string) {
    return this.call<NodeSetImageResult>("node.setImage", { labId, node, imageId });
  }

  linkAdd(labId: string, link: LabLink) {
    return this.call<void>("link.add", { labId, link });
  }

  linkRemove(labId: string, link: LabLink) {
    // Wire shape matches link.add: docs/protocol.md documents both verbs as
    // `{ "labId","link":<link.json> }`, and the Go handler unmarshals `link`
    // as a full lab.Link (only .id is read, but the field must decode as an
    // object, not a bare id) — see handleLinkRemove in
    // supervisor/internal/server/handlers.go.
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

  // ---- durable lab-document store ----
  // Labs persist as YAML text (iolbox's native format). This client is the YAML
  // boundary: it serialises on save and parses on read, so callers still work in
  // LabDocument terms. Docs that fail to parse are dropped from the list.
  labSaveDoc(lab: LabDocument) {
    return this.call<LabSaveDocResult>("lab.saveDoc", { lab: labToYaml(lab) });
  }

  async labListDocs(): Promise<LabListDocsResult> {
    const res = await this.call<{ labs: string[] }>("lab.listDocs", {});
    const labs: LabDocument[] = [];
    for (const text of res.labs ?? []) {
      try {
        labs.push(labFromText(text));
      } catch {
        // Skip a corrupt/unparseable stored doc rather than failing the whole list.
      }
    }
    return { labs };
  }

  async labGetDoc(labId: string): Promise<LabGetDocResult> {
    const res = await this.call<{ lab: string }>("lab.getDoc", { labId });
    return { lab: labFromText(res.lab) };
  }

  labDeleteDoc(labId: string) {
    return this.call<Record<string, never>>("lab.deleteDoc", { labId });
  }

  configExtract(labId: string, nodes: number[] | null = null) {
    return this.call<ConfigResult>("config.extract", { labId, nodes });
  }

  /** Topology Painter (WS5): one-shot live scrape + parse of a protocol's
   *  decision state across the running IOL nodes. `dest` is a prefix/host
   *  STRING (required for eigrp/bgp, optional for ospf, ignored for stp);
   *  `nodes` defaults to all running IOL nodes when omitted. `vlan` is
   *  REQUIRED (>0) when proto is "stp" — the backend rejects vlan<=0 for STP
   *  since spanning-tree is per-VLAN; ignored for the routing protocols. */
  painterCollect(
    labId: string,
    proto: PainterProto,
    dest?: string,
    nodes?: number[],
    vlan?: number
  ) {
    return this.call<PainterResult>("painter.collect", { labId, proto, dest, nodes, vlan });
  }

  /** STP VLAN discovery: which VLANs a given node currently runs spanning-tree
   *  on, so the painter panel can offer a VLAN picker before collecting. */
  painterStpVlans(labId: string, nodeId: number) {
    return this.call<StpVlansResult>("painter.stpVlans", { labId, nodeId });
  }

  status() {
    return this.call<StatusResult>("status");
  }
}
