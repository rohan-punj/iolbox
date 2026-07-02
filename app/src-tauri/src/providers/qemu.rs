//! QEMU compatibility provider (docs/providers.md).
//!
//! Bundled `qemu-system-x86_64` + tiny kernel/initrd running the supervisor,
//! TCG (no hypervisor). Slow (IOL idle spin hurts) but conflicts with
//! nothing — the always-available last resort.
//!
//! TODO(P1):
//! - `detect()`: always available (bundled binary shipped with the app).
//! - `start()`: spawn qemu with user-mode net + hostfwd for control (4000),
//!   console (9000+), capture (5500+) ports; endpoint is `127.0.0.1:<port>`.
//! - `sync_image()`: 9p/virtfs share or copy into the guest image.

use super::{Detection, Endpoint, Health, Provider, ProviderError, ProviderId, ProviderResult};
use std::path::Path;

#[derive(Default)]
pub struct QemuProvider;

impl Provider for QemuProvider {
    fn id(&self) -> ProviderId {
        ProviderId::Qemu
    }

    fn detect(&self) -> Detection {
        Detection {
            id: ProviderId::Qemu,
            // Bundled → always available, but never the recommended default.
            available: true,
            recommended: false,
            detail: "Bundled software emulation (TCG). Slow but conflicts with nothing.".into(),
            warning: None,
        }
    }

    fn provision(&self, _appliance: &Path) -> ProviderResult<()> {
        Err(ProviderError::NotImplemented("qemu::provision".into()))
    }

    fn start(&self) -> ProviderResult<Endpoint> {
        Err(ProviderError::NotImplemented("qemu::start".into()))
    }

    fn stop(&self) -> ProviderResult<()> {
        Err(ProviderError::NotImplemented("qemu::stop".into()))
    }

    fn endpoint(&self) -> Option<Endpoint> {
        None
    }

    fn sync_image(&self, _local: &Path) -> ProviderResult<String> {
        Err(ProviderError::NotImplemented("qemu::sync_image".into()))
    }

    fn health(&self) -> Health {
        Health::Unknown
    }
}
