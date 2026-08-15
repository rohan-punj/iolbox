# M5 handoff — Apple Silicon macOS track

Updated 2026-08-15, end of the M5 continuation session. Read this
**before** `docs/macos-m4-handoff.md` — this supersedes it for anything M5
changed; M1–M4's own findings are still current and not repeated here.

Branch: **`luna/macos-m5-honest-caps`**, off `luna/macos-m4-runtime`, in
worktree `J:\Claude code\iolab-m5-wt`. **Not merged anywhere.**

---

## 1. Status: PASS — read `docs/macos-m5-result.md` in full

All five acceptance criteria are now proven on real hardware. Criteria 1, 3,
4, 5 on the Mac/Lima stack; criterion 2 (a real ordinary amd64 non-Mac
target) on `noble-builder-vm` (192.168.226.10), the project's existing
Ubuntu 24.04 build box, owner-directed for this specific check. Full
per-criterion evidence in `docs/macos-m5-result.md` §1–4.

**M5 is not merged anywhere and nothing has been committed** — see §7 below
for exactly what's uncommitted. "PASS" here means the acceptance criteria are
proven, not that the branch has landed.

---

## 2. What the continuation session did, and why it mattered

Luna's implementation session could not attempt criterion 4's real
rendered-browser proof — "the in-app browser and Chrome control surfaces
both reported unavailable" in that session. The continuation session had an
actual browser control surface available, so it:

1. Started the already-provisioned (but Stopped) `iolbox-m5-e2e` Lima
   machine.
2. Opened an SSH tunnel from Windows to the Mac's GUI port:
   `ssh -i .m5-ssh/iolbox_mac_m0 -N -L 44101:127.0.0.1:4101
   rohansharma@192.168.101.166`.
3. Drove the real embedded GUI through that tunnel with DOM reads and
   in-page JS assertions (`element.disabled`, `.title`, `.draggable`, actual
   `<select><option disabled>` state) — not just an HTTP GET, which the plan
   explicitly says is insufficient proof.

This found two real GUI defects the offline test suite could not have caught
(pure Svelte template logic, no unit test covered either path) and one
test-harness artifact that looked like a defect but wasn't. All three are
detailed in `docs/macos-m5-result.md` §2. Both real defects were fixed,
redeployed to the same hardware, and reverified live before this session
ended.

**Takeaway for future GUI-adjacent hardware sessions on this project:** an
offline `npm run check` + unit-test pass proves the code compiles and pure
helpers behave — it does not prove a Svelte component actually wires a
disabled/title binding to the right control. Every one of this session's two
defects was in exactly that gap. If a browser control surface is available,
use it; if criterion language says "GUI never offers X" or similar, treat a
static HTTP check as insufficient the same way this plan already flags it.

---

## 3. Redeploy procedure used (repeatable for future GUI-only fixes on this VM)

No server-side (Go) code changed in the continuation session — only two
Svelte files. The redeploy loop, done twice:

```bash
cd app && npm run build:embed   # writes into supervisor/internal/web/dist
cd ../supervisor && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
  -o supervisor-linux-amd64-<tag> -ldflags "-X main.version=<tag>" ./cmd/supervisor
scp -i .m5-ssh/iolbox_mac_m0 supervisor-linux-amd64-<tag> \
  rohansharma@192.168.101.166:~/iolbox-m1/m5/
ssh -i .m5-ssh/iolbox_mac_m0 rohansharma@192.168.101.166 \
  '/opt/homebrew/bin/limactl copy ~/iolbox-m1/m5/supervisor-linux-amd64-<tag> \
   iolbox-m5-e2e:/tmp/supervisor-<tag>'
ssh -i .m5-ssh/iolbox_mac_m0 rohansharma@192.168.101.166 \
  '/opt/homebrew/bin/limactl shell iolbox-m5-e2e -- bash -c \
   "sudo systemctl stop iolbox-supervisor.service; \
    sudo cp /tmp/supervisor-<tag> /opt/iolbox/supervisor; \
    sudo chmod 0755 /opt/iolbox/supervisor; \
    sudo systemctl start iolbox-supervisor.service"'
```

Confirm `systemctl is-active` = `active` and `/opt/iolbox/supervisor
--version` prints the new tag before trusting anything served through the
GUI. This preserves the guest's existing images/labs/iourc/cache — it only
swaps the binary, same as the M1-proven in-place-upgrade path.

Note: `limactl` is not on the default SSH `PATH` on this Mac — invoke it as
`/opt/homebrew/bin/limactl`, not bare `limactl`.

---

## 4. A quirk this session hit and worked around: post-restart WS reconnect

After swapping the supervisor binary and restarting the systemd unit, the
already-open browser tab's WebSocket to `/control` initially failed to
reconnect (console showed repeated `WebSocket connection ... failed`) even
though a plain HTTP GET of `/` through the same tunnel kept returning 200.
A raw manual `new WebSocket(...)` from the page's own JS context connected
successfully, and a full page reload (not just a WS retry) reliably
recovered the app's own connection afterward. If a future session sees a
GUI that looks "stuck disconnected" right after a supervisor restart,
reload the tab fully before assuming a real regression — this was not one.

