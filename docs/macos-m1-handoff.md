# M1 handoff — Apple Silicon macOS track

Living document. Started 2026-08-14 during the M1 implementation session and
updated as work proceeds. Read this **before** `docs/macos-handoff.md`, which
was written against macOS 13.5 and is now partly obsolete — see
"Corrections to earlier documents" below.

Branch: **`luna/macos-m1-provisioner`**, off `main`, in worktree
`J:\Claude code\iolab-m1-wt`. The M7 arm64 work on
`luna/macos-arm64-invariant` is untouched and still uncommitted.

---

## 1. The headline: the constraint that defined M1 is gone on macOS 26

M0 established that macOS 13.5's Rosetta aborts **every** amd64 executable on
Linux kernels >= 6.3 with `rosetta error: unhandled auxillary vector type 28`
(`AT_RSEQ_ALIGN`). That single fact drove the whole slice: pin Ubuntu 22.04,
hold kernel 5.15, gate on a canary.

**Measured on this Mac today, macOS 26.6.1 (25G76), all exit 0:**

| Guest | Kernel | macOS 13.5 (M0) | macOS 26.6.1 (measured) |
|---|---|---|---|
| Ubuntu 22.04 | `5.15.0-185-generic` | PASS | **PASS** — `ld.so (Ubuntu GLIBC 2.35-0ubuntu3.14)` |
| Ubuntu 24.04 | `6.8.0-134-generic` | **FAIL**, auxv type 28 | **PASS** — `ld.so (Ubuntu GLIBC 2.39-0ubuntu8.8)` |
| Debian 13 trixie | `6.12.101+deb13-cloud-arm64` | not tested | **PASS** — `ld.so (Debian GLIBC 2.41-12+deb13u3)` |

All three are genuine Rosetta translation: the `rosetta` binfmt entry is
registered against `EM_X86_64` magic `…02003e00`, interpreter
`/mnt/lima-rosetta/rosetta`.

So the `AT_RSEQ_ALIGN` fix point is now **bounded** — broken at 13.5, fixed by
26.6.1. It is still **not narrowed to a specific release**, and must not be
guessed at. The product rule is unchanged and was vindicated: **gate on the
runtime canary, never on a version comparison.** Had M1 shipped the plan's
earlier instinct of "refuse kernels >= 6.3", it would today be rejecting a
configuration that demonstrably works on the owner's own machine.

The kernel pin and hold remain correct for hosts on older macOS. They are no
longer the authority; the canary is.

### Owner decision (2026-08-14)

**Default guest moves to the newest, lightest build: Debian 13 trixie.**
Ubuntu 22.04/Jammy is demoted to a compatibility profile — **not deleted**. It
is the only guest proven on both old and new macOS, and hosts on macOS < 14
still need it.

Why trixie: 322 MiB image versus Jammy's 671 MB; kernel 6.12; and Debian serves
amd64 **and** arm64 from the same host (`deb.debian.org`), which removes the
entire `ports.ubuntu.com` amd64-404 hazard that `guest/10-multiarch.sh` exists
to work around.

---

## 2. Environment as it actually is now

| Item | M0 record | **Now** |
|---|---|---|
| Host | `192.168.101.144` | **`192.168.101.166`** (mDNS `Rohans-MacBook-Air.local` resolves it) |
| macOS | 13.5 (22G74) | **26.6.1 (25G76)** |
| Free disk | ~5.4 GB | **~56 GiB** |
| Lima | 2.2.0 | 2.2.0, but **bottle rebuilt** — see below |
| Hardware | Apple M1, 8 GB | unchanged |
| Rosetta | `oahd` running | `oahd` running; `/Library/Apple/usr/libexec/oah/RosettaLinux/{rosetta,rosettad}` present |

Access unchanged: `ssh -i ~/.ssh/iolbox_mac_m0 rohansharma@192.168.101.166`.
Homebrew is still **not on the non-login PATH** — always use
`/opt/homebrew/bin/limactl` absolutely.

### Lima machines on the Mac

