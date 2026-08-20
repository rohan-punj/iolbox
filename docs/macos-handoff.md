# Apple Silicon macOS track — handoff

Written 2026-08-14, end of the planning + M0 session. Read this first, then the
three documents in "Authoritative documents" below.

## One-paragraph state of the world

iolbox is **confirmed working on Apple Silicon** using the existing, unmodified
linux/amd64 payload, translated by Apple Rosetta inside an arm64 Lima VM. M0
passed on real hardware: two Cisco IOL 17.18.02 routers booted and pinged
(90%, 9/10, 1/5/37 ms). No arm64 port is required for the MVP. The remaining
work is provisioning, packaging, and UX parity — estimated 8-12 focused
engineering days. The next slice is **M1**.

## Authoritative documents

| File | What it is | Trust |
|---|---|---|
| `docs/macos-m0-result.md` | Executed hardware test record | **Ground truth.** Measured, not predicted. |
| `docs/macos-arm64-plan.md` | The plan, rewritten around M0 | Current ruling and phases M1-M7. |
| `docs/macos-arm64-plan-review.md` | Adversarial review of an earlier plan draft | Findings F1-F6 already folded in. Historical, but F1 is worth reading. |

Where these disagree, precedence is: M0 result > plan > review.

## The ruling in one sentence

Ship one unsigned `darwin/arm64` launcher archive that provisions a **pinned
Ubuntu 22.04 / kernel 5.15 Lima VZ machine**, gates Rosetta with the amd64
loader canary, installs the existing amd64 native tarball, and opens the
existing browser GUI. Lima is the sole required VM manager for v1; the user
installs it themselves, and it — not us — owns the Apple virtualization
entitlement.

## Non-negotiable constraints

1. **No Apple Developer account, ever.** No Developer ID, notarization,
   provisioning profile, or restricted entitlement. The artifact is unsigned;
   quarantine recovery (`right-click/Open`, `xattr -d com.apple.quarantine`) is
   part of the documented UX, not a bug.
2. **Guest kernel must stay < 6.3.** macOS 13.5's Rosetta aborts *every* amd64
   executable on kernel 6.8 with `rosetta error: unhandled auxillary vector
   type 28` (`AT_RSEQ_ALIGN`, emitted by Linux 6.3+). Ubuntu 22.04 / 5.15
   works; Ubuntu 24.04 / 6.8 does not.
3. **Gate on the canary, never on a version number.** The macOS release that
   fixes this is **UNVERIFIED**. The supported check is executing
   `/lib64/ld-linux-x86-64.so.2 --version` through Rosetta and failing closed.
4. **Do not redistribute a hypervisor.** No bundled QEMU, no OrbStack, no Lima
   binaries in our archive.
5. **The arm64 port is optional (M7), not a prerequisite.** Its justification is
   independence from constraint 2, not performance.

## Environment: the Mac

| Item | Value |
|---|---|
| Host | `192.168.101.144`, `Rohans-MacBook-Air.local` |
| User | `rohansharma` (NOT the display name "Admin") |
| Auth | key `~/.ssh/iolbox_mac_m0` (ed25519, no passphrase), installed in `authorized_keys` |
| Hardware | Apple M1, 8 GB RAM |
| OS | macOS **13.5** (22G74) — at the Rosetta floor |
| Rosetta | preinstalled (`oahd` running) |
| Homebrew | `/opt/homebrew/bin/brew` — **not on the non-login PATH**, always use the absolute path over SSH |
| Lima | 2.2.0, `/opt/homebrew/bin/limactl` |
| Disk | **chronically tight.** Was 3.5 GB free; ~5.4 GB after cleanup. Check before creating VMs. |

Revoke access by deleting the `iolbox-m0-mac` line from
`~/.ssh/authorized_keys`. The account password used to bootstrap the key was
shared in plaintext and should be rotated.

### Reproducing the proven environment

```bash
/opt/homebrew/bin/limactl start --name=iol22 --vm-type=vz --rosetta \
  --cpus=4 --memory=4 --disk=15 --tty=false template://ubuntu-22.04
```

Then, inside the guest, before anything else:

