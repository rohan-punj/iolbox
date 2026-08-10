// WebSocket transport. Speaks the NDJSON control protocol (docs/protocol.md)
// over WebSocket *text* frames — one JSON object per frame, no trailing
// newline. This is the browser build's transport: browsers can't open raw
// TCP, so the supervisor's WS bridge (internal/wsbridge) fronts the same
// protocol handled on the TCP listener.
//
// Verified against supervisor/internal/wsbridge/wsbridge.go: GET /control
// upgrades to WS and runs the shared NDJSON control loop over a
// textFrameRWC — each WS text frame carries exactly one JSON object (no
// newline-delimited batching, no hello/auth handshake beyond the normal
// `hello` verb request/response). Plain RFC 6455, no subprotocol.
//
// Console streams (GET /console/{nodeId}) are a separate WS URL/framing
// (binary frames for terminal bytes, text frames for {"resize":...}) — see
// consoleTransport.ts, not this control socket.
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
  private reconnectHandlers = new Set<() => void>();
  private ws: WebSocket | null = null;
  private url: string;
  private reconnect: boolean;
  private reconnectAttempts = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private intentionalClose = false;
  /** True once this transport has completed at least one connect(); a later
   *  successful open is therefore a RECONNECT (see onopen). */
  private everConnected = false;
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
        // A prior connect() already completed (see everConnected) => this open
        // is a RECONNECT after a drop, not the app's initial connect. Nothing
        // pushed by the server during the gap was buffered anywhere (the
        // server has no per-client replay log — see broadcaster.publish), so
        // a subscriber's cached view of server state (node run-states, etc.)
        // can be silently stale relative to reality. Tell them to re-sync.
        if (this.everConnected) {
          for (const h of this.reconnectHandlers) h();
        }
        this.everConnected = true;
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
    this.reconnectHandlers.clear();
    this.outbox = [];
    // An intentional disconnect ends this transport's session; a later
    // connect() (a fresh session, not a network drop) must not fire
    // reconnect handlers no one has re-subscribed yet.
    this.everConnected = false;
  }

  onMessage(handler: (frame: IncomingFrame) => void): () => void {
    this.handlers.add(handler);
    return () => this.handlers.delete(handler);
  }

  onReconnect(handler: () => void): () => void {
    this.reconnectHandlers.add(handler);
    return () => this.reconnectHandlers.delete(handler);
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