| Machine | Guest | State | Notes |
|---|---|---|---|
| `iol22` | Ubuntu 22.04 | Stopped | **The M0 evidence machine. Do not delete.** It no longer boots (see §3). |
| `m1jammy` | Ubuntu 22.04 / 5.15 | Running | Created this session; canary PASS |
| `m1trixie` | Debian 13 / 6.12 | Running | Created this session from our pinned template; canary PASS |

Payload and a real Cisco image live on the Mac at `~/iolbox-m0/`:
`iolbox-server-v0.4.1.tar.gz` and
`x86_64_crb_linux-adventerprisek9-ms.iol17.18.02.bin`.
Our provisioner is staged at `~/iolbox-m1/macos/`, copied into guests at
`/opt/iolbox-provision/`.

---

## 3. Gotchas discovered this session

1. **A stale Homebrew bottle silently disables Rosetta, and Lima only warns.**
   `limactl` was the `arm64_ventura` bottle (`minos 13.0, sdk 13.3`) left behind
   by the macOS upgrade. Lima's VZ bindings gate the Rosetta directory-share API
   behind a **build-time** macOS 14.0 availability check, so that binary could
   not create the share at all. It logged
   `Unable to configure Rosetta: … unsupported build target macOS version for
   14.0 (the binary was built with __MAC_OS_X_VERSION_MAX_ALLOWED=130300; needs
   recompilation)` — a **warning** — and then reported `READY` anyway. The guest
   came up with an empty `/mnt/lima-rosetta` and no `rosetta` binfmt entry, so
   *no* amd64 binary could execute.
   Fix: `brew reinstall lima` to get `arm64_tahoe` (`minos 26.0, sdk 26.4`).
   Note `brew upgrade lima` is a **no-op** here — the version is already current;
   only a reinstall re-pours the bottle.
   **Product consequence:** "Lima says READY" is not evidence. `doctor` must
   surface that hostagent warning explicitly.
2. **A major macOS upgrade invalidates existing VZ machines.** `iol22` reports
   `Running` but emits **0 bytes** of serial console output and Lima waits
   forever for guest SSH. Not a slow boot. Argues for the provisioner being
   deterministic and re-runnable, and matters for M2's lifecycle design.
3. **Debian 13 renamed `libssl3` to `libssl3t64`** (64-bit `time_t`
   transition). apt installed `libssl3t64:amd64 3.5.6-1~deb13u2` fine; the
   step's own architecture assertion then failed because it looked for the old
   name, and exited 4. bookworm still uses `libssl3`. Package names must be
   per-suite.
4. **`local a=… b="$a/x"` aborts under `set -u`.** Bash expands *every* argument
   to an assignment builtin before performing any assignment, so `$a` is still
   unset. This crashed the canary itself on hardware
   (`30-canary.sh: line 192: record_dir: unbound variable`, exit 1). Fixed;
   swept tree-wide, one occurrence. `bash -n` cannot catch it.
5. **deb822 sources have multiple stanzas.** Appending one `Architectures: arm64`
   line at EOF binds only the **last** stanza, so apt still queries
   `ports.ubuntu.com` for amd64 and 404s. Retire deb822 wholesale, or edit every
   stanza.
6. `limactl list --format '{{.Name}}\t{{.Status}}'` emits `\t` **literally** —
   Go templates do not interpret escapes. Any parser splitting on whitespace
   sees one field.
7. Carried forward from M0 and still true: there is **no `/api/health`**
   (readiness is `GET /` with status < 500); `install.sh` warns on a non-x86_64
   `uname -m` and continues, which is expected; console output replays on
   connect, so always regex the **last** `Success rate` match; Rosetta is
   64-bit only, so `i86bi` images cannot work.

---

## 4. Corrections to earlier documents

`docs/macos-m0-result.md`, `docs/macos-arm64-plan.md` and
`docs/macos-arm64-plan-review.md` are **immutable** — they are the historical
record and are correct *for macOS 13.5*. They are not wrong; their baseline
moved. Specifically, these statements are no longer true of this Mac:

