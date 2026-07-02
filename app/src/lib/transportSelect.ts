// Picks which Transport implementation the app talks through, at startup.
//
// Rules (P1, browser-first — see docs/protocol.md + wsTransport.ts):
//   1. Explicit override always wins: `?transport=mock` or
//      `?transport=ws[&url=ws://host:port/control]`.
//   2. Running under Tauri's webview (desktop shell) → MockTransport for now.
//      The desktop build still drives the Rust/provider plumbing (P2); it does
//      not yet spawn a supervisor + point this client at it.
//   3. `npm run dev` (Vite dev server, no supervisor listening on the page's
//      own origin) → MockTransport, so the existing click-through dev flow
//      keeps working with no backend required.
//   4. Otherwise — the page is being served over http(s) by something that
//      isn't the Vite dev server, i.e. the supervisor itself via go:embed
//      (`vite build --outDir ../supervisor/internal/web/dist`) — default to a
//      same-origin WsTransport: `ws(s)://<location.host>/control`, the exact
//      host:port the page was loaded from (the supervisor's HTTP+WS bridge
//      serves both the static assets and /control on one listener).
import { MockTransport } from "./mockTransport";
import { WsTransport } from "./wsTransport";
import type { Transport } from "./transport";

/** True when running inside the Tauri desktop webview (v1 or v2 globals). */
function isTauri(): boolean {
  const w = window as unknown as Record<string, unknown>;
  return Boolean(w.__TAURI_INTERNALS__ || w.__TAURI__);
}

/** Same-origin control WS URL derived from the page's own host:port. */
export function sameOriginControlUrl(): string {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${location.host}/control`;
}

export interface TransportSelection {
  transport: Transport;
  /** For diagnostics/logging only. */
  kind: "mock" | "ws";
  url?: string;
}

/**
 * Choose a transport for this session. Reads `?transport=` (and `?url=` for
 * the ws case) from the current page URL; falls back to the dev-flow rules
 * above when absent.
 */
export function selectTransport(): TransportSelection {
  const params = new URLSearchParams(location.search);
  const override = params.get("transport");

  if (override === "mock") {
    return { transport: new MockTransport(), kind: "mock" };
  }
  if (override === "ws") {
    const url = params.get("url") ?? sameOriginControlUrl();
    return { transport: new WsTransport({ url }), kind: "ws", url };
  }

  // No explicit override: Tauri desktop shell and the Vite dev server both
  // stay on MockTransport (no real supervisor reachable from either yet).
  if (isTauri() || import.meta.env.DEV) {
    return { transport: new MockTransport(), kind: "mock" };
  }

  // Served by something other than Vite dev — assume it's the supervisor's
  // embedded static server, which also fronts /control on the same origin.
  const url = sameOriginControlUrl();
  return { transport: new WsTransport({ url }), kind: "ws", url };
}
