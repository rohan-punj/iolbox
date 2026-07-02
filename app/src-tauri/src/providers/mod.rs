//! Runtime provider orchestration (docs/providers.md).
//!
//! A provider hosts the Linux environment that runs the supervisor + IOL +
//! VPCS. The trait below mirrors the "conceptual Rust trait" in the
//! contract doc. Bodies are stubs today — first real implementation should
//! be `vmware` (the primary provider per PLAN.md), reusing the
//! `vmrun`-managed-VM pattern already proven for the PNetLab gate VMs.

pub mod qemu;
pub mod remote;
pub mod vmware;
pub mod wsl2;

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ProviderId {
    Vmware,
    Wsl2,
    Remote,
    Qemu,
}

impl ProviderId {
    pub fn as_str(&self) -> &'static str {
        match self {
            ProviderId::Vmware => "vmware",
            ProviderId::Wsl2 => "wsl2",
            ProviderId::Remote => "remote",
            ProviderId::Qemu => "qemu",
        }
    }
}

/// Result of `Provider::detect()` — availability + a plain-English reason,
/// surfaced directly in the Preflight UI.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Detection {
    pub id: ProviderId,
    pub available: bool,
    pub recommended: bool,
    pub detail: String,
    pub warning: Option<String>,
}

/// Control-plane endpoint, always reachable from Windows as `host:port`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Endpoint {
    pub host: String,
    pub control_port: u16,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Health {
    Unknown,
    Starting,
    Healthy,
    Unreachable,
}

#[derive(Debug, thiserror::Error)]
pub enum ProviderError {
    #[error("provider not available: {0}")]
    Unavailable(String),
    #[error("not implemented yet: {0}")]
    NotImplemented(String),
    #[error("io error: {0}")]
    Io(#[from] std::io::Error),
}

pub type ProviderResult<T> = Result<T, ProviderError>;

/// Conceptual provider trait from docs/providers.md. Not yet object-safe /
/// wired into a registry — that lands with the first real provider
/// (`vmware`) in P1. Kept here as the shape every provider module should
/// converge on.
pub trait Provider {
    fn id(&self) -> ProviderId;
    fn detect(&self) -> Detection;
    fn provision(&self, appliance: &std::path::Path) -> ProviderResult<()>;
    fn start(&self) -> ProviderResult<Endpoint>;
    fn stop(&self) -> ProviderResult<()>;
    fn endpoint(&self) -> Option<Endpoint>;
    fn sync_image(&self, local: &std::path::Path) -> ProviderResult<String>;
    fn health(&self) -> Health;
}

/// Detect + rank all providers. Currently returns mock detections; the
/// frontend already has its own mock copy in `Preflight.svelte` for
/// pure-Vite dev, but this is the real seam once `detect_providers` calls
/// through to Rust.
pub fn detect_all() -> Vec<Detection> {
    vec![
        vmware::VmwareProvider::default().detect(),
        wsl2::Wsl2Provider::default().detect(),
        remote::RemoteProvider::default().detect(),
        qemu::QemuProvider::default().detect(),
    ]
}
