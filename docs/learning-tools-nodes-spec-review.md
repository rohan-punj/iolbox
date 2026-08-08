## Critical

1. **The proposed Level-A HTTP GUI is not reachable with the stated topology.** A process listening on `:80` inside `iolt<N>` is reachable only through that netns’s loopback or an addressed/routed interface. The design assigns no IP address or host-facing management path; the bridge-side veth is L2-only on a lab bridge. The supervisor cannot simply dial “netns-local `:80`” from its own network namespace. Also, port 80 requires root or `CAP_NET_BIND_SERVICE`, which the proposed allowlist excludes.

   Fix: make the pack GUI listen on a per-node Unix-domain socket in a supervisor-created private directory, and reverse-proxy that socket; or explicitly add a separate private management transport with firewalling. Prefer an unprivileged port/socket over granting another capability.

2. **“Netns is the isolation boundary Docker used to provide” is materially overstated.** P0/P1 as described supplies only a network namespace plus two powerful capabilities to a real-UID process. It does not isolate filesystem, processes, IPC, hostname, cgroup resources, kernel attack surface, or often Docker’s default seccomp/AppArmor/LSM confinement. With the supervisor normally privileged, a mistake in capability setup can leave the child effectively host-root.

   `CAP_NET_RAW`/`CAP_NET_ADMIN` do confine ordinary packet traffic to interfaces visible in the netns; they do not make an untrusted Python program safe. It can manipulate its interface/routes/firewall/qdisc, attack the shared kernel through networking surfaces, and intentionally disrupt every endpoint on its attached L2 segment. It ordinarily cannot move a veth into the initial namespace without authority there, so the veth is a useful boundary—but not a container replacement.

   Fix: make user namespace + unprivileged mapped UID, mount namespace, PID namespace, cgroup v2, `no_new_privs`, a cleared bounding set, seccomp, and an LSM profile prerequisites for general tool packs—not P3 hardening. If that is infeasible, explicitly support only immutable first-party packs and describe them as privileged host-adjacent code, not isolated plugins.

3. **The ambient-capability proposal is incomplete and unsafe as written.** “Ambient `NET_RAW,NET_ADMIN` and drop all others” is not achieved merely by `setpriv --ambient-caps` or a Go `SysProcAttr` setting. Ambient caps require matching permitted/inheritable capability state; a privileged UID can regain capabilities across exec unless securebits and the bounding set are handled correctly. The spec also gives the long-lived pack GUI—and therefore all scripts it launches—`CAP_NET_ADMIN`, rather than granting it only to narrowly scoped helpers.

   Current code is not a precedent for this: extnet performs privileged host operations via `ip`/`iptables` commands, not delegated ambient child capabilities ([endpoint_linux.go:61](J:/Claude%20code/iolab/supervisor/internal/extnet/endpoint_linux.go:61), [detect_linux.go:37](J:/Claude%20code/iolab/supervisor/internal/extnet/detect_linux.go:37)).

   Fix: create network objects from the supervisor, then launch a non-root child with an explicitly tested capability transition. Use file capabilities or a tiny dedicated helper where possible; give `NET_ADMIN` only to modules proven to need it. Test `/proc/self/status`, `capsh --print`, privilege regain after exec, and attempted namespace/interface operations.

4. **The phase order ships unbounded privileged attack workloads before basic containment.** P1/P2 would run arbitrary Python attack modules without CPU, memory, PID, disk, or reliable process-tree control. `RLIMIT_AS` is not a substitute for cgroup memory accounting; it does not limit CPU or aggregate descendants. `systemd-run --scope` is not portable to WSL/LXC and may lack controller delegation.

   Fix: move cgroup v2 creation and limits to P0/P1. Set `memory.max`, `pids.max`, CPU weight/quota, and clean up the cgroup on every failure path. Define a platform capability gate based on an actual create/start/kill probe, not package presence.

## High

