// WebSocket transport (Track 3 / B2). Mirrors tcpTransport.ts but speaks the
// NDJSON control protocol (docs/protocol.md) over WebSocket *text* frames — one
// JSON object per frame. This is the browser build's transport: browsers can't
// open raw TCP, so the supervisor's WS bridge (B1) fronts the same protocol.
//
// The supervisor WS endpoint is being built in parallel; this client matches the
// documented framing. Default endpoint: ws://127.0.0.1:4001/control.
//
// TODO(B1/B2): verify against the real supervisor WS bridge once it lands —
// specifically (a) whether the bridge sends one JSON object per text frame
// (assumed here) or newline-delimited batches within a frame (handled too), and
// (b) any auth/hello handshake framing. Console/capture streams will use
// separate WS URLs provided in status/events, not this control socket.
import type { Request } from "./protocol";
import type { IncomingFrame, Transport } from "./transport";

export interface WsTransportOptions {
  /** Full ws:// or wss:// URL of the supervisor control bridge. */
  url?: string;
  /** Auto-reconnect with backoff on unexpected close. Default true. */
  reconnect?: boolean;
}

const DEFAULT_URL = "ws://127.0.0.1:4001/control";

export class WsTransport implements Transport {
  connected = false;
  private handlers = new Set<(frame: IncomingFrame) => void>();
  private ws: WebSocket | null = null;
  private url: string;
  private reconnect: boolean;
  private reconnectAttempts = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private intentionalClose = false;
  /** Frames queued while the socket is (re)connecting. */
  private outbox: string[] = [];

  constructor(opts: WsTransportOptions = {}) {
    this.url = opts.url ?? DEFAULT_URL;
    this.reconnect = opts.reconnect ?? true;
  }

  connect(): Promise<void> {
    this.intentionalClose = false;
    return new Promise<void>((resolve, reject) => {
      let settled = false;
      try {
        this.ws = new WebSocket(this.url);
      } catch (e) {
        reject(e instanceof Error ? e : new Error(String(e)));
        return;
      }

      this.ws.onopen = () => {
        this.connected = true;
        this.reconnectAttempts = 0;
        // Flush anything queued while connecting.
        for (const line of this.outbox) this.ws?.send(line);
        this.outbox = [];
        if (!settled) {
          settled = true;
          resolve();
        }
      };

      this.ws.onmessage = (ev) => this.onRaw(ev.data);

      this.ws.onerror = () => {
        if (!settled) {
          settled = true;
          reject(new Error(`WsTransport failed to connect to ${this.url}`));
        }
      };

      this.ws.onclose = () => {
        this.connected = false;
        if (!this.intentionalClose && this.reconnect) this.scheduleReconnect();
      };
    });
  }

  disconnect(): void {
    this.intentionalClose = true;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.connected = false;
    this.ws?.close();
    this.ws = null;
    this.handlers.clear();
    this.outbox = [];
  }

  onMessage(handler: (frame: IncomingFrame) => void): () => void {
    this.handlers.add(handler);
    return () => this.handlers.delete(handler);
  }

  send(req: Request): void {
    const line = JSON.stringify(req);
    if (this.ws && this.connected && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(line);
    } else {
      // Not open yet (initial connect / mid-reconnect): queue and flush on open.
      this.outbox.push(line);
    }
  }

  /** Parse an incoming text frame. Tolerates one-object-per-frame and NDJSON
   *  batches (multiple newline-separated objects in one frame). */
  private onRaw(data: unknown): void {
    if (typeof data !== "string") return; // control plane is text/JSON only
    for (const line of data.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      let frame: IncomingFrame;
      try {
        frame = JSON.parse(trimmed) as IncomingFrame;
      } catch {
        continue; // ignore malformed frames rather than tearing down the socket
      }
      for (const h of this.handlers) h(frame);
    }
  }

  private scheduleReconnect(): void {
    if (this.reconnectTimer) return;
    const delay = Math.min(30_000, 500 * 2 ** this.reconnectAttempts);
    this.reconnectAttempts++;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      if (this.intentionalClose) return;
      // Best-effort; failures reschedule via onclose/onerror.
      void this.connect().catch(() => {});
    }, delay);
  }
}
