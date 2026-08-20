# M1 handoff — Apple Silicon macOS track

Updated 2026-08-14, end of the M1 implementation session. Read this **before**
`docs/macos-handoff.md`, which was written against macOS 13.5 and is now partly
obsolete (see §4).

Branch: **`luna/macos-m1-provisioner`**, off `main`, in worktree
`J:\Claude code\iolab-m1-wt`. The M7 arm64 work on
`luna/macos-arm64-invariant` is untouched and still uncommitted.

---

## 1. Status: M1 acceptance criteria are MET on hardware

Every criterion below was **executed on the real Mac**, not asserted. Evidence
lives in `~/iolbox-m1/evidence/` on the Mac and in `docs/macos-m1-result.md`.

| Criterion | Result |
|---|---|
| One command from clean state → working guest | `iolbox-mac.sh provision`, exit 0 |
| Pinned image, digest-verified by Lima | Debian 13 trixie, `sha512:7a0eeb42…`, 322 MiB |
| Guest kernel matches the profile pin exactly | `6.12.101+deb13-cloud-arm64` |
| Rosetta canary passes | `ld.so (Debian GLIBC 2.41-12+deb13u3)`, exit 0 |
| Latest payload installs | **v0.5.2** (in-place upgrade over v0.4.1) |
| Canary gates **every** service start | systemd `ExecStartPre`; 6+ gated starts in the journal |
| Supervisor active + GUI reachable | `active`, `GET /` = **200** (there is no `/api/health`) |
| Image registers, cache is reused | `1 hashed` → `0 hashed, 1 from cache` |
| Two-node IOL lab runs | both `running`, RSS ~537 MiB each |
| **Data plane** | **90% (9/10), round-trip 1/23/202 ms** |
| Restart persistence | service, listener, `GET /`, hostid, iourc, image cache, saved lab, gate evidence all survive a VM restart |

Comparison with M0 (macOS 13.5 / Ubuntu 22.04 / kernel 5.15 / v0.4.1):
**90% (9/10), 1/5/37 ms**. Same success rate and the same single first-packet
ARP loss; **latency is notably worse** (avg 5→23 ms, max 37→202 ms). Both are
single samples. This is recorded as an **open question** in §6, not as a
regression or a non-issue.

---

## 2. The headline finding: M1's driving constraint is gone on macOS 26

M0 established that macOS 13.5's Rosetta aborts **every** amd64 executable on
Linux kernels >= 6.3 with `rosetta error: unhandled auxillary vector type 28`
(`AT_RSEQ_ALIGN`). That single fact drove the whole slice.

**Measured today on macOS 26.6.1 (25G76), all exit 0:**

| Guest | Kernel | macOS 13.5 (M0) | macOS 26.6.1 (measured) |
|---|---|---|---|
| Ubuntu 22.04 | `5.15.0-185-generic` | PASS | **PASS** — `ld.so (Ubuntu GLIBC 2.35-0ubuntu3.14)` |
| Ubuntu 24.04 | `6.8.0-134-generic` | **FAIL**, auxv type 28 | **PASS** — `ld.so (Ubuntu GLIBC 2.39-0ubuntu8.8)` |
| Debian 13 trixie | `6.12.101+deb13-cloud-arm64` | not tested | **PASS** — `ld.so (Debian GLIBC 2.41-12+deb13u3)` |

All genuine Rosetta translation: binfmt `rosetta` registered against
`EM_X86_64`, interpreter `/mnt/lima-rosetta/rosetta`, and the IOL processes run
as `/mnt/lima-rosetta/rosetta …iol17.18.02.bin -e 1 -s 1 -m 1024 -n 65 <id>`.

The `AT_RSEQ_ALIGN` fix point is therefore **bounded** — broken at 13.5, fixed
by 26.6.1 — and still **not narrowed to a release**. Do not guess it. The
product rule is unchanged and was vindicated: **gate on the runtime canary,
never on a version comparison.** Shipping the plan's earlier instinct ("refuse
kernels >= 6.3") would today reject a configuration that demonstrably works.

**Owner decision:** the default guest is now **Debian 13 trixie** (322 MiB vs
Jammy's 671 MB; single-host multiarch on `deb.debian.org` removes the entire
`ports.ubuntu.com` amd64-404 hazard). Jammy is retained as **COMPATIBILITY** —
it is the only guest proven on both old and new macOS, and hosts on older macOS
still need it.

