// iolab Tauri app — Rust core.
//
// Owns process/provider orchestration on the Windows side (see
// docs/providers.md for the provider contract) and the local image library
// (see PLAN.md "Image loading & swapping"). The frontend talks to the
// supervisor over TCP (docs/protocol.md); that socket is NOT implemented
// here yet (see `commands::supervisor` TODOs) — the GUI runs against
// `MockTransport` in the meantime.

mod commands;
mod image;
mod providers;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_dialog::init())
        .manage(image::ImageLibrary::new())
        .invoke_handler(tauri::generate_handler![
            commands::detect_providers,
            commands::start_runtime,
            commands::stop_runtime,
            commands::pick_image_file,
            commands::register_image,
            commands::list_images,
            commands::open_external_console,
            commands::start_capture,
        ])
        .run(tauri::generate_context!())
        .expect("error while running iolab application");
}