---

## 5. Criterion 2: how it was closed, and how to reproduce it elsewhere

`noble-builder-vm` (192.168.226.10) is this project's existing Ubuntu 24.04
Noble build box for the *separate* PNetLab project — it is not an iolbox
machine and shouldn't become one. The check was done as a clean
install-verify-uninstall round trip, not a permanent deployment:

```bash
# from Windows, key auth already set up for ubuntu@192.168.226.10
scp <local-copy-of-iolbox-server-m5-luna.tar.gz> ubuntu@192.168.226.10:~/
ssh ubuntu@192.168.226.10 'mkdir -p ~/iolbox-m5-criterion2 && cd ~/iolbox-m5-criterion2 && tar xzf ~/iolbox-server-m5-luna.tar.gz'
ssh ubuntu@192.168.226.10 'cd ~/iolbox-m5-criterion2/iolbox-server-v0.5.3-netprobe-cgroupfd-fix && sudo ./install.sh --bind local'
# ... capture hello / systemctl show / image.register+node.start evidence over
# the guest-loopback control socket at 127.0.0.1:4000, same NDJSON-over-/dev/tcp
# trick used throughout this project ...
ssh ubuntu@192.168.226.10 'cd ~/iolbox-m5-criterion2/iolbox-server-v0.5.3-netprobe-cgroupfd-fix && sudo ./uninstall.sh --yes'
ssh ubuntu@192.168.226.10 'rm -rf ~/iolbox-m5-criterion2 ~/iolbox-server-m5-luna.tar.gz'
```

Two things worth knowing if this needs to be redone:

- The plan's `docs/macos-m5-plan.md` §11 example builds a synthetic ELF32
  header by hand with `printf`/`dd` — an off-by-one there (one extra `\x00`)
  silently shifted every subsequent field and made the image classify as
  `arch:unknown` instead of `i386`. Building it with a short `python3 -c
  "..."` one-liner using `struct.pack` is far less error-prone than counting
  escape bytes by hand — worth doing that way from the start next time.
- `install.sh` prints a hostname warning because `noble-builder` looks
  cloud-init-managed. It's a real, permanent hostname on this VM (not
  actually randomized per boot), so the warning is a false positive for this
  specific box — but it's still worth reading if this is ever run against an
  actual cloud instance, since IOL licensing really does break if the
  hostname changes after install.

---

## 6. Environment as it actually is now

| Item | Value |
|---|---|
| Host (Mac, criteria 1/3/4/5) | `rohansharma@192.168.101.166`, key `.m5-ssh/iolbox_mac_m0` |
| `iolbox-m5-e2e` | **Stopped** (this session's working machine; returned to that state at the end). Installed supervisor: `m5-cont-20260815b`. Has both the router x86_64 image and the synthetic `i86bi_m5_unsupported.bin` (2048-byte ELF32/EM_386 fixture, test-only, not a real Cisco image) registered, plus the `m5-unsupported-lab` saved lab (its `startupConfig` was rewritten to `''` this session — see result doc §2's test-harness-artifact note; it now loads correctly). |
| `iolbox-m4-e2e` | **Running** — pre-existing state from an earlier session, untouched by any M5 work. Do not assume its supervisor build reflects M5. |
| `iol22` | Untouched. Never touch. |
| Other Mac machines | Untouched, Stopped. |
| Host (Windows, criterion 2) | `ubuntu@192.168.226.10` (`noble-builder-vm`) |
| `noble-builder-vm` | Returned to its normal PNetLab-Noble-build-only state — no iolbox service or files left installed. |

Evidence roots: `~/iolbox-m1/evidence-m5/m5-luna-20260815T0118/`,
`~/iolbox-m1/evidence-m5/m1-20260815T052417Z-43472/` (Luna's session),
`~/iolbox-m1/evidence-m5/continue-20260815/` (browser-proof continuation
session), and `~/iolbox-m1/evidence-m5/criterion2-20260815/` (non-Mac
criterion-2 session) — all on the Mac. See result doc §3 for exact contents
of each.

---

## 7. Process notes carried forward

- Same SSH-from-Windows access pattern as prior sessions worked fine here:
  `ssh -i "J:\Claude code\iolab-m5-wt\.m5-ssh\iolbox_mac_m0"
  rohansharma@192.168.101.166`.
- `git status` on this worktree shows a large uncommitted diff spanning both
  Luna's implementation and this session's two Svelte fixes, plus build
  byproducts (`supervisor/internal/web/dist/*`, `.m5-go-cache/`,
  `tools/.m5-build/`, `tools/tools/`). None of this has been committed —
  the owner should decide the commit/squash shape before merging anywhere.
- `supervisor/internal/web/dist/index.html` etc. being modified in `git
  status` is expected: it's the tracked placeholder that gets overwritten by
  `npm run build:embed` and is meant to be restored by `build-release.sh`'s
  own placeholder-restore step for a real release build — this session did
  not run that restore since no release build was being produced, only
  focused hardware-test binaries.