```
NAME       ROLE           GUEST              KERNEL  QUALIFICATION
debian13   DEFAULT        Debian 13 trixie   6.12    PASS (SUPPORTED)
jammy      COMPATIBILITY  Ubuntu 22.04       5.15    PASS (SUPPORTED)
debian12   CANDIDATE      Debian 12 bookworm 6.1     UNMEASURED — CANARY REQUIRED
```

`debian12` is honestly unmeasured and its digest is unpinned; the host refuses
to provision it. **No inference is recorded as an observation anywhere in that
table** — in particular there is deliberately no `debian13 | 13.5 | FAIL_AUXV`
row, because Debian 13 was never run on macOS 13.5.

---

## 3. Environment as it actually is now

| Item | M0 record | **Now** |
|---|---|---|
| Host | `192.168.101.144` | **`192.168.101.166`** (mDNS `Rohans-MacBook-Air.local` resolves it) |
| macOS | 13.5 (22G74) | **26.6.1 (25G76)** |
| Free disk | ~5.4 GB | ~53 GiB |
| Lima | 2.2.0 (Ventura bottle) | 2.2.0, **arm64_tahoe bottle** (`minos 26.0, sdk 26.4`) |
| Bash on the Mac | — | **3.2.57 only** — no Homebrew bash; `#!/usr/bin/env bash` resolves to `/bin/bash` |
| Hardware | Apple M1, 8 GB | unchanged |

Access: `ssh -i ~/.ssh/iolbox_mac_m0 rohansharma@192.168.101.166`. Homebrew is
**not** on the non-login PATH — always use `/opt/homebrew/bin/limactl`.

### Lima machines on the Mac

| Machine | Guest | Notes |
|---|---|---|
| `iol22` | Ubuntu 22.04 | **M0 evidence machine. Do not delete.** No longer boots after the macOS upgrade (§4 gotcha 2). |
| `m1jammy` | Ubuntu 22.04 / 5.15 | canary PASS |
| `m1trixie` | Debian 13 / 6.12 | canary PASS |
| `iolbox-m1-e2e` | Debian 13 / 6.12 | **the M1 acceptance machine** — full stack, v0.5.2, two-node lab |

Payload and image on the Mac in `~/iolbox-m0/`:
`iolbox-server-v0.5.2.tar.gz` (7,602,945 bytes, sha256 `0a4082d4…`, from the
GitHub release) and
`x86_64_crb_linux-adventerprisek9-ms.iol17.18.02.bin`.
Provisioner staged at `~/iolbox-m1/packaging/macos/`; logs and evidence in
`~/iolbox-m1/`.

---

## 4. Gotchas found this session

1. **A stale Homebrew bottle silently disables Rosetta, and Lima only warns.**
   `limactl` was the `arm64_ventura` bottle (`sdk 13.3`) left by the macOS
   upgrade. Lima's VZ bindings gate the Rosetta directory-share API behind a
   **build-time** macOS 14.0 availability check, so it could not create the
   share at all. It logged `Unable to configure Rosetta: … unsupported build
   target macOS version for 14.0 … needs recompilation` — a **warning** — then
   reported `READY`. The guest came up with an empty `/mnt/lima-rosetta` and no
   binfmt entry, so no amd64 binary could execute.
   Fix: **`brew reinstall lima`**. `brew upgrade lima` is a **no-op** when the
   version is already current — only a reinstall re-pours the bottle.
   **"Lima says READY" is not evidence.**
2. **A major macOS upgrade invalidates existing VZ machines.** `iol22` reports
   `Running`, emits **0 bytes** of serial output, and Lima waits forever for
   guest SSH.
3. **Debian 13 renamed `libssl3` → `libssl3t64`** (64-bit `time_t`). Package
   names must be per-suite; bookworm still uses `libssl3`.
4. **`local a=… b="$a/x"` aborts under `set -u`.** Bash expands every argument
   to an assignment builtin before performing any assignment. `bash -n` cannot
   see it. Now trapped by `tests/lint.sh`.
5. **An `EXIT` trap referencing a function-`local` aborts under `set -u`**, because
   the trap fires after the function has returned. This crashed the installer
   *after a fully successful install*, and would also have fired on the
   canary-failure path — converting a clear compatibility refusal into a
   confusing internal error. **Not** trapped by lint; hard to detect textually.
6. **deb822 sources have multiple stanzas.** Appending one `Architectures:` line
   at EOF binds only the last stanza.