- "Guest kernel must stay < 6.3" — false on macOS 26.6.1; 6.8 and 6.12 both pass.
- "macOS 13.5 … at the Rosetta floor" — the host is now macOS 26.6.1.
- The disk-tightness premise (~5.4 GB free) that required the negative test to
  be cheap — ~56 GiB free now.
- `192.168.101.144` — the Mac is at `.166`.

`docs/macos-handoff.md` inherits those and should be read as a macOS 13.5
document.

---

## 5. What is proven versus written

### Executed on hardware this session

- Rosetta canary PASS on three guests/kernels (table in §1), via the real
  `guest/30-canary.sh`, including its `--json` output.
- The canary **failing closed** in a genuinely broken environment (exit 2) when
  Rosetta was unavailable, and correctly classifying `FAIL_MISSING` (loader
  absent) versus a compatibility failure — it refused to convert a provisioning
  gap into a compatibility verdict.
- `guest/10-multiarch.sh` on Ubuntu 22.04: amd64 multiarch installed and
  ELF-verified.
- `guest/10-multiarch-debian.sh` on Debian 13: single-host multiarch works, amd64
  packages fetched from `deb.debian.org`; step then failed its own assertion on
  the `libssl3t64` rename (gotcha 3).
- Debian 13 boots under Lima VZ from our **own pinned template** with a
  digest-verified image, ~90 s to `READY`.
- Lima bottle diagnosis and repair.

### Written but NOT yet executed

- Payload install (`guest/40-install-payload.sh`) and verification
  (`guest/50-verify.sh`) — no end-to-end run yet.
- Two-node IOL lab on the new default guest; restart persistence.
- `iolbox-mac.sh` against real Lima (only `--dry-run`, and it has known parsing
  defects — §6).
- `guest/20-kernel-hold.sh` / `20-kernel-hold-debian.sh`.
- `tests/negative-kernel68.sh` — and its premise is now **invalid** on macOS 26:
  a 6.8 guest is no longer expected to be rejected. It will report
  UNEXPECTED-PASS and exit non-zero, which is correct behaviour but means the
  test must be redefined.
- `tests/canary-probe.sh` and the `--profile` wiring.

**M1 is NOT complete** and must not be reported as such.

---

## 6. Open defects

From an adversarial review plus hardware findings. Numbering is stable; do not
renumber.

| # | Sev | File | Defect |
|---|---|---|---|
| D1 | MAJOR | `iolbox-mac.sh:482,488` | `\t` emitted literally by the Go template; machine state never parses, so every machine looks absent |
| D2 | MAJOR | `iolbox-mac.sh:523-529` | existing VM is started before the numbered steps; `install.sh` leaves the supervisor `Restart=always`, so systemd can start the amd64 payload **before** the canary |
| D3 | MAJOR | `guest/10-multiarch.sh:44-55,134` | lines already carrying any `arch=` are left alone and verification accepts any arch value, so a pre-existing `[arch=amd64] ports.ubuntu.com` survives |
| D4 | MAJOR | `tests/negative-kernel68.sh:45-47` | `limactl list \| grep -Fxq` under `pipefail` returns 141 on SIGPIPE, so the "already exists" safety refusal can be skipped |
| D5 | MAJOR | `guest/10-multiarch-debian.sh` | `libssl3` vs `libssl3t64` — package names must be per-suite (found on hardware) |
| D6 | MAJOR | `lima/profiles.env` | support status and canary expectation are keyed to "the macOS 13.5 baseline"; `debian13` is marked BLOCKED/expect-FAIL_AUXV yet measurably PASSES on 26.6.1 |
| D7 | MINOR | `guest/20-kernel-hold.sh:172-177` | same `grep -q` + `pipefail` SIGPIPE pattern reports a held package as unheld |
| D8 | MINOR | `guest/20-kernel-hold.sh:20` | `IOLBOX_PROVISION_DATE` defaults to now and is rewritten every run, so the step is not strictly idempotent |
| D9 | MINOR | `guest/30-canary.sh` | the stored verdict record does not include the host macOS build, so a cached verdict can be misread after an OS upgrade — exactly how this Mac became confusing |
| D10 | MINOR | `iolbox-mac.sh` | `doctor` does not surface Lima's `Unable to configure Rosetta` hostagent warning, which is the single most informative line when Rosetta is broken |

