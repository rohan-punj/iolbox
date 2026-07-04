// Entry point for the compiled binary. Actual app setup lives in `lib.rs`
// so it can also be exercised by integration tests / mobile targets later.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

fn main() {
    iolbox_lib::run();
}