```bash
# amd64 packages are NOT on ports.ubuntu.com (404 on every index).
sudo sed -i "s|^deb http|deb [arch=arm64] http|g" /etc/apt/sources.list
sudo tee /etc/apt/sources.list.d/amd64.list >/dev/null <<'EOF'
deb [arch=amd64] http://archive.ubuntu.com/ubuntu/ jammy main restricted universe multiverse
deb [arch=amd64] http://archive.ubuntu.com/ubuntu/ jammy-updates main restricted universe multiverse
deb [arch=amd64] http://security.ubuntu.com/ubuntu/ jammy-security main restricted universe multiverse
EOF
sudo dpkg --add-architecture amd64
sudo apt-get update
sudo apt-get install -y libc6:amd64 libssl3:amd64

# THE CANARY — must print a glibc version, not a rosetta error
/lib64/ld-linux-x86-64.so.2 --version
```

Then `sudo ./install.sh --bind all` from the unpacked
`iolbox-server-*.tar.gz`, drop the IOL `.bin` into `/opt/iolbox/images/`,
`chmod 0755`, and restart `iolbox-supervisor.service` to trigger the rescan.

## Repository state

- Branch **`luna/macos-arm64-invariant`** — created to hold work off `main`.
- **Everything is UNCOMMITTED.** 11 modified files plus new untracked files.
  Commit before doing anything destructive.
  - `build-release.sh` — emits `supervisor-linux-arm64`
  - `runtime/fetch-vpcs.sh` — `--arch amd64|arm64`, amd64 default unchanged
  - `runtime/build-rootfs.sh` — arch-parameterised, omits i386 multiarch on arm64
  - `runtime/README.md` — documents the arm64 target
  - comment-only fixes in `supervisor/internal/{dirstat,iouyap,slowtee,tool,vtap}`,
    `tools/p0-reaper`, `tools/secbench-attacks-go`
  - `tools/translation-rehearsal/` + `docs/translation-rehearsal.md` — FEX/qemu-user
    harness, **never executed**
- All of this is **M7 material**. None of it is needed for M1-M6.
- `main` is clean.

## What is proven vs. assumed

**Proven on hardware** (see M0 record for evidence): Rosetta binfmt; `/dev/net/tun`;
systemd + cgroup delegation; `install.sh` on an arm64 host; supervisor as a
service through Rosetta; GUI HTTP 200; image scan + L3 classification; iourc
generation from hostid/hostname; two-node start in 10 s; NVRAM startup-config
injection; p2p data plane; VM stop/start persistence with image cache and
licence intact; ~530 MB RSS per IOL, load 0.07.

**NOT proven — these are still real gates:** four-node labs; capture / Wireshark
tee; NAT/extnet; VPCS inside a lab; multi-link fabrics; soak beyond ~12 min; the
**browser GUI** (only HTTP status and control API were exercised — sol made
"HTTP 200 alone does not count" a release gate); Lima port-forwarding of
console/capture ranges to the macOS side (the lab was driven from inside the
guest); anything on macOS ≥ 14; OrbStack entirely (it requires macOS 14.0 and
could not run on this host).

## Known gotchas

1. `codex exec` **hangs forever** without `< /dev/null` when the prompt is an
   argument. A lone `Reading additional input from stdin...` line is a hang.
2. The codex sandbox has a **read-only `.git`** — agents cannot create branches
   or commit. Do that from the main session.
3. Lima `limactl copy` is the way into the guest; the Mac home is **not**
   mounted in the VM by default.
4. `lab.listDocs` returns raw **YAML** strings, not JSON objects.
5. Control protocol: NDJSON over `127.0.0.1:4000`, request `id` must be a
   **string**.
6. Console replay ring means a fresh connect replays prior output — always
   regex the **last** `Success rate` match, never the first.
7. `hello` advertises an `i386` capability the platform cannot honour (Rosetta
   is 64-bit only). M5 suppresses it.
8. There is **no `/api/health`**; readiness is `GET /` with status < 500.

## Next slice: M1

Per `docs/macos-arm64-plan.md` §M1 — make the proven guest reproducible and
fail-safe: a deterministic Jammy provisioner, the kernel hold, the canary as a
hard gate, and a **negative test** proving a 6.8-kernel guest is correctly
rejected rather than silently broken. Estimated 1-1.5 days.
