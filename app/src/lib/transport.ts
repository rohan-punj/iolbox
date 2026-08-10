// Transport interface: how the supervisor client moves NDJSON frames.
// Two implementations: MockTransport (in-memory, drives the whole UI with no
// backend) and TcpTransport (real one, wired via Tauri invoke later).
import type { Request, Response, SupervisorEvent } from "./protocol";

export type IncomingFrame = Response | SupervisorEvent;

export interface Transport {
  /** Send one request frame. Fire-and-forget at this layer; correlation is by id. */
  send(req: Request): void;
  /** Subscribe to all incoming frames (responses + pushed events). */
  onMessage(handler: (frame: IncomingFrame) => void): () => void;
  /** Connect/open. Resolves once ready to send. */
  connect(): Promise<void>;
  /** Tear down. */
  disconnect(): void;
  readonly connected: boolean;
  /** Subscribe to reconnects: fires when connectivity is re-established after
   *  having been lost (never on the initial connect()). Any event the server
   *  pushed while disconnected is gone for good — there is no replay — so a
   *  subscriber must treat this as "assume our view is stale, re-sync from
   *  scratch" rather than "resume where we left off". Implementations that
   *  never disconnect on their own (MockTransport) may no-op. */
  onReconnect(handler: () => void): () => void;
}

export function isResponse(frame: IncomingFrame): frame is Response {
  return "id" in frame && "ok" in frame;
}

export function isEvent(frame: IncomingFrame): frame is SupervisorEvent {
  return "event" in frame;
}
