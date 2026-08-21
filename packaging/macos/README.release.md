# iolbox on Apple Silicon macOS

This unsigned archive is the Apple Silicon (darwin/arm64) iolbox launcher,
the two Linux payloads produced by the same release workflow (one linux/amd64,
one linux/arm64), and the locked Lima profiles/provisioner it needs. It
contains no guest disk, hypervisor, Lima installation, or Cisco software. You
supply a legally held **x86_64 IOL** image; i386/i86bi and arm64-native IOL
are unsupported.

## Quick start

Prerequisites:

- Apple Silicon macOS. Rosetta is required for the `debian13`/`jammy`
  Rosetta profiles; the `native-arm64` profile does not use Rosetta and
  translates x86_64 IOL with qemu-user inside the guest instead;
- Lima installed and managed separately by you (Apache-2.0; it is not
  redistributed or installed, upgraded, or removed by iolbox).

Download iolbox-macos-arm64.tar.gz and its matching
iolbox-macos-arm64.tar.gz.sha256 from the same release. Verify the outer
checksum before extracting:

~~~sh
shasum -a 256 -c iolbox-macos-arm64.tar.gz.sha256
mkdir extract
tar -xzf iolbox-macos-arm64.tar.gz -C extract
cd extract/iolbox-macos-arm64
shasum -a 256 -c SHA256SUMS
~~~

The artifact is unsigned. Before the first run, inspect quarantine on the
downloaded archive, extracted directory, and launcher:

~~~sh
xattr -l /path/to/iolbox-macos-arm64.tar.gz || true
xattr -p com.apple.quarantine /path/to/iolbox-macos-arm64.tar.gz; echo "archive xattr exit=$?"
xattr -l .; echo "directory xattr exit=$?"
xattr -p com.apple.quarantine ./iolbox; echo "binary xattr exit=$?"
~~~

If the launcher has com.apple.quarantine, remove that attribute from the
launcher only, after the checksum checks above:

~~~sh
xattr -d com.apple.quarantine ./iolbox
~~~

Then start the product from the extracted directory with one command:

~~~sh
./iolbox start
~~~

### Optional: double-click launcher (IOLbox.app)

The archive also ships `IOLbox.app`, an unsigned convenience wrapper around
the same `iolbox` CLI. Once you've completed the quarantine check above, it
lets you start iolbox from Finder or the Dock instead of a terminal command:
it opens a real Terminal window running `./iolbox start`, so every prompt
and log line you'd see from the CLI — including the vCPU/RAM prompt on first
run — is still there. Once `start` finishes, that same Terminal window drops
into a small `iolbox>` prompt: type `stop` to shut the VM down, `status` or
`diagnose` to check on it, or just close the window to leave it running in
the background — closing the window does **not** stop the VM. `IOLbox.app`
is still not a control panel: `upgrade` and anything beyond stop/status/
diagnose remain CLI-only, run from a normal terminal in the extracted
folder.

`IOLbox.app` cannot make the first run warning-free; nothing in this archive
is signed or notarized. If you downloaded with `curl`, nothing is
quarantined and the app just works. **If you downloaded through a browser,
running the command below first is not optional — it is the only way to
launch `IOLbox.app` at all.** Run this once, before the first double-click,
on the whole extracted folder:

~~~sh
xattr -dr com.apple.quarantine /path/to/extracted/iolbox-macos-arm64
~~~

**Skipping this step does not show a bypassable warning — it gets the app
deleted.** A quarantined `IOLbox.app` shows the dialog "IOLbox is damaged
and can't be opened. You should move it to the Trash," with no recovery
option offered; Trash is the only button. Nothing is actually corrupted —
this is Apple's standard message for an unsigned, non-notarized app. If you
see it, re-extract the archive, run the command above, and try again.

Even with the command above run first, macOS may still translocate a
quarantined app to a temporary, read-only copy that can't find the `iolbox`
CLI or `lima/` folder next to it. If `IOLbox.app` shows an alert about not
finding its files, it's telling you to run the command above and try again.

The launcher resolves a profile, then creates or reuses the durable named
Lima guest for it, verifies that profile's canary and pinned guest/kernel, and
serves the GUI on http://127.0.0.1:4001. The GUI, console, and capture
forwards are loopback-only and have no authentication.

Profiles:

- native-arm64 (Lima guest iolbox-native-arm64) runs the supervisor, VPCS, and
  tool packs as real arm64 binaries on Debian 13/trixie with the pinned
  6.12.101+deb13-cloud-arm64 kernel. x86_64 IOL is translated by qemu-user
  inside the guest, which the guest installs from Debian at provisioning time.
  It has no host qualification row, so the launcher reports it as
  UNMEASURED - CANARY REQUIRED and gates it on its own fail-closed canary.
- debian13 (Lima guest iolbox-debian13) is the Rosetta default: the amd64
  payload and x86_64 IOL under Rosetta, same pinned Debian 13/trixie kernel.
- jammy is the Rosetta compatibility profile, pinned to Ubuntu 22.04 with the
  qualified 5.15 guest/kernel line.
- The Debian 12/bookworm files are shipped as an unqualified candidate and are
  refused while their digest remains unpinned.

On a qualifying host with no existing iolbox install, the default automatic
selection is native-arm64. If you already have an install, it keeps its
current profile and Lima machine - the launcher will not migrate it for you,
and says so on stderr when it declines. To migrate deliberately:

~~~sh
./iolbox start --profile native-arm64
~~~

That choice is remembered. To pin the Rosetta path instead, use
--profile rosetta-amd64 or set IOLBOX_PROFILE=rosetta-amd64. Automatic
selection falls back to Rosetta whenever the native preflight fails; an
explicit --profile native-arm64 fails closed rather than falling back.

Host data defaults to:

~~~text
~/Library/Application Support/iolbox/images
~/Library/Application Support/iolbox/labs
~~~

./iolbox stop stops the guest and preserves its VM, images, labs, license,
and Lima cache. Removing the extracted launcher directory is ordinary,
non-destructive uninstall. An optional destructive reset may delete only the
named iolbox-debian13 Lima machine and move the two iolbox data paths to
Trash after explicit checks; it is not part of ordinary launcher removal.

## Upgrade and support boundary

From a later extracted release, run ./iolbox upgrade in the existing data
environment. It preserves host images/labs, the host identity and license,
the Lima machine, and the pinned guest kernel while replacing the guest
payload.

On the Rosetta profiles the runtime executes the amd64 supervisor/VPCS and
x86_64 IOL under Rosetta inside the arm64 Lima guest. On native-arm64 the
supervisor, VPCS, and tool packs are real arm64 binaries and only x86_64 IOL
is translated, by in-guest qemu-user. Either way this release supports
**x86_64 IOL only**; i386/i86bi and arm64-native IOL are not supported and are
not made runnable by the archive.

`./iolbox upgrade` keeps your existing Lima machine and its profile.

The installation guide for this exact release is:
<https://github.com/rohan-punj/iolbox/blob/@VERSION@/docs/INSTALL.md>

The archive's SHA-256 files detect corruption or a changed download. Because
this release is unsigned, they do not authenticate the publisher. Lima fetches
the digest-locked Debian/Ubuntu guest image and apt packages at provisioning
time; those guest assets are not embedded here. See notices/THIRD_PARTY.md
for the distributed VPCS notice, and for what the native-arm64 profile
installs into the guest from Debian.
