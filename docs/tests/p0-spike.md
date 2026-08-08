# P0 learning-tool kernel spike

This is the real-target acceptance gate for T0.1 through T0.9 in
`docs/learning-tools-nodes-plan.md`. It is deliberately independent of the
supervisor pack engine and frontend.

Build the Linux helpers from the repository checkout and run the complete gate
as root on the candidate appliance/runtime:

```sh
sudo bash docs/tests/p0-spike.sh
```

The target must provide `iproute2`, util-linux `setpriv` (at least 2.33 for the
pinned ambient-capability argv), `libcap`/`capsh`, `tcpdump`, Python 3 with
Scapy, and an unprivileged `ioltool` account. The script creates only
ID-derived objects: a temporary delegated cgroup subtree, `iolt<ID>` netns,
`vtool<ID>` veths, `/run/iolbox/tool/<ID>`, and the manual `iolbr0` bridge. It
refuses to overwrite an existing `iolbr0` and removes its own objects on exit.
The durable-state fixture defaults to `/var/lib/iolbox/p0-spike-<ID>` so it
does not overwrite a live install's `/var/lib/iolbox/instance-id`; set
`IOLBOX_P0_STATE_DIR` to an equivalent disposable persistent directory when
the target layout requires it.

The output is the acceptance record. T0.2 also records whether the exact
`setpriv` command was sufficient or whether `p0-launcher` had to take the
native securebits fallback. A target lacking delegation, a required controller,
Scapy, or a kernel primitive prints `FAIL`; do not convert that into a passing
result.

The hostile probe intentionally reports `HOST_FILE_ACCEPTED_RISK`: v1 does not
have a mount namespace, so a tool running as the shared `ioltool` account can
read a world-readable host file. All other hostile markers must be `DENIED`.

T0.9 starts the stub GUI in one netns, attaches its bridge-side veth to the
exact production bridge name `iolbr0`, and uses the stub's `/send-arp` endpoint
to run Scapy inside the netns. It requires both a peer RX packet and an ARP
frame in the `tcpdump -i iolbr0` pcap.

## Real-target run log (2026-08-08, appliance VM 192.168.226.233, Debian 12 bookworm)

**T0.1–T0.8 all genuinely PASS** on real hardware, reached after 7 fix rounds against
live evidence (each commit on `feat/learning-tool-nodes` names the bug it fixes —
`git log --oneline main..feat/learning-tool-nodes` for the full list). Worth knowing
before the next run:

- **Target prerequisites this appliance did NOT ship out of the box**, installed
  ad hoc for this run (not baked into the rootfs — that's a P1/packaging concern,
  not done here): `python3` + `python3-scapy`, an `ioltool` system account
  (`useradd -r -M -s /usr/sbin/nologin ioltool`), and Go **1.26+** (the shipped
  `golang-go` 1.19 is too old for this module's `go 1.26` directive — installed the
  official tarball to `/usr/local/go`). `setpriv`, `capsh`, `tcpdump`, `ip` were
  already present.
- **Run the gate inside a properly delegated systemd scope**, not a bare shell —
  the production unit will need `Delegate=yes` + `{CPU,Memory,Tasks,IO}Accounting=yes`
  (already tracked as a P1 packaging task), and the spike needs the equivalent to
  even get a `cpu` controller listed at all:
  ```sh
  systemd-run --scope --unit=p0-spike -p Delegate=yes -p CPUAccounting=yes \
    -p MemoryAccounting=yes -p TasksAccounting=yes -p IOAccounting=yes -- \
    bash docs/tests/p0-spike.sh
  ```
  Running it as a plain child of an undelegated cgroup (e.g. `open-vm-tools.service`
  via a bare `runProgramInGuest`) fails immediately on the top-level `cpu` controller
  check — expected, not a bug.
- **Run from a world-traversable path**, e.g. `/opt/...`, not `/root/...` — `/root`
  is `0700` by default and the unprivileged `ioltool` account can't exec anything
  under it (surfaces as a bare `permission denied`/EACCES from the launcher, easy to
  mis-read as a capability bug).
- Each run leaves kernel-global objects (a dummy `eth1`, `iolt*`/`vtool*`
  namespaces/veths, `tool-*` cgroups) if it fails before its own cleanup trap runs;
  a stale dummy `eth1` from a prior aborted run can fail T0.4. Clear state between
  attempts if re-running after a hard failure (`ip link del eth1`, `ip netns del
  <name>`, etc.) rather than assuming a clean box.

**T0.9 NOT YET RUN** — this appliance had a real lab already running (`iolbr0` up,
live `vpcs` processes) when P0 was exercised, and T0.9 correctly refused to touch
the pre-existing production bridge rather than disrupt it (working as designed, see
above). Run T0.9 on a lab-free box (stop the running lab first, or use a clean
VM/builder) before treating P0 as fully closed.
