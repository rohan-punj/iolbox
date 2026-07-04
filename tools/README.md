# tools/

Small standalone helpers that support the app but aren't part of the GUI or the
supervisor.

## capture-helper

Bridges an iolbox supervisor **capture port** (a raw pcapng byte stream for one
link, opened by the `capture.start` verb) to a live Wireshark window.

```
supervisor (in runtime)                Windows
  link relay --tee--> TCP :5500  ====>  capture-helper  --stdin-->  wireshark -k -i -
```

- `capture-helper -connect 127.0.0.1:5500` — the minimum. Auto-detects
  `Wireshark.exe`, launches `wireshark -k -i -`, and streams frames into it live.
- `-out capture.pcapng` — also record to a file (record mode).
- `-relaunch` (default on) — if the user closes Wireshark, respawn it and resume,
  replaying the buffered pcapng section header so the new window gets a valid file.
  This is what makes "right-click link → Capture" feel like a persistent tap.

The Tauri app's `start_capture` command spawns this helper (or reimplements the
same ~30 lines of copy loop in Rust). Keeping it standalone means capture also
works from a plain terminal for debugging, independent of the GUI.

Build (target is the Windows host, where Wireshark runs):

```
cd tools/capture-helper
GOOS=windows GOARCH=amd64 go build -o ../../app/src-tauri/binaries/capture-helper.exe .
```

### Assumption to verify in P0
The helper treats the supervisor's **first TCP read** as the pcapng section header
(SHB + IDB) to buffer for relaunches. The supervisor is documented (see
`docs/protocol.md`) to flush SHB+IDB before any packet block. If a real IOL
capture shows a torn header on relaunch, replace `readInitialHeader` with a proper
pcapng block splitter (SHB=0x0A0D0D0A, IDB=0x00000001) — the code has a comment
marking exactly where.
