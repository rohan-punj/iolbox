//! WSL2 provider (docs/providers.md).
//!
//! Chosen only when the Windows Hypervisor Platform / Hyper-V is ALREADY
//! enabled. **Critical rule:** never enable it automatically — doing so
//! degrades VMware Workstation and kills nested virtualization. If WSL2
//! would require enabling it, `detect()` must report `available = false`
//! with a warning and let `vmware` win.
//!
//! TODO(P1):
//! - `detect()`: `wsl.exe` present AND VMP/Hyper-V feature state already on
//!   (query via DISM / `Get-WindowsOptionalFeature` or the registry).
//! - `provision()`: `wsl --import iolab <dir> iolab-rootfs.tar` (no Store
//!   distro needed).
//! - `start()`: launch the supervisor inside the distro; endpoint is
//!   `127.0.0.1:4000` directly via WSL2 localhost forwarding.
//! - `sync_image()`: images live on a Windows path visible at `/mnt/...`,
//!   or are copied in.

use super::{Detection, Endpoint, Health, Provider, ProviderError, ProviderId, ProviderResult};
use std::path::Path;

#[derive(Default)]
pub struct Wsl2Provider;

impl Provider for Wsl2Provider {
    fn id(&self) -> ProviderId {
        ProviderId::Wsl2
    }

    fn detect(&self) -> Detection {
        // TODO(P1): probe wsl.exe + Hyper-V/VMP feature state.
        Detection {
            id: ProviderId::Wsl2,
            available: false,
            recommended: false,
            detail: "WSL2 detection not implemented yet (Rust stub).".into(),
            warning: Some(
                "Enabling Hyper-V/WHP for WSL2 degrades VMware Workstation and disables nested \
                 virtualization. iolab never enables it automatically."
                    .into(),
            ),
        }
    }

    fn provision(&self, _appliance: &Path) -> ProviderResult<()> {
        Err(ProviderError::NotImplemented("wsl2::provision".into()))
    }

    fn start(&self) -> ProviderResult<Endpoint> {
        Err(ProviderError::NotImplemented("wsl2::start".into()))
    }

    fn stop(&self) -> ProviderResult<()> {
        Err(ProviderError::NotImplemented("wsl2::stop".into()))
    }

    fn endpoint(&self) -> Option<Endpoint> {
        None
    }

    fn sync_image(&self, _local: &Path) -> ProviderResult<String> {
        Err(ProviderError::NotImplemented("wsl2::sync_image".into()))
    }

    fn health(&self) -> Health {
        Health::Unknown
    }
}
