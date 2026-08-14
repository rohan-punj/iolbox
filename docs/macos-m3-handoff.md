# M3 handoff — Apple Silicon macOS track

Updated 2026-08-14, end of the M3 implementation session. Read this
**before** `docs/macos-m1-handoff.md` (M1) — this supersedes it for anything
M2/M3 changed; M1's own findings (Rosetta bounded-by-macOS-version, the
profile/qualification model, the structural canary gate) are still current
and not repeated in full here.

Branch: **`luna/macos-m3-ux`**, off `luna/macos-m1-provisioner` (which holds
M1+M2), in worktree `J:\Claude code\iolab-m3-wt`. Commits `4d25795`
(implementation) + `e0f7b4d` (result doc). **Not merged anywhere** — M1 was
never merged to `main` either, so the stack is `main` ← (unmerged)
`luna/macos-m1-provisioner` ← (unmerged) `luna/macos-m3-ux`.

---

## 1. Status: M1, M2, M3 acceptance criteria are all MET on hardware

| Slice | Verdict | Result doc |
|---|---|---|
| M0 (feasibility) | GO | `docs/macos-m0-result.md` |
| M1 (provisioner) | PASS | `docs/macos-m1-result.md` |
| M2 (Darwin launcher) | PASS | `docs/macos-m2-result.md` |
| M3 (browser/sync/console/capture UX) | PASS | `docs/macos-m3-result.md` |

M3 in one line: explicit loopback-only Lima port-forward contract (GUI +
console 9000-9049 + capture 5500-5529, guest control port no longer
forwarded at all), Darwin host folder sync at lifecycle boundaries
(`~/Library/Application Support/iolbox/{images,labs}`), and a real
browser-equivalent HTTP/WebSocket flow (upload/register/save/list/load/
start/dual-console/live-capture/stop/reload) proven on hardware, including a
difficult host path (`M3 data café` — space + non-ASCII).

---

## 2. Seven defects found this session, all only surfaced by real hardware

Full detail and fix direction in `docs/macos-m3-result.md`. Headline
lesson: **static review (even a careful adversarial one) cannot catch these
— only running against a real Lima guest and a real IOS boot can.**

