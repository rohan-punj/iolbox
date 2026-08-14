# M1 result — deterministic macOS/Lima provisioner: **PASS**

Executed 2026-08-14 on real hardware. This is the M1 counterpart to
`docs/macos-m0-result.md` and, like it, records only what was measured.

## Verdict

**PASS.** From a clean state, one command provisions a pinned Debian 13 guest
on an Apple M1, gates it on the Rosetta amd64-loader canary, installs the
current **v0.5.2** payload behind a structural systemd gate, and runs two real
Cisco IOL 17.18.02 routers that pass traffic. The lab and its supporting state
survive a VM restart.

```
console LAST Success rate: Success rate is 90 percent (9/10), round-trip min/avg/max = 1/23/202 ms
ok: last console Success rate is 90% (>= 80%)
EXIT=0
```

## Test bed

| Item | Value |
|---|---|
| Host | MacBook Air, Apple M1, 8 GB RAM, macOS **26.6.1** (25G76) |
| Rosetta | preinstalled; `/Library/Apple/usr/libexec/oah/RosettaLinux/{rosetta,rosettad}` |
| Machine manager | **Lima 2.2.0**, `arm64_tahoe` bottle (`minos 26.0, sdk 26.4`) |
| Guest | Debian **13 trixie**, kernel **6.12.101+deb13-cloud-arm64**, aarch64, 4 vCPU / 4 GiB / 15 GiB |
| Guest image | `debian-13-genericcloud-arm64-20260810-2566.qcow2`, `sha512:7a0eeb42…`, 337,313,792 bytes |
| Payload | `iolbox-server-v0.5.2.tar.gz`, unmodified, sha256 `0a4082d4…`, `install.sh --bind all` |
| Image | `x86_64_crb_linux-adventerprisek9-ms.iol17.18.02.bin`, image id `b858503827356c55` |
| Machine | `iolbox-m1-e2e` |
| Evidence | `~/iolbox-m1/evidence/m1-20260814T123851Z-12906/` on the Mac |

## What was proven

| Criterion | Result |
|---|---|
| One-command provision from clean state | `iolbox-mac.sh provision`, exit 0 |
| Pinned image digest verified by Lima | sha512 match; boot to `READY` in ~90 s |
| Guest kernel matches the profile pin exactly | `6.12.101+deb13-cloud-arm64` |
| Debian single-host multiarch | amd64 + arm64 from `deb.debian.org`; no source rewriting |
| amd64 runtime installed and ELF-verified | `libc6:amd64`, `libssl3t64:amd64` (trixie rename) |
| Rosetta canary | **PASS** — `ld.so (Debian GLIBC 2.41-12+deb13u3)`, exit 0 |
| Canary gates every service start | `ExecStartPre`; `30-canary.sh[NNNN]: PASS` before each `Started` |
| Structural gate survives an in-place upgrade | v0.4.1 → v0.5.2 rewrote the unit; gate still effective |
| Payload installs | `payload=v0.5.2 supervisor=v0.5.2` |
| Supervisor + GUI | `active`; `GET /` = **200** (no `/api/health` exists) |
| IOL licence | `install.sh` generated `/opt/iolbox/iourc`; hostid `a8c00f05` |
| Image registration + cache | `1 hashed` then `0 hashed, 1 from cache` |
| Two-node lab | both nodes `running`; RSS ~537 MiB each; `-m 1024` |
| Real IOL executes under Rosetta | full IOS-XE banner, `Version 17.18.2, RELEASE SOFTWARE (fc3)` |
| **Data plane** | **90% (9/10), round-trip 1/23/202 ms** |
| Restart persistence | service active, control listener bound, `GET /` 200, hostid, iourc, image cache, saved lab, and journal gate evidence all intact |

Offline gates: **50 cases, 0 failures**, run on **macOS bash 3.2.57 and Linux
bash 5** (`lint`, `canary-classify`, `profile-lifecycle`, `source-policy`,
`kernel-policy`).

## The compatibility finding

M0 measured that macOS 13.5's Rosetta aborts every amd64 executable on kernels
>= 6.3 (`AT_RSEQ_ALIGN`, auxv type 28). Measured today on macOS 26.6.1:

| Guest | Kernel | macOS 13.5 (M0) | macOS 26.6.1 |
|---|---|---|---|
| Ubuntu 22.04 | 5.15.0-185 | PASS | **PASS** |
| Ubuntu 24.04 | 6.8.0-134 | **FAIL** auxv 28 | **PASS** |
| Debian 13 | 6.12.101 | not tested | **PASS** |

The fix point is now **bounded** (broken at 13.5, fixed by 26.6.1) but is still
**not narrowed to a release**. The product continues to gate on the runtime
canary and never on a version comparison. Had M1 shipped a "refuse kernels
>= 6.3" rule, it would today reject a working configuration.

## Comparison with M0

| | M0 | M1 |
|---|---|---|
| macOS | 13.5 (22G74) | 26.6.1 (25G76) |
| Guest / kernel | Ubuntu 22.04 / 5.15 | Debian 13 / 6.12 |
| Payload | v0.4.1 | v0.5.2 |
| Ping | 90% (9/10), 1/5/37 ms | 90% (9/10), 1/23/202 ms |
| RSS per IOL node | ~530 MiB | ~537 MiB |

Same success rate, same single first-packet ARP loss, essentially identical
memory cost. **Latency is notably worse** and is recorded as an open question
(handoff §6, Q1), not a regression: both figures are single samples, and the
candidate causes — cold first ping, harness contention, an 8 GB host running
two 1 GiB nodes, or a genuine newer-Rosetta difference — have not been
separated.

## What is NOT proven

- Four-node labs; capture/Wireshark tee; NAT/extnet; VPCS in a lab; multi-link
  fabrics; sustained soak (longest run here was minutes).
- The **browser GUI** — only HTTP status and the control API were exercised.
- Lima port-forwarding of console/capture ranges to the macOS side; the lab was
  driven from inside the guest.
- Debian 12 bookworm — defined as a CANDIDATE profile, digest unpinned, canary
  never run. The launcher refuses to provision it.
- Any macOS between 13.5 and 26.6.1, and therefore the exact `AT_RSEQ_ALIGN`
  fix point.
- Repeatability of the latency figure.

## Defects found and fixed

Eighteen defects were found by running on hardware; all are fixed and
re-verified. Five were in provisioning/product code, thirteen in the acceptance
harness. They are catalogued as gotchas in `docs/macos-m1-handoff.md` §4. The
recurring shapes worth remembering:

1. **`set -u` × bash scoping** — same-line `local` self-reference; an `EXIT`
   trap referencing a function-local; empty-array expansion on bash 3.2. None
   are visible to `bash -n`.
2. **Assumptions about what a tool actually does** — how `limactl copy` lays out
   a directory, when `nc` returns against a streaming socket, how `json.dumps`
   spaces its output, whether an IOL image needs `+x`.
3. **`ok` that is not success** — `ok:true` with `result.failed[]` populated;
   `lab.load` warnings; nodes `running` with a wedged IOS; a supervisor `active`
   before its port is bound.

None were reachable by code inspection or dry-running.
