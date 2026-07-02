# P0 spike — risk-retirement runbook

P0 proves the risky, unknown parts work against a **real IOL image** before we
build more on top. If every step below passes, everything after P0 is known-good
engineering. No GUI is involved — this is supervisor + runtime + capture only.

Requires: one real IOL image (user-supplied, L3), VMware Workstation (primary
provider here), Wireshark, and the built supervisor + runtime.

## Build inputs

```
# supervisor (linux target)
cd supervisor && GOOS=linux GOARCH=amd64 go build -o bin/supervisor-linux-amd64 ./cmd/supervisor

# runtime appliance with that supervisor baked in (on a Linux builder)
cd runtime && ./build-all.sh ../supervisor/bin/supervisor-linux-amd64

# capture helper (windows)
cd tools/capture-helper && GOOS=windows GOARCH=amd64 go build -o capture-helper.exe .
```

## Steps & pass criteria

| # | Step | Pass criteria | Assumption it validates |
|---|------|---------------|--------------------------|
| 1 | `vmrun -T ws start iolab-appliance.vmx nogui`; connect to control port | `hello` returns supervisor version + `features` includes `i386` | VMware provider boots headless; endpoint reachable from Windows |
| 2 | Copy IOL image into runtime; `image.register` | returns id, `class`, `arch=i386\|x86_64` | ELF sniff + i386 multiarch present |
| 3 | First-boot iourc generated | `/opt/iolab/iourc` exists; IOL licenses OK | keygen algorithm correct for this hostid |
| 4 | `lab.load` + `node.start` one IOL (R1); telnet its console port from Windows | IOS boots to prompt; **no `-l` flag** → idle CPU low | console mechanism + keepalive-flag fix |
| 5 | Start R2; `link.add` p2p R1e0/0–R2e0/0; configure /30 both sides | `ping` R1→R2 succeeds | UDP p2p wiring + NETMAP encoding |
| 6 | Start VPCS PC1; link to R1 e0/1; set PC IP | PC1 pings R1 LAN IP | VPCS UDP tunnel + mixed IOL/VPCS segment |
| 7 | `capture.start` on the R1–R2 link; run `capture-helper -connect <ip>:<capPort>` | Wireshark opens, shows the ICMP echoes as clean ethernet frames | pcapng tee + UDP-header strip + helper bridge |
| 8 | Close Wireshark, reopen via helper `-relaunch` | capture resumes with valid header | header-buffer/relaunch logic |
| 9 | `config.save` R1; stop; start; `config.extract` | startup-config round-trips through NVRAM | NVRAM codec |
| 10 | Repeat step 1–7 under the **wsl2** provider | same results | provider abstraction holds across backends |

## Known assumptions to confirm here (from the build agents)

These were flagged during the parallel build as needing a real image — P0 is where
they get confirmed or corrected. Each has a clearly-marked spot in the code:

- **IOL UDP header size** (relay strip length) — `supervisor/internal/relay`.
- **Telnet console mechanism** (native IOL telnet port vs pty bridge) — `supervisor/internal/node`.
- **iourc keygen** hostid→key — `supervisor/internal/iourc`.
- **NVRAM format** header/checksum — `supervisor/internal/nvram`.
- **L2 vs L3 class heuristic** — `supervisor/internal/image`.
- **pcapng first-read-is-header** heuristic — `tools/capture-helper`.
- **supervisor `-gen-iourc` flag** name — coordinated with `runtime/firstboot-iourc.sh`.
- **host-only IP** (default `192.168.171.2`) — `runtime/pack-vmware.sh` + vmware provider.

## P0 findings — 2026-07-02 (real IOL 17.18.02, x86_64)

Environment: isolated Ubuntu 24.04 VMware VM (`J:\iolab-runtime-vm`, host-only
192.168.111.0/24, key `iolab_key`), built from an Ubuntu cloud image — no WSL, no
project VMs touched. Supervisor + both images + VPCS 0.8.3 deployed to `/opt/iolab`.

**Confirmed:**
- ✅ **Image sniff**: `x86_64_crb_linux-...iol17.18.02.bin` is ELF64 x86-64; runs on
  stock Ubuntu 24.04 glibc — **no i386 multiarch needed** for the 17.18.02 line
  (`ldd` shows all libs present). i386 only matters for the older `i86bi_` images.
- ✅ **iourc keygen ACCEPTED by real IOL.** `supervisor -gen-iourc` on hostid
  `a8c0986f` / hostname `iolab-rt` produced `iolab-rt = 97824a8bfa46e85b;`. IOL
  booted (`IOS On Unix - Cisco Systems confidential…`) with **no license error**.
  The community keygen in `internal/iourc` is correct.
- ✅ **Console mechanism = pty, NOT a TCP port.** IOL uses stdin/stdout on a
  controlling pty for its console and opens **no** telnet port itself. A
  pty→TCP bridge (validated here with `socat TCP-LISTEN,fork EXEC:…,pty,setsid,ctty`)
  exposes it as telnet. **Action:** `internal/node/spawn_linux.go` must allocate a
  pty, run IOL attached to it (`setsid`+`ctty`), and bridge that pty to the node's
  telnet console port — the `wsbridge` console path then dials that port. Resolves
  the open "console mechanism" assumption (it is neither env-selected nor 2000+id).
- ✅ Benign: IOL prints `Warning: Abnormal ciscoversion string … we parsed - NULL`
  on this build — not fatal, ignore.

**Still to run (needs the supervisor's node pty-bridge + UDP wiring exercised
end-to-end):** steps 5–9 above — 2×IOL+VPCS over UDP netio, ping, Wireshark tee,
NVRAM round-trip. This is the next P0 session: run the actual supervisor on the VM,
`lab.load` + `lab.start` the P0 lab, and verify wiring/capture. Expect a targeted
fix in `internal/node` for the pty console bridge before consoles work through the
supervisor.

## Exit

P0 passes when steps 1–9 pass on VMware and step 10 passes on WSL2. Then proceed to
P1 (wire the GUI's real `TcpTransport` to the supervisor, replacing the mock).