1. Lima `--set` yq expressions need **quoted map keys**.
2. The GUI bridge (`wsbridge.go`) requires a session cookie + same-origin
   `Origin` header on **every** WS route, not just `/control` — had to be
   fixed in three independent places (Go launcher, hardware harness's own
   probe, E2E test's console/capture dials).
3. IOS needs an **active console wake** (`\r\n` on connect, re-poke while
   waiting) — it will not print a fresh prompt passively.
4. Telnet echo arrives one or two bytes per WS frame — substring checks
   must accumulate across reads, not check each frame in isolation.
5. `show version` pagination (`--More--`) strands a console mid-page unless
   `terminal length 0` runs first; completion checks must require a prompt
   as the *last* line, not merely present anywhere in the buffer.
6. Lima's dynamic per-port forwarding has a brief propagation lag after a
   fresh `capture.start` — needs a short bounded retry on the direct dial.
7. **256 MB per-node RAM silently wedges IOL 17.18.02** (`%SYS-2-MALLOCFAIL`
   mid-init, `lab.start` still reports `ok:true`/`running`). The shared
   `labs/example-p0.lab.json` fixture had this; fixed to 1024 MB, matching
   a same-day upstream fix (`b470412` / `fix/iol-ram-floor`, on `main`, not
   yet in this branch's ancestry) that root-caused the identical failure on
   the identical image independently, the same day.

---

## 3. VPCS/PC nodes do not work on this Mac at all — separate from M1/M2/M3

Discovered live during this session (not part of M3's own test matrix, which
only exercises IOL nodes): starting a VPCS/"PC" node from the GUI fails with
`runtime does not support PC nodes: <reasons>`.

**Root cause, confirmed live on the Mac**: the `ioltool` system account that
`supervisor/internal/tool/detect_linux.go`'s capability probe requires
(`user.Lookup("ioltool")`) does not exist on the Lima Debian 13 trixie
guest (`sudo id ioltool` → "no such user"). cgroup v2 controllers **are**
present at the kernel level; the missing piece is guest provisioning —
`packaging/macos/guest/` never creates this account or verifies the
netns/veth tooling PC nodes need, because **M0/M1/M2/M3 only ever proved
IOL nodes** (Rosetta amd64 translation, a completely separate code path).

**A fix has landed**, committed directly to this same branch
(`luna/macos-m3-ux`) by a separate concurrent session as `9916fb9` (see §4):
four layered gaps, all live-reproduced — (1) `install.sh` never created the
`ioltool` account, (2) `install.sh`/`pack-native.sh` never built/shipped
`iolbox-toollaunch`, (3) the native launcher's `--cgroup` fallback ran
inside `ip netns exec`, which remounts `/sys` and hides the cgroup2 mount,
breaking `cgroup.procs` writes with ENOENT — fixed with a `--netns` flag
that joins the namespace via `setns()` before dropping root instead, (4)
`install.sh` never installed `/opt/iolbox/tools/packs` at all. Each fix was
individually confirmed via direct NDJSON probes and a raw `clone3`
reproduction, **but the commit message itself says full end-to-end
PC-node-reaches-running confirmation is still owed**, blocked partly by this
M3 session's own concurrent hardware runs resetting the shared guest.
**Before relying on VPCS for M4's bidirectional-ping criterion, run that
end-to-end confirmation first** — don't assume the commit alone is
equivalent to a hardware PASS.

---

## 4. Shared-branch/worktree gotcha this session

Mid-session, `git status` in this worktree showed six files modified that
this session never touched (`supervisor/internal/tool/launch*.go`,
`runtime/pack-native.sh`, `runtime/files/native/install.sh`,
`tools/iolbox-toollaunch/*`), with mtimes falling inside the session's own
active work window. These turned out to be a **separate concurrent session**
working the `ioltool`/netns/cgroup fix (§3) directly in this exact worktree
directory on disk, on this exact branch (`luna/macos-m3-ux`) — confirmed
after the fact when that session's own commit (`9916fb9`) appeared in `git
log` on this branch, ahead of nothing conflicting because this session never
staged those files.

**Never `git add -A` or `git add .` in a worktree that might have concurrent
activity** — stage the exact file list by path. This session committed only
the files that were actually part of M3's own change list; the six
unrelated files were left completely untouched and uncommitted for their own
owner, who committed them cleanly afterward with no conflict. **A new M4
session should assume this pattern can recur** — check `git log`/`git
status` for unexpected commits or dirty files before assuming the worktree
is exactly as this handoff describes it, and never broad-stage.

---

## 5. Environment as it actually is now

Unchanged from the M1 handoff except:

| Item | Value |
|---|---|
| Host | `rohansharma@192.168.101.166`, key `~/.ssh/iolbox_mac_m0` |
| macOS | 26.6.1 (25G76) |
| Lima | 2.2.0, `/opt/homebrew/bin/limactl` |
| Bash on the Mac | 3.2.57 only |
| Free disk (end of session) | ~45 GiB |
| Free RAM (idle, all VMs stopped) | ~172 MB unused reported by `top`, `vm_stat` free pages misleadingly low — always check `top -l 1 -s 0 \| grep PhysMem`, not raw `vm_stat` free pages, before creating a new VM on this 8 GB Mac |

### Lima machines on the Mac (end of session, all Stopped)

| Machine | Notes |
|---|---|
| `iol22` | **M0 evidence machine. Never touch.** |
| `iolbox-m1-e2e` | M1 acceptance machine, reusable/deletable |
| `iolbox-m2-e2e` | M2 acceptance machine, reusable/deletable |
| `iolbox-m3-e2e` | **M3 acceptance machine — left Stopped with the full passing evidence run's state**, reusable/deletable |
| `m1jammy`, `m1trixie` | M1 profile canaries, reusable/deletable |

Payload/image staging: `~/iolbox-m0/iolbox-server-v0.5.2.tar.gz` and
`~/iolbox-m0/x86_64_crb_linux-adventerprisek9-ms.iol17.18.02.bin`. The
`~/iolbox-m1/` directory on the Mac is the working asset root used across
M1/M2/M3 sessions (packaging assets, launcher binaries, evidence); it is
**not** a full repo checkout — only specific fixture files were staged into
it as needed (see `docs/macos-m3-result.md` for exactly which).

Evidence: `~/iolbox-m1/evidence-m3/m3-20260814T184248Z-24776/` on the Mac is
the final passing run.

---

## 6. Open items going into M4

| # | Sev | Item |
|---|---|---|
| — | BLOCKING (partial) | VPCS/PC nodes broken (§3) — a fix is in flight in a separate session; check its status before relying on VPCS for M4's bidirectional-ping criterion. |
| — | NOTE | Four-node qualification may need more than this Mac's 8 GB RAM — the plan itself flags this as an owner decision, not yet made. |
| Q1 (carried from M1, still open) | OPEN QUESTION | Ping latency variance (avg 5→23 ms, max 37→202 ms across M0→M1) never got repeat/warm-path measurements. M2/M3 didn't touch this either. If M4's soak/capacity work naturally produces more samples, record them; don't go out of scope to chase it. |
| — | NOTE | No Wireshark (`capinfos`/`tshark`) installed on the Mac — M3's capture validation used the in-process pcapng structural parser as authoritative. If M4's soak run wants an external cross-check, installing Wireshark first would help. |

---

## 7. Process notes carried forward + one new one

- `codex exec` **hangs forever** on stdin unless the prompt is piped
  (`- < prompt.md`) or the command ends `< /dev/null`.
- Give codex **sol** a `workspace-write` sandbox for any pass whose
  deliverable is a file (a plan, a review doc) — a `read-only` sandbox can
  silently lose the entire deliverable if the write is rejected at the very
  end, with only a one-line summary surviving in the final message.
- The codex sandbox's `.git` is read-only; commits happen from the main
  session, and should stage an **explicit file list**, never `-A`/`.`.
- Do not use `sed` on lines with regex escapes (`\r`/`\n` corruption risk).
- Each hardware harness run that restarts the supervisor tears down any
  running lab (nodes are children of its cgroup) — budget a full ~90 s IOS
  boot per attempt, and expect several attempts: this session needed **8**
  full end-to-end retries to reach a clean M3 PASS, each one surfacing
  exactly one new real bug, never the same bug twice — a good sign the
  iterate-on-hardware loop was doing real work.
