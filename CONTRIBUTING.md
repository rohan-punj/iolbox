# Contributing to iolbox

Thanks for helping! A few things keep this project healthy and legal.

## Ground rules

- **Never commit or attach Cisco software** — no IOL/IOU `.bin`/`.iol` images, no
  `iourc` keys, no config dumps containing secrets. The `.gitignore` blocks the
  common file types; don't work around it.
- iolbox is **IOL + VPCS, single-user, localhost-only** by design. Features that
  add other runtimes, multi-user, or network-exposed services are out of scope —
  that's what PNetLab/EVE/CML are for. Keep it lightweight.

## Repo map

| Path | What | Language |
|---|---|---|
| `contracts/` | `lab.schema.json` — source of truth for lab format | JSON Schema |
| `docs/` | protocol, providers, architecture, build | Markdown |
| `supervisor/` | control + data plane, runs in the runtime | Go (linux/amd64) |
| `runtime/` | rootfs + WSL/VMware appliance build | Bash |
| `app/` | Tauri + Svelte GUI | Rust + Svelte/TS |
| `tools/` | capture-helper, dev scripts | Go |
| `labs/` | example labs + pack importer | JSON + Python |

## Changing the lab format

Edit `contracts/lab.schema.json` first, then update the Go structs
(`supervisor/internal/lab`) and TS types (`app/src/lib/labTypes.ts`) to match, and
bump `version` if it's breaking. CI validates `labs/*.lab.json` against the schema.

## Before you push

- `supervisor`: `go vet ./... && go test ./...` and a `GOOS=linux` build.
- `app`: `npm run check && npm run build`.
- Keep commits scoped; describe cross-component contract changes clearly.

## Assumptions that need real hardware

Several IOL specifics (UDP header size, telnet console mechanism, NVRAM format,
keygen) are validated in the **P0 spike** against a real image. If you have IOL
access and can confirm/correct one, that's the most valuable contribution.
