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
