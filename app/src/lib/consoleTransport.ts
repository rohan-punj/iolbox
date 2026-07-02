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

export class ConsoleTransport {
  private ws: WebSocket | null = null;
  connected = false;

  constructor(
    private nodeId: number,
    private handlers: ConsoleTransportHandlers
  ) {}

  connect(): void {
    const ws = new WebSocket(consoleUrl(this.nodeId));
    ws.binaryType = "arraybuffer";
    this.ws = ws;

    ws.onopen = () => {
      this.connected = true;
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
      this.connected = false;
      this.handlers.onClose?.();
    };
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
    this.ws?.close();
    this.ws = null;
    this.connected = false;
  }
}
