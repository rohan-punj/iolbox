# iolbox on Apple Silicon macOS

This unsigned archive is the Apple Silicon (darwin/arm64) iolbox launcher,
the exact Linux payload produced by the same release workflow, and the locked
Lima profiles/provisioner it needs. It contains no guest disk, hypervisor,
Lima installation, or Cisco software. You supply a legally held **x86_64 IOL**
image; i386/i86bi and arm64-native IOL are unsupported.

## Quick start

Prerequisites:

- Apple Silicon macOS with Rosetta available;
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

The launcher creates or reuses the durable named Lima guest
iolbox-debian13, verifies the VZ/Rosetta canary and pinned Debian 13/trixie
guest/kernel (6.12.101+deb13-cloud-arm64), and serves the GUI on
http://127.0.0.1:4001. The GUI, console, and capture forwards are
loopback-only and have no authentication.

For the compatibility profile, select jammy; it is pinned to Ubuntu 22.04
with the qualified 5.15 guest/kernel line. The Debian 12/bookworm files are
shipped as an unqualified candidate and are refused while their digest remains
unpinned.

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

The runtime executes the existing Linux supervisor/VPCS and x86_64 IOL under
Rosetta inside the arm64 Lima guest. This release supports **x86_64 IOL only**;
i386/i86bi and arm64-native IOL are not supported and are not made runnable by
the archive.

The installation guide for this exact release is:
<https://github.com/rohan-punj/iolbox/blob/@VERSION@/docs/INSTALL.md>

The archive's SHA-256 files detect corruption or a changed download. Because
this release is unsigned, they do not authenticate the publisher. Lima fetches
the digest-locked Debian/Ubuntu guest image and apt packages at provisioning
time; those guest assets are not embedded here. See notices/THIRD_PARTY.md
for the distributed VPCS notice and the boundary around Windows-only QEMU.
