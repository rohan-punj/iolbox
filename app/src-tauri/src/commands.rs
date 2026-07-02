//! Tauri command handlers invoked from the Svelte frontend.
//!
//! These are the Windows-side entry points. Several are still stubs returning
//! mock JSON — the frontend runs against `MockTransport` today (see
//! app/src/lib/mockTransport.ts), so the real supervisor socket + provider
//! orchestration can land incrementally (P1–P3) without blocking the UI.

use crate::image::{ImageLibrary, ImageMeta};
use crate::providers::{self, Detection};
use serde::{Deserialize, Serialize};
use std::path::PathBuf;
use tauri::State;

/// First-run provider detection (docs/providers.md). Ranked list surfaced by
/// the Preflight UI. Currently returns the providers' own `detect()` output
/// (stubs); no system probing is wired yet.
#[tauri::command]
pub fn detect_providers() -> Vec<Detection> {
    providers::detect_all()
}

#[derive(Debug, Serialize)]
pub struct RuntimeEndpoint {
    pub host: String,
    pub control_port: u16,
}

/// Boot the selected runtime provider and return the control endpoint.
///
/// TODO(P1): dispatch to the chosen provider's `start()`, then open the TCP
/// control socket and begin forwarding supervisor NDJSON frames to the
/// frontend as Tauri events (`supervisor://frame`). See
/// app/src/lib/tcpTransport.ts for the frontend side of this seam.
#[tauri::command]
pub fn start_runtime(provider: String) -> Result<RuntimeEndpoint, String> {
    let _ = provider;
    // Stub: pretend the supervisor is reachable on the default control port.
    Ok(RuntimeEndpoint {
        host: "127.0.0.1".into(),
        control_port: 4000,
    })
}

/// Stop the active runtime provider.
///
/// TODO(P1): dispatch to the active provider's `stop()`.
#[tauri::command]
pub fn stop_runtime() -> Result<(), String> {
    Ok(())
}

/// Open a native file picker for a `.bin`/`.iol` image. Returns the chosen
/// path, or `None` if the user cancelled.
///
/// TODO(P4): use tauri-plugin-dialog's async file dialog. Returning `None`
/// here keeps the command wired without pulling the async dialog into this
/// stub signature.
#[tauri::command]
pub fn pick_image_file() -> Option<String> {
    None
}

/// Register an image into the local library: fingerprint (sha256) + sniff
/// class/arch. Nodes then reference it by `id` for hot-swap.
#[tauri::command]
pub fn register_image(path: String, library: State<'_, ImageLibrary>) -> Result<ImageMeta, String> {
    library
        .register(&PathBuf::from(path))
        .map_err(|e| e.to_string())
}

/// List the local image library.
#[tauri::command]
pub fn list_images(library: State<'_, ImageLibrary>) -> Vec<ImageMeta> {
    library.list()
}

/// Launch an external terminal against a node's telnet console port.
///
/// TODO(P2): shell out to Windows Terminal (`wt.exe`) or `telnet.exe`
/// pointed at `127.0.0.1:<console_port>` via tauri-plugin-shell.
#[tauri::command]
pub fn open_external_console(node: u32, console_port: u16) -> Result<(), String> {
    let _ = (node, console_port);
    Ok(())
}

#[derive(Debug, Deserialize)]
pub struct CaptureRequest {
    pub link: u32,
    pub capture_port: u16,
}

/// Start a live Wireshark capture on a link.
///
/// TODO(P3): launch `wireshark -k -i -` and pipe the pcapng TCP byte stream
/// from `127.0.0.1:<capture_port>` (allocated by the supervisor on
/// capture.start, docs/protocol.md) into Wireshark's stdin. Until then this
/// is a no-op stub so the frontend button is wired end-to-end.
#[tauri::command]
pub fn start_capture(req: CaptureRequest) -> Result<(), String> {
    let _ = req;
    Ok(())
}
