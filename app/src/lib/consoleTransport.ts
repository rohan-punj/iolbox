// Real node-console WS client, one instance per open console tab.
//
// Framing verified against supervisor/internal/wsbridge/wsbridge.go
// (handleConsole/bridgeConsole): GET /console/{nodeId} upgrades to WS, then
//   - node -> browser: raw terminal bytes (post telnet-IAC negotiation) as
//     BINARY frames.
//   - browser -> node: keystrokes as BINARY frames, written straight through;
//     a resize request is a TEXT frame `{"resize":{"cols":C,"rows":R}}`,
//     translated server-side to a NAWS subnegotiation (never forwarded as
//     terminal data).
// Same-origin URL, derived from the page's own host — the supervisor's
// static+WS bridge serves both from one listener (see transportSelect.ts).
import { sameOriginControlUrl } from "./transportSelect";

export interface ConsoleTransportHandlers {
  onData(bytes: Uint8Array): void;
  onOpen?(): void;
  onClose?(): void;
  onError?(err: unknown): void;
}

/** Builds ws(s)://<host>/console/<nodeId> from the same origin as /control. */
export function consoleUrl(nodeId: number): string {
  // sameOriginControlUrl() ends in "/control"; swap the path segment rather
  // than re-deriving proto/host so the two stay in lockstep by construction.
  return sameOriginControlUrl().replace(/\/control$/, `/console/${nodeId}`);
}

const BACKOFF_BASE_MS = 1000;
const BACKOFF_CAP_MS = 5000;

// RECONNECT: like captureTransport, connect() used to be ONE-SHOT — a console
// tab opened moments before its node's telnet listener came up (lab starting,
// node restarting) dialed once, got the 502/close, and was dead until the tab
// was cycled. The transport now retries with capped backoff while the tab is
// open, and retryNow() collapses the backoff when the app KNOWS the node just
// came up (node.state -> running). Each (re)connection is a fresh telnet
// session server-side; the console hub replays recent output so the terminal
// repaints its context. Consumers reset per-stream state (colorizer) in onOpen.
export class ConsoleTransport {
  private ws: WebSocket | null = null;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private attempt = 0;
  private stopped = false;
  connected = false;
  private nodeId: number;
  private handlers: ConsoleTransportHandlers;

  constructor(nodeId: number, handlers: ConsoleTransportHandlers) {
    this.nodeId = nodeId;
    this.handlers = handlers;
  }

  /** Open (or re-open) the console stream; keeps retrying until disconnect(). */
  connect(): void {
    this.stopped = false;
    this.dial();
  }

  /** Collapse the backoff and reconnect immediately — used when the node just
   *  reported running (its console listener is bound before spawn returns). */
  retryNow(): void {
    if (this.stopped || this.connected) return;
    this.attempt = 0;
    this.clearRetry();
    this.dial();
  }

  private dial(): void {
    if (this.stopped || this.ws) return;
    const ws = new WebSocket(consoleUrl(this.nodeId));
    ws.binaryType = "arraybuffer";
    this.ws = ws;

    ws.onopen = () => {
      this.connected = true;
      this.attempt = 0;
      this.handlers.onOpen?.();
    };
    ws.onmessage = (ev) => {
      if (ev.data instanceof ArrayBuffer) {
        this.handlers.onData(new Uint8Array(ev.data));
      }
      // Text frames on this socket are server->client control messages, if
      // any are ever added; none are currently sent (see wsbridge.go).
    };
    ws.onerror = (ev) => this.handlers.onError?.(ev);
    ws.onclose = () => {
      const wasConnected = this.connected;
      this.connected = false;
      this.ws = null;
      this.handlers.onClose?.();
      if (this.stopped) return;
      if (wasConnected) this.attempt = 0;
      this.scheduleRetry();
    };
  }

  private scheduleRetry(): void {
    if (this.retryTimer !== null) return;
    const delay = Math.min(BACKOFF_BASE_MS * 2 ** this.attempt, BACKOFF_CAP_MS);
    this.attempt += 1;
    this.retryTimer = setTimeout(() => {
      this.retryTimer = null;
      this.dial();
    }, delay);
  }

  private clearRetry(): void {
    if (this.retryTimer !== null) {
      clearTimeout(this.retryTimer);
      this.retryTimer = null;
    }
  }

  /** Send raw keystrokes as a binary frame. */
  sendInput(data: string): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    this.ws.send(new TextEncoder().encode(data));
  }

  /** Send a NAWS resize request as the one text-frame control message. */
  sendResize(cols: number, rows: number): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    this.ws.send(JSON.stringify({ resize: { cols, rows } }));
  }

  disconnect(): void {
    this.stopped = true;
    this.clearRetry();
    const ws = this.ws;
    this.ws = null; // onclose sees stopped=true and does not retry
    ws?.close();
    this.connected = false;
  }
}