5. **The spec’s bridge names are wrong, and several “already implemented” claims are broader than the code.** The bridge fabric names links `iolbr<id>`, not `br-<linkid>` ([commands.go:99](J:/Claude%20code/iolab/supervisor/internal/fabric/commands.go:99)). `internal/fabric` creates taps and bridges and performs `ip link … master`; it does not implement netns, veth, mounts, or iptables ([commands.go:14](J:/Claude%20code/iolab/supervisor/internal/fabric/commands.go:14)). iptables/NAT wiring belongs to extnet.

   NAT does attach an unbridged tap later and supports hot attach/detach ([endpoint_linux.go:215](J:/Claude%20code/iolab/supervisor/internal/extnet/endpoint_linux.go:215)), so that limited analogy is sound. But it is a process-less tap/DHCP endpoint, not a model for a supervised process tree.

   Fix: correct all names and separate “existing bridge attach primitive” from entirely new netns/veth/process-sandbox machinery.

6. **Fabric integration requires more than adding cases to `attachFabricLink`.** The current switch defaults unknown kinds to a static IOL tap ([fabric_linux.go:293](J:/Claude%20code/iolab/supervisor/internal/server/fabric_linux.go:293)), and fabric eligibility is calculated separately. Link stats likewise only recognize VPCS, NAT, or static IOL taps ([fabric_linux.go:596](J:/Claude%20code/iolab/supervisor/internal/server/fabric_linux.go:596)). A tool node would otherwise fail with “no static tap,” not hot-connect.

   Fix: add `tool` explicitly to:
   - fabric eligibility/planning;
   - attach/detach;
   - late-start attachment;
   - stats/directional classifier endpoint discovery;
   - teardown/rebuild paths;
   - tests for link add/remove and node restart.

7. **Lifecycle and crash semantics are underspecified.** Existing IOL lifecycle relies on a `cmd.Wait()` goroutine which changes state and tears down console resources ([spawn_linux.go:394](J:/Claude%20code/iolab/supervisor/internal/node/spawn_linux.go:394)); VPCS needed special process-group and orphan cleanup due daemonization ([spawn_linux.go:140](J:/Claude%20code/iolab/supervisor/internal/node/spawn_linux.go:140)). A tool GUI starts its own helpers, potentially daemonizes, and may become PID 1 in a PID namespace. PID 1 has special signal/reaping behavior; without an init/reaper, stopped modules become zombies.

   Fix: introduce a dedicated endpoint supervisor with a stable init/reaper wrapper, process group/cgroup ownership, `Pdeathsig`, readiness check, exit watcher, state/event emission, bounded graceful-stop then kill, and stale-netns/veth/cgroup recovery after supervisor crash.

8. **Mount/PID namespace text is internally inconsistent and operationally incomplete.** The spec says the rest of the appliance rootfs is “not mounted,” but Python, the dynamic loader, stdlib, Go GUI binary, DNS/config files, devices, and required libraries must be available. A read-only bind mount of only the venv and pack is not a runnable Linux root. `CLONE_NEWPID` also needs a correctly mounted private `/proc`; merely unsharing PID does not hide the old `/proc`.

   Fix: specify an executable minimal root (or a read-only recursive root bind with carefully remounted writable paths), mount propagation set to private, exact `/dev` policy, `/proc` mount, tmpfs quotas, DNS/time requirements, and a test proving host files and PIDs are not visible.

9. **The reverse proxy adds a new web security boundary that is not designed.** wsbridge currently has no Origin check ([wsbridge.go:30](J:/Claude%20code/iolab/supervisor/internal/wsbridge/wsbridge.go:30)) and only exposes fixed `/control`, `/console`, and `/capture` routes ([wsbridge.go:134](J:/Claude%20code/iolab/supervisor/internal/wsbridge/wsbridge.go:134)). A pack-controlled HTML/htmx application proxied under the appliance origin can target control APIs, exploit cookies/storage, use WebSockets, or create path/cookie/redirect confusion.

   Fix: serve pack UI on a separate origin with strict CSP/frame-src, authenticate/authorize its proxy path, reject arbitrary proxy paths and upgrades by default, sanitize forwarded headers, and define whether pack UIs may make same-origin requests.

## Medium

