// Real transport stub. Will be backed by a Tauri command that owns a TCP
// socket to the supervisor (127.0.0.1:4000 by default, see docs/protocol.md)
// and streams NDJSON lines back as Tauri events. Not wired yet — the Rust
// side has no socket implementation, only command stubs (see src-tauri).
import type { Request } from "./protocol";
import type { IncomingFrame, Transport } from "./transport";

export interface TcpTransportOptions {
  host?: string;
  port?: number;
}

export class TcpTransport implements Transport {
  connected = false;
  private handlers = new Set<(frame: IncomingFrame) => void>();
  private host: string;
  private port: number;

  constructor(opts: TcpTransportOptions = {}) {
    this.host = opts.host ?? "127.0.0.1";
    this.port = opts.port ?? 4000;
  }

  async connect(): Promise<void> {
    // TODO(P1): invoke a Tauri command (e.g. `supervisor_connect`) that opens
    // the TCP socket on the Rust side and forwards NDJSON lines as a Tauri
    // event (`supervisor://frame`), since the browser webview has no raw TCP
    // access. Listen for that event here and dispatch into this.handlers.
    throw new Error(
      `TcpTransport not implemented yet — requires a Tauri command wired to ` +
        `${this.host}:${this.port}. Use MockTransport for now.`
    );
  }

  disconnect(): void {
    this.connected = false;
    this.handlers.clear();
  }

  onMessage(handler: (frame: IncomingFrame) => void): () => void {
    this.handlers.add(handler);
    return () => this.handlers.delete(handler);
  }

  send(_req: Request): void {
    // TODO(P1): invoke `supervisor_send` with the serialized request line.
    throw new Error("TcpTransport not implemented yet — use MockTransport for now.");
  }
}
