//! VMware provider (primary, per PLAN.md). Lifecycle via `vmrun`.
//!
//! TODO(P1):
//! - `detect()`: locate `vmrun.exe` on PATH / default install dirs (Workstation
//!   and Player), report which.
//! - `provision()`: import `iolbox-appliance.vmx` (+ .vmdk) into a known
//!   location if not already present.
//! - `start()`/`stop()`: `vmrun -T ws start <vmx> nogui` / `stop`; Player
//!   uses `-T player`.
//! - `endpoint()`: `vmrun getGuestIPAddress <vmx>` then the supervisor's
//!   fixed control port, OR a fixed host-only IP baked into the appliance
//!   (mirrors the PNetLab gate VM pattern already in production use).
//! - `sync_image()`: `vmrun CopyFileFromHostToGuest`, or a shared folder
//!   mounted at `/opt/iolbox/images` inside the appliance.

use super::{Detection, Endpoint, Health, Provider, ProviderError, ProviderId, ProviderResult};
use std::path::Path;

#[derive(Default)]
pub struct VmwareProvider;

impl Provider for VmwareProvider {
    fn id(&self) -> ProviderId {
        ProviderId::Vmware
    }

    fn detect(&self) -> Detection {
        // TODO(P1): actually probe for vmrun.exe (PATH + Program Files).
        Detection {
            id: ProviderId::Vmware,
            available: false,
            recommended: true,
            detail: "vmrun.exe detection not implemented yet (Rust stub).".into(),
            warning: None,
        }
    }

    fn provision(&self, _appliance: &Path) -> ProviderResult<()> {
        Err(ProviderError::NotImplemented("vmware::provision".into()))
    }

    fn start(&self) -> ProviderResult<Endpoint> {
        Err(ProviderError::NotImplemented("vmware::start".into()))
    }

    fn stop(&self) -> ProviderResult<()> {
        Err(ProviderError::NotImplemented("vmware::stop".into()))
    }

    fn endpoint(&self) -> Option<Endpoint> {
        None
    }

    fn sync_image(&self, _local: &Path) -> ProviderResult<String> {
        Err(ProviderError::NotImplemented("vmware::sync_image".into()))
    }

    fn health(&self) -> Health {
        Health::Unknown
    }
}
