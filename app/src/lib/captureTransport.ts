// Live-capture WS client, one per open capture tab (feature 1). Connects to
// GET /capture/{linkId}, which upgrades to a WebSocket carrying a raw pcapng
// byte stream (binary frames) for the link's active capture. A 404 (JSON body)
// before the upgrade means the link has no active capture — the socket simply
// fails to open, surfaced via onError/onClose so the tab can show its hint.
//
// Same-origin URL, derived from the page's own host, exactly like
// consoleTransport.ts (both are served by the supervisor's one WS bridge).
import { sameOriginControlUrl } from "./transportSelect";

export interface CaptureTransportHandlers {
  onData(bytes: Uint8Array): void;
  onOpen?(): void;
  onClose?(): void;
  onError?(err: unknown): void;
}

/** Builds ws(s)://<host>/capture/<linkId> from the same origin as /control. */
export function captureUrl(linkId: number): string {
  return sameOriginControlUrl().replace(/\/control$/, `/capture/${linkId}`);
}

export class CaptureTransport {
  private ws: WebSocket | null = null;
  connected = false;

  constructor(
    private linkId: number,
    private handlers: CaptureTransportHandlers
  ) {}

  connect(): void {
    const ws = new WebSocket(captureUrl(this.linkId));
    ws.binaryType = "arraybuffer";
    this.ws = ws;

    ws.onopen = () => {
      this.connected = true;
      this.handlers.onOpen?.();
    };
    ws.onmessage = (ev) => {
      if (ev.data instanceof ArrayBuffer) this.handlers.onData(new Uint8Array(ev.data));
      // Client→server frames are ignored by the bridge; we never send any.
    };
    ws.onerror = (ev) => this.handlers.onError?.(ev);
    ws.onclose = () => {
      this.connected = false;
      this.handlers.onClose?.();
    };
  }

  disconnect(): void {
    this.ws?.close();
    this.ws = null;
    this.connected = false;
  }
}
