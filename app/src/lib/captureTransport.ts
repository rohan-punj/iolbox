// Live-capture WS client, one per open capture tab (feature 1). Connects to
// GET /capture/{linkId}, which upgrades to a WebSocket carrying a raw pcapng
// byte stream (binary frames) for the link's active capture. A 404 (JSON body)
// before the upgrade means the link has no active capture — the socket fails
// to open, surfaced via onError/onClose.
//
// RECONNECT (item-1 fix): connect() used to be one-shot, which raced the
// supervisor's async capture.start relay rebuild — the tab connected first,
// got the 404, and stayed dead forever even once the capture came up. The
// transport now retries with capped exponential backoff (1s, 2s, 4s, 5s cap)
// for as long as the tab is open, and retryNow() collapses the backoff when
// the app KNOWS the capture just (re)armed (a capture.started event or a lab
// start). Each successful (re)connection delivers a fresh pcapng stream from
// its SHB, so the consumer must reset its parser in onOpen.
//
// Same-origin URL, derived from the page's own host, exactly like
// consoleTransport.ts (both are served by the supervisor's one WS bridge).
import { sameOriginControlUrl } from "./transportSelect";

export interface CaptureTransportHandlers {
  onData(bytes: Uint8Array): void;
  /** Fires on every (re)connection — reset per-stream state (pcapng parser). */
  onOpen?(): void;
  onClose?(): void;
  onError?(err: unknown): void;
}

/** Builds ws(s)://<host>/capture/<linkId> from the same origin as /control. */
export function captureUrl(linkId: number): string {
  return sameOriginControlUrl().replace(/\/control$/, `/capture/${linkId}`);
}

const BACKOFF_BASE_MS = 1000;
const BACKOFF_CAP_MS = 5000;

export class CaptureTransport {
  private ws: WebSocket | null = null;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private attempt = 0;
  private stopped = false;
  connected = false;
  private linkId: number;
  private handlers: CaptureTransportHandlers;

  constructor(linkId: number, handlers: CaptureTransportHandlers) {
    this.linkId = linkId;
    this.handlers = handlers;
  }

  /** Open (or re-open) the stream; keeps retrying until disconnect(). */
  connect(): void {
    this.stopped = false;
    this.dial();
  }

  /** Collapse the backoff and reconnect immediately — used when a
   *  capture.started event (or a lab start) says the port is live NOW. */
  retryNow(): void {
    if (this.stopped || this.connected) return;
    this.attempt = 0;
    this.clearRetry();
    this.dial();
  }

  private dial(): void {
    if (this.stopped || this.ws) return;
    const ws = new WebSocket(captureUrl(this.linkId));
    ws.binaryType = "arraybuffer";
    this.ws = ws;

    ws.onopen = () => {
      this.connected = true;
      this.attempt = 0;
      this.handlers.onOpen?.();
    };
    ws.onmessage = (ev) => {
      if (ev.data instanceof ArrayBuffer) this.handlers.onData(new Uint8Array(ev.data));
      // Client→server frames are ignored by the bridge; we never send any.
    };
    ws.onerror = (ev) => this.handlers.onError?.(ev);
    ws.onclose = () => {
      const wasConnected = this.connected;
      this.connected = false;
      this.ws = null;
      this.handlers.onClose?.();
      if (this.stopped) return;
      // A drop after a healthy session restarts the ladder from the bottom.
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

  disconnect(): void {
    this.stopped = true;
    this.clearRetry();
    const ws = this.ws;
    this.ws = null; // onclose sees stopped=true and does not retry
    ws?.close();
    this.connected = false;
  }
}