7. **`limactl copy <dir> <machine>:<path>`** creates `<path>/<basename>`, and
   `mv SRC DEST` where DEST exists nests instead of renaming. Together they put
   the guest steps one level too deep; every step then failed with exit 127.
8. **The control plane is a *streaming* NDJSON socket.** The supervisor pushes
   `host.stats` about once a second for as long as the connection is open, so:
   `nc -w N` never returns (measured: still running after 300 s), and the first
   frame back is an event, not your reply. **Correlate by request `id`.**
9. **`ok:true` means the request was understood, not that it succeeded.**
   `lab.load` returns ok with `result.warnings`; `lab.start` returns ok with
   every node in `result.failed[]`. Both must be inspected.
10. **`lab.saveDoc`/`lab.listDocs` take/return raw YAML strings; `lab.load`
    takes a structured JSON `lab.Lab` object.** They are not symmetric.
11. **The response envelope and `result` both carry `id`.** A naive first-match
    extraction yields the *request* id — producing a lab that references a
    nonexistent image, with `image_not_found` appearing two steps later.
12. **An IOL image must be `0755`.** The supervisor execs it directly; a 0644
    image yields nodes that never run while every control call still says ok.
13. **`ram: 256` wedges modern 17.x IOL images.** `%SYS-2-MALLOCFAIL` during
    init. `supervisor/internal/node/argv.go:77-80` documents that omitting
    `-m` falls back to IOL's built-in 256 MB, so *removing* the field
    reproduces the bug — set an explicit larger value. **1024 MB works**
    (RSS ~537 MiB/node). A separate task tracks the shipped starter labs, which
    still specify 256.
14. **Liveness is not readiness — three distinct instances.** A node reporting
    `running` may have a wedged IOS; a supervisor reporting `active` may not
    have bound :4001 yet; a node reporting `running` may not have a serving
    console (connect too early and the first write gets `BrokenPipeError`).
    Poll for the real signal.
15. **`ping <ip> repeat N` is an extended-ping keyword valid only at privileged
    EXEC (`R1#`).** At `R1>` IOS answers `% Invalid input detected`.
16. Carried forward from M0 and still true: there is **no `/api/health`**;
    `install.sh` warns on a non-x86_64 `uname -m` and continues, which is
    expected; the console replays its ring buffer on connect so always take the
    **last** `Success rate` match; Rosetta is 64-bit only, so `i86bi` images
    cannot work.

---

## 5. Corrections to earlier documents

`docs/macos-m0-result.md`, `docs/macos-arm64-plan.md` and
`docs/macos-arm64-plan-review.md` are **immutable**. They are correct *for
macOS 13.5*; their baseline moved. These statements are no longer true of this
Mac: "guest kernel must stay < 6.3"; "macOS 13.5 … at the Rosetta floor"; the
~5.4 GB disk-pressure premise; and the `.144` address.
`docs/macos-handoff.md` inherits those and should be read as a macOS 13.5
document.

---

## 6. Open items

| # | Sev | Item |
|---|---|---|
| D11 | MINOR | `IOLBOX_HOST_LIMA` reports `unknown` even when `limactl` is found. That value is exactly what a canary failure message needs to be actionable. |
| Q1 | OPEN QUESTION | Ping latency is worse than M0 (avg 5→23 ms, max 37→202 ms) at the same 90% success rate. Both are single samples. Candidates: cold first-ping after boot, harness contention, 8 GB host under 2×1 GiB nodes, or a genuine newer-Rosetta difference. Needs repeat runs and a warm-path measurement before it means anything. |
| Q2 | OPEN QUESTION | The starter labs shipped with the product still use `ram: 256` (see gotcha 13). Being handled in a separate task. |
| — | NOTE | `shellcheck` is not installed on either box, so `tests/lint.sh` honestly reports it **SKIPPED**. Installing it would raise the bar: several defects found this session are classic shellcheck findings. |
| — | NOTE | The `iolab-runtime-vm` Linux builder was deleted this session to reclaim disk. M1 did not need it because v0.5.2 was published as a GitHub release asset. A future slice that needs a *new* payload build must recreate it; the cloud-init seed and keys were preserved. |

Defects D1–D10 and D12–D18 from earlier in this session are **fixed and
verified on hardware**.

---

## 7. Pins

