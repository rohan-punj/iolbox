//! Local image library (Windows side), per PLAN.md "Image loading & swapping".
//!
//! The library is a Windows folder (default `%APPDATA%\iolbox\images`) the GUI
//! manages. "Add image" copies a `.bin`/`.iol` in; the app fingerprints it
//! (sha256), sniffs L2-vs-L3 and arch (i386/x86_64), and records metadata.
//! Nodes reference images by `id` (sha256 prefix), so swapping a node's image
//! is just changing the id — see the lab schema's `imageRef`.
//!
//! Authoritative class/arch detection is re-done by the supervisor inside the
//! runtime (docs/protocol.md `image.register`); the cached hints here are for
//! the GUI's convenience only.

use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::path::{Path, PathBuf};
use std::sync::Mutex;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ImageClass {
    L2,
    L3,
    Unknown,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ImageArch {
    I386,
    X86_64,
    Unknown,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ImageMeta {
    /// Library id = sha256 prefix. Nodes bind to THIS (enables hot-swap).
    pub id: String,
    pub filename: String,
    pub class: ImageClass,
    pub arch: ImageArch,
    pub sha256: String,
    pub size: u64,
}

/// Tauri-managed state: the in-memory catalog of registered images.
pub struct ImageLibrary {
    inner: Mutex<Vec<ImageMeta>>,
}

impl ImageLibrary {
    pub fn new() -> Self {
        Self {
            inner: Mutex::new(Vec::new()),
        }
    }

    /// Default library location: `%APPDATA%\iolbox\images`.
    pub fn default_dir() -> PathBuf {
        let base = std::env::var_os("APPDATA")
            .map(PathBuf::from)
            .unwrap_or_else(|| PathBuf::from("."));
        base.join("iolbox").join("images")
    }

    pub fn list(&self) -> Vec<ImageMeta> {
        self.inner.lock().expect("image lib poisoned").clone()
    }

    /// Fingerprint + sniff a file and add it to the catalog. Idempotent by id.
    pub fn register(&self, path: &Path) -> std::io::Result<ImageMeta> {
        let meta = fingerprint(path)?;
        let mut guard = self.inner.lock().expect("image lib poisoned");
        if !guard.iter().any(|m| m.id == meta.id) {
            guard.push(meta.clone());
        }
        Ok(meta)
    }

    pub fn remove(&self, id: &str) {
        let mut guard = self.inner.lock().expect("image lib poisoned");
        guard.retain(|m| m.id != id);
    }
}

impl Default for ImageLibrary {
    fn default() -> Self {
        Self::new()
    }
}

/// Compute sha256 + best-effort class/arch heuristics from the file.
///
/// NOTE: filename heuristics are a GUI convenience only; the supervisor
/// re-detects authoritatively by inspecting the ELF inside the runtime.
pub fn fingerprint(path: &Path) -> std::io::Result<ImageMeta> {
    let bytes = std::fs::read(path)?;
    let mut hasher = Sha256::new();
    hasher.update(&bytes);
    let digest = hasher.finalize();
    let sha256 = hex_encode(&digest);
    let id = sha256[..8].to_string();

    let filename = path
        .file_name()
        .and_then(|s| s.to_str())
        .unwrap_or("image.bin")
        .to_string();

    let class = sniff_class(&filename);
    let arch = sniff_arch(&filename, &bytes);

    Ok(ImageMeta {
        id,
        filename,
        class,
        arch,
        sha256,
        size: bytes.len() as u64,
    })
}

fn sniff_class(filename: &str) -> ImageClass {
    let lower = filename.to_ascii_lowercase();
    if lower.contains("l2") || lower.contains("_l2") {
        ImageClass::L2
    } else if lower.contains("linux") || lower.contains("adventerprise") {
        ImageClass::L3
    } else {
        ImageClass::Unknown
    }
}

fn sniff_arch(filename: &str, bytes: &[u8]) -> ImageArch {
    // Prefer the ELF header's machine field if the file looks like an ELF.
    // e_machine is at offset 18 (little-endian u16): 0x03 = i386, 0x3E = x86_64.
    if bytes.len() > 20 && &bytes[0..4] == b"\x7fELF" {
        let machine = u16::from_le_bytes([bytes[18], bytes[19]]);
        match machine {
            0x03 => return ImageArch::I386,
            0x3E => return ImageArch::X86_64,
            _ => {}
        }
    }
    let lower = filename.to_ascii_lowercase();
    if lower.contains("x86_64") || lower.contains("amd64") {
        ImageArch::X86_64
    } else if lower.contains("i386") || lower.contains("linux") {
        ImageArch::I386
    } else {
        ImageArch::Unknown
    }
}

fn hex_encode(bytes: &[u8]) -> String {
    let mut s = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        s.push_str(&format!("{b:02x}"));
    }
    s
}