10. **P0 does not validate the riskiest assumptions.** It validates basic L2 forwarding and one raw packet. It does not validate:
   - capability transition through Go GUI → Python → Scapy;
   - host-to-netns GUI proxying;
   - non-root port binding;
   - mount/PID/user namespace composition;
   - process-tree reaping/kill;
   - cgroup enforcement;
   - no host filesystem/process visibility;
   - stale cleanup after supervisor kill;
   - runtime support in WSL/LXC/Docker.

   Fix: split P0 into explicit acceptance tests, including a hostile test program that tries to enumerate host interfaces/processes/files, regain capabilities, alter interfaces, escape its cgroup, and survive parent termination.

11. **Runtime feature detection cannot be copied verbatim from NAT.** NAT detection is only `/dev/net/tun` plus `sudo -n true` ([detect_linux.go:41](J:/Claude%20code/iolab/supervisor/internal/extnet/detect_linux.go:41)); it does not prove `ip`, netns creation, veth creation, mount unshare, cgroup delegation, or even account for the supervisor already running as root. `/dev/net/tun` is not required for a veth-only tool node.

   Fix: use an operational, cleanup-verified probe for the exact required primitive set, cache its detailed failure reason, and expose a specific capability matrix rather than one optimistic `tools` flag.

12. **GUI work is understated.** The palette drag type and canvas types are currently literal unions of `iol | vpcs | nat` ([Palette.svelte:9](J:/Claude%20code/iolab/app/src/lib/components/Palette.svelte:9)); canvas node registration maps NAT to `VpcsNode`, with only IOL/VPCS/NAT registered. The current console dock accepts terminal/capture tabs, not arbitrary iframe tool panels. `tool.listPacks` also requires protocol verbs, client transport/types, state, error handling, and installed-pack metadata—not just a palette loop.

   Fix: make “tool panel/proxy” a separate vertical slice with protocol/schema/client-store design and tests; do not present it as a small `ToolNode.svelte` addition.

13. **“Drop a pack directory with no Go code” is not yet a safe plugin contract.** A manifest selects executable paths, arguments, HTTP behavior, capabilities, and options. It needs strict schema/versioning, canonical path containment, immutable ownership/modes, signature/hash verification before execution, upgrade/rollback semantics, and a policy for pack-specific binaries and Python imports. A shared read-only venv prevents writes, but does not prevent one pack’s Python from importing another writable/search-path artifact if environment construction is loose.

   Fix: keep packs immutable and root-owned, enumerate them at build time for v1, use absolute allowlisted executable paths and a scrubbed environment, pin/offline-wheel dependencies with hashes/SBOMs, and defer third-party pack installation until a signed-pack trust model exists.

14. **Build claim needs validation and reproducibility work.** `build-rootfs.sh` currently has no Python/venv/libpcap/util-linux in `BASE_INCLUDE` ([build-rootfs.sh:186](J:/Claude%20code/iolab/runtime/build-rootfs.sh:186)), and its shown chroot package step is only the optional i386 install ([build-rootfs.sh:217](J:/Claude%20code/iolab/runtime/build-rootfs.sh:217)). “`pip install scapy` in the chroot” needs an offline wheelhouse, pinned hashes, architecture considerations, license/SBOM handling, and cleanup of pip caches—not a runtime network assumption.

   Fix: add a build-stage wheelhouse with `--require-hashes --no-index`, verify imports in the produced rootfs, and measure the final compressed artifacts per provider rather than quote an unverified 40–60 MB estimate.

15. **NAT/mgmt wording is stale.** The spec repeatedly says NAT/mgmt is an existing analogous pattern, but the current `lab.Kind` and `extnet.Kind` contain NAT only ([lab.go:40](J:/Claude%20code/iolab/supervisor/internal/lab/lab.go:40), [extnet.go:22](J:/Claude%20code/iolab/supervisor/internal/extnet/extnet.go:22)). That should not be used as evidence of a second external-node implementation.

Overall: the veth-to-link-bridge idea is viable and should preserve capture visibility, but it should be treated as a privileged sandboxing subsystem plus a web-proxy subsystem—not a small extension of the existing NAT endpoint.