```
# debian13 (DEFAULT) — 337,313,792 bytes
https://cloud.debian.org/images/cloud/trixie/20260810-2566/debian-13-genericcloud-arm64-20260810-2566.qcow2
sha512:7a0eeb424f4a0e9fe35a4c04dee92cdded59aa4b056655488caf606cad4711b08c88af69a3d7de8af6837b082609872017e009a845837bde371c17b8fc27cd76

# jammy (COMPATIBILITY) — 703,594,496 bytes
https://cloud-images.ubuntu.com/releases/jammy/release-20260807/ubuntu-22.04-server-cloudimg-arm64.img
sha256:b17d9ac9b6249ab30f8c95630acdab3b7a51d76050229ab0ce6c013e303f5ccd
```

Debian publishes **SHA512SUMS only** — there is no SHA256SUMS for cloud images.
Ubuntu's `releases/22.04/…` path 302-redirects; pin `releases/jammy/…`.
Pin variables are uniform across profiles: `IOLBOX_IMAGE_URL`,
`IOLBOX_IMAGE_DIGEST` (algorithm-qualified), `IOLBOX_IMAGE_BYTES`.

---

## 8. Repository layout

```
packaging/macos/
  iolbox-mac.sh                    host entry point (macOS bash 3.2)
  lima/profiles.env                static profile table + exact-host qualification table
  lima/iolbox-{jammy,bookworm,trixie}.yaml   tokenised Lima templates
  lima/pinned-image{,-debian12,-debian13}.env
  guest/lib.sh                     shared helpers + exit-code contract (sourced)
  guest/10-multiarch.sh            Ubuntu multiarch
  guest/10-multiarch-debian.sh     Debian multiarch (no source surgery needed)
  guest/20-kernel-hold.sh          Ubuntu kernel hold
  guest/20-kernel-hold-debian.sh   Debian kernel hold
  guest/30-canary.sh               the Rosetta gate (pure classifier + renderer + main)
  guest/40-install-payload.sh      payload install + structural systemd gate + attestation
  guest/50-verify.sh               service/socket/HTTP/persistence verification
  tests/lint.sh                    bash -n, shellcheck-if-present, house style, 2 regression traps
  tests/canary-classify-test.sh    offline table test of the gate (14 cases)
  tests/profile-lifecycle-test.sh  profile/qualification/parsing fixtures (18 cases)
  tests/source-policy-test.sh      apt source policy fixtures (7 cases)
  tests/kernel-policy-test.sh      kernel policy fixtures (11 cases)
  tests/canary-probe.sh            generic per-profile hardware canary probe
  tests/negative-rosetta-unavailable.sh  fail-closed proof (Rosetta binfmt removed)
  tests/hardware-m1.sh             the M1 acceptance evidence collector
```

Exit-code contract (`guest/lib.sh`, shared host and guest): `0` ok, `1`
usage/internal, `2` **canary failed**, `3` preflight, `4` apt/repo, `5` verify.

Offline gates: **50 cases, 0 failures on macOS bash 3.2.57 AND Linux bash 5.**

---

## 9. Structural gate

The canary is enforced by systemd, not by procedure:

```
/etc/systemd/system/iolbox-supervisor.service.d/10-iolbox-macos-canary.conf
  [Service]
  ExecStartPre=/opt/iolbox-provision/30-canary.sh --quiet
```

Every boot, restart, crash-restart and manual start runs the canary first —
observed repeatedly in the journal as `30-canary.sh[NNNN]: PASS` immediately
before `Started iolbox-supervisor.service`. It survived an in-place
v0.4.1→v0.5.2 upgrade, which rewrites the base unit and daemon-reloads.

Attestations: guest `/var/lib/iolbox/macos-structural-gate.json`, host mirror
`$HOME/.iolbox/macos/<machine>-structural-gate.json`. The host refuses to start
an existing **stopped** machine without a valid attestation, and deletes a stale
one whenever it creates a fresh machine of the same name.

---

## 10. Process notes

- `codex exec` **hangs forever** on stdin when the prompt is an argument unless
  the command ends `< /dev/null`. Passing the prompt as `- < prompt.md` avoids
  the problem entirely and is preferred.
- The codex sandbox has a read-only `.git`; branches and commits happen from the
  main session.
- Do **not** use `sed` to rewrite lines containing regex escapes — it interprets
  `\r`/`\n` and corrupts the file. Use a Python rewrite and verify the result.
- Each harness run restarts the supervisor, which tears down the lab (the nodes
  are children of its cgroup), so every attempt costs a full ~90 s IOS boot.
