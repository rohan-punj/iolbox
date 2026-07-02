//! Remote (SSH) provider (docs/providers.md).
//!
//! User supplies `ssh user@host`. The app scps the supervisor binary, starts
//! it, and tunnels control/console/capture ports over SSH. Also the
//! CI/headless mode.
//!
//! TODO(P1):
//! - config: capture the user's ssh target (host, user, key/agent).
//! - `detect()`: reachable only once configured; report accordingly.
//! - `provision()`: scp `/opt/iolab/supervisor` (linux/amd64) to the host.
//! - `start()`: launch supervisor over ssh, open local port-forwards for
//!   control (4000), console (9000+), capture (5500+); endpoint is the
//!   local forwarded `127.0.0.1:<port>`.
//! - `sync_image()`: scp the image into `/opt/iolab/images` on the host.

use super::{Detection, Endpoint, Health, Provider, ProviderError, ProviderId, ProviderResult};
use std::path::Path;

#[derive(Default)]
pub struct RemoteProvider {
    /// e.g. "user@host". None until the user configures a target.
    pub target: Option<String>,
}

impl Provider for RemoteProvider {
    fn id(&self) -> ProviderId {
        ProviderId::Remote
    }

    fn detect(&self) -> Detection {
        Detection {
            id: ProviderId::Remote,
            available: self.target.is_some(),
            recommended: false,
            detail: match &self.target {
                Some(t) => format!("Configured target: {t} (reachability check not implemented)."),
                None => "No remote host configured yet.".into(),
            },
            warning: None,
        }
    }

    fn provision(&self, _appliance: &Path) -> ProviderResult<()> {
        Err(ProviderError::NotImplemented("remote::provision".into()))
    }

    fn start(&self) -> ProviderResult<Endpoint> {
        Err(ProviderError::NotImplemented("remote::start".into()))
    }

    fn stop(&self) -> ProviderResult<()> {
        Err(ProviderError::NotImplemented("remote::stop".into()))
    }

    fn endpoint(&self) -> Option<Endpoint> {
        None
    }

    fn sync_image(&self, _local: &Path) -> ProviderResult<String> {
        Err(ProviderError::NotImplemented("remote::sync_image".into()))
    }

    fn health(&self) -> Health {
        Health::Unknown
    }
}