**Fixed already:** the `set -u` assignment-builtin crash in `guest/30-canary.sh`
(gotcha 4).

---

## 7. Pins

Jammy (compatibility profile), verified against `SHA256SUMS` 2026-08-14:

```
https://cloud-images.ubuntu.com/releases/jammy/release-20260807/ubuntu-22.04-server-cloudimg-arm64.img
sha256:b17d9ac9b6249ab30f8c95630acdab3b7a51d76050229ab0ce6c013e303f5ccd
```
(671 MB. Note `releases/22.04/…` 302-redirects to `releases/jammy/…`; pin the
latter.)

Debian 13 trixie (new default), SHA512 from the serial directory and
cross-checked against `latest/SHA512SUMS`, 2026-08-14:

```
https://cloud.debian.org/images/cloud/trixie/20260810-2566/debian-13-genericcloud-arm64-20260810-2566.qcow2
sha512:7a0eeb424f4a0e9fe35a4c04dee92cdded59aa4b056655488caf606cad4711b08c88af69a3d7de8af6837b082609872017e009a845837bde371c17b8fc27cd76
```
(337,313,792 bytes.) **Debian publishes SHA512SUMS only — there is no
SHA256SUMS for cloud images**, so any pin variable named `..._SHA256` is
misnamed for the Debian profiles.

Debian 12 bookworm (candidate, unpinned): checksum available at
`https://cloud.debian.org/images/cloud/bookworm/20260806-2562/SHA512SUMS`,
image `debian-12-genericcloud-arm64-20260806-2562.qcow2`.

---

## 8. Repository layout produced by M1

```
packaging/macos/
  iolbox-mac.sh                    host entry point (macOS bash 3.2)
  lima/profiles.env                the guest profile table
  lima/iolbox-jammy.yaml           tokenised Lima template (@TOKEN@ rendered at run time)
  lima/iolbox-bookworm.yaml
  lima/iolbox-trixie.yaml
  lima/pinned-image.env            jammy pin
  lima/pinned-image-debian12.env
  lima/pinned-image-debian13.env
  guest/lib.sh                     shared helpers + exit-code contract (sourced)
  guest/10-multiarch.sh            Ubuntu multiarch
  guest/10-multiarch-debian.sh     Debian multiarch (no source surgery needed)
  guest/20-kernel-hold.sh          Ubuntu kernel hold
  guest/20-kernel-hold-debian.sh   Debian kernel hold
  guest/30-canary.sh               the Rosetta gate (pure classifier + renderer + main)
  guest/40-install-payload.sh
  guest/50-verify.sh
  tests/lint.sh                    bash -n + shellcheck-if-present + house style
  tests/canary-classify-test.sh    offline table test of the gate, 14 cases
  tests/canary-probe.sh            generic per-profile hardware probe
  tests/negative-kernel68.sh       premise now invalid on macOS 26 (see §5)
```

Exit-code contract (in `guest/lib.sh`, shared host and guest): `0` ok,
`1` usage/internal, `2` **canary failed**, `3` preflight failed, `4` apt/repo,
`5` verification failed.

---

## 9. Process notes

- `codex exec` **hangs forever** on stdin when the prompt is an argument unless
  the command ends `< /dev/null`. Passing the prompt as `- < prompt.md` avoids
  the problem entirely and is preferred.
- The codex sandbox has a read-only `.git`; branches and commits happen from the
  main session.
- `shellcheck` is not installed on the dev box, so `tests/lint.sh` reports it
  **SKIPPED** rather than passed. Installing it would materially raise the bar,
  since several of the defects in §6 are classic shellcheck findings.